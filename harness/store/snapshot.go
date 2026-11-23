package store

// snapshot.go: consistent store copies for the CLONE law. The copy is the SESSION FILES
// (the truth) plus the index, and the index half travels through SQLite itself - never a
// byte copy: the source is LIVE (a wake child's store is open while it runs), and a live
// WAL-mode database keeps committed rows in the -wal until a checkpoint.

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// SnapshotStore copies the store at srcDSN to dstPath. A missing source is a no-op (an
// agent that never ran has no store, and a clone must not create one); an existing
// destination is refused - a clone never clobbers.
func SnapshotStore(srcDSN, dstPath string) error {
	if !homeHasStore(srcDSN) {
		return nil // never ran: no store to copy
	}
	if _, err := os.Stat(dstPath); err == nil {
		return fmt.Errorf("snapshot store: destination %s already exists", dstPath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("snapshot store: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return fmt.Errorf("snapshot store: %w", err)
	}
	// The files first: if the copy dies after them, the destination still opens and
	// rebuilds its index from them.
	if err := copySessionsDir(sessionsDirFor(srcDSN), sessionsDirFor(dstPath)); err != nil {
		return err
	}
	if _, err := os.Stat(srcDSN); err != nil {
		if os.IsNotExist(err) {
			return nil // files-only source: the destination's index rebuilds at open
		}
		return fmt.Errorf("snapshot store: %w", err)
	}
	// The same pragma set Open applies: the read side must see the source exactly.
	db, err := sql.Open("sqlite", srcDSN+"?_pragma=foreign_keys(1)&_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return fmt.Errorf("snapshot store %s: open: %w", srcDSN, err)
	}
	defer db.Close()
	if _, err := db.Exec(`VACUUM INTO ?`, dstPath); err != nil {
		return fmt.Errorf("snapshot store %s: %w", srcDSN, err)
	}
	return nil
}

// copySessionsDir copies every session file. A missing source directory is empty, not an
// error: a store whose sessions were all deleted still clones.
func copySessionsDir(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("snapshot store: read %s: %w", src, err)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return fmt.Errorf("snapshot store: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		from := filepath.Join(src, entry.Name())
		to := filepath.Join(dst, entry.Name())
		if err := copyFile(from, to); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return fmt.Errorf("snapshot store: %w", err)
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("snapshot store: %w", err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return fmt.Errorf("snapshot store: %w", err)
	}
	return out.Close()
}
