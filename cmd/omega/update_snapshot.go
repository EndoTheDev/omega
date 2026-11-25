package main

// The rollback snapshot: at finalize, WAL checkpoint, then copy omega.db and
// config.yaml into <data>/.pre-<newversion>. Retention is ONE - the latest snapshot only,
// matching the one-generation omega.bak contract; older .pre-* directories are deleted in
// the same step. A window pass keeps the snapshot as the retained backup; a window fail
// restores it.

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// snapshotPrefix names the snapshot directories beside omega.db.
const snapshotPrefix = ".pre-"

// updateSnapshot is one retained rollback point.
type updateSnapshot struct {
	Dir        string
	DB         string
	Config     string
	LiveDB     string
	LiveConfig string
	Version    string
}

// checkpointStore flushes the live store's WAL. A TRUNCATE checkpoint leaves every
// committed row in the main file and an empty -wal, so the copy that follows is the whole
// state and a rollback target that opens.
func checkpointStore(dbPath string) error {
	if _, err := os.Stat(dbPath); err != nil {
		if os.IsNotExist(err) {
			return nil // never ran: nothing to checkpoint
		}
		return err
	}
	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return fmt.Errorf("checkpoint store: %w", err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		return fmt.Errorf("checkpoint store: %w", err)
	}
	return nil
}

// snapshotBeforeFinalize checkpoints, prunes older snapshots (retention 1), and copies the
// store + config into the new snapshot directory.
func snapshotBeforeFinalize(dbPath, configPath, version string) (*updateSnapshot, error) {
	if err := checkpointStore(dbPath); err != nil {
		return nil, err
	}
	dataDir := filepath.Dir(dbPath)
	dir := filepath.Join(dataDir, snapshotPrefix+version)
	if err := pruneSnapshots(dataDir, dir); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("snapshot dir: %w", err)
	}
	snap := &updateSnapshot{Dir: dir, DB: filepath.Join(dir, filepath.Base(dbPath)), Config: filepath.Join(dir, "config.yaml"), LiveDB: dbPath, LiveConfig: configPath, Version: version}
	if _, err := os.Stat(dbPath); err == nil {
		if err := copyFile(dbPath, snap.DB); err != nil {
			return nil, fmt.Errorf("snapshot store: %w", err)
		}
		if err := validateSnapshot(snap.DB); err != nil {
			return nil, fmt.Errorf("snapshot store is not a database: %w", err)
		}
	}
	if configPath != "" {
		if err := copyFile(configPath, snap.Config); err != nil {
			return nil, fmt.Errorf("snapshot config: %w", err)
		}
	}
	return snap, nil
}

// validateSnapshot opens the copy and reads its schema: a torn copy fails here, while the
// running process is still healthy, never at restore time.
func validateSnapshot(path string) error {
	db, err := sql.Open("sqlite", path+"?mode=ro")
	if err != nil {
		return err
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var n int
	return db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master`).Scan(&n)
}

// pruneSnapshots keeps exactly one .pre-* directory: the one about to be written.
func pruneSnapshots(dataDir, keep string) error {
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), snapshotPrefix) {
			continue
		}
		if filepath.Join(dataDir, e.Name()) == keep {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		if err := os.RemoveAll(filepath.Join(dataDir, name)); err != nil {
			return fmt.Errorf("prune snapshot %s: %w", name, err)
		}
	}
	return nil
}

// restoreSnapshot puts the retained snapshot back: the store file (and its WAL sidecars,
// removed first so a stale -wal can never graft onto the restored file) and config.yaml.
// It returns the labels of what it actually restored, for the receipt.
func restoreSnapshot(snap *updateSnapshot) []string {
	if snap == nil {
		return nil
	}
	var restored []string
	if _, err := os.Stat(snap.DB); err == nil {
		for _, sidecar := range []string{snap.LiveDB + "-wal", snap.LiveDB + "-shm"} {
			_ = os.Remove(sidecar)
		}
		if err := copyFile(snap.DB, snap.LiveDB); err != nil {
			fmt.Fprintf(os.Stderr, "omega update: rollback could not restore %s: %v\n", snap.LiveDB, err)
		} else {
			restored = append(restored, "omega.db")
		}
	}
	if snap.Config != "" {
		if _, err := os.Stat(snap.Config); err == nil {
			if err := copyFile(snap.Config, snap.LiveConfig); err != nil {
				fmt.Fprintf(os.Stderr, "omega update: rollback could not restore %s: %v\n", snap.LiveConfig, err)
			} else {
				restored = append(restored, "config.yaml")
			}
		}
	}
	return restored
}
