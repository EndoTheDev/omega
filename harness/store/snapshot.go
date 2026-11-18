package store

// snapshot.go: consistent store copies for the CLONE law. A SQLite store is copied
// through SQLite itself, never by reading its files. The source is LIVE at clone time
// (main's store is the serving daemon's own; a wake child's store is open while it
// runs), and a live WAL-mode database keeps committed rows in the -wal until a
// checkpoint: a byte copy of db + wal + shm is not a snapshot. It can tear (the wal and
// the main file are copied at different instants), it can silently miss frames
// committed between reads, and -shm is a process-local index that means nothing in
// another home.

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
)

// SnapshotStore writes a consistent copy of the SQLite database at srcDSN to dstPath. A
// missing source is a no-op: an agent that never ran has no store to copy, and a clone
// must not create a database it has no rows for. An existing destination is refused
// (VACUUM INTO will not overwrite, and a clone never clobbers).
func SnapshotStore(srcDSN, dstPath string) error {
	if _, err := os.Stat(srcDSN); err != nil {
		if os.IsNotExist(err) {
			return nil // never ran: no store to copy
		}
		return fmt.Errorf("snapshot store: %w", err)
	}
	if _, err := os.Stat(dstPath); err == nil {
		return fmt.Errorf("snapshot store: destination %s already exists", dstPath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("snapshot store: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
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
