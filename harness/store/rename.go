package store

// rename.go: renaming ONE session id inside ONE store file (card
// t_5df01d0f). The canonical-chat id became agent-qualified, and every
// agent home created before that carries the bare id - a live instance
// keeps its history only if the row MOVES, not if a fresh empty chat is
// minted beside it.
//
// Messages and pending calls follow the row: a session whose history
// stayed behind under the old id has been split in two, which is the
// one outcome worse than the collision this renames away.

import (
	"database/sql"
	"fmt"
	"os"
)

// RenameSession moves sessionID to newID inside the store file at dsn -
// the session row plus its messages, pending calls and both FTS
// entries. Child rows that point at the id (a forked session's
// parent_id) move with it.
//
// No-ops, each for its own honest reason:
//   - the file does not exist: an agent home that never ran has no rows
//     to rename, and opening it would create the database it lacks;
//   - oldID is absent: already renamed (idempotent, so every boot may
//     call it);
//   - the target id already exists: never merge two chats into one. The
//     caller decides what that means; this function invents nothing.
func RenameSession(dsn, oldID, newID string) error {
	if oldID == "" || newID == "" || oldID == newID {
		return nil
	}
	if _, err := os.Stat(dsn); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("rename session: %w", err)
	}
	db, err := sql.Open("sqlite", dsn+"?_pragma=foreign_keys(1)&_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return fmt.Errorf("rename session: open: %w", err)
	}
	defer db.Close()

	var present, taken int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE id = ?`, oldID).Scan(&present); err != nil {
		return fmt.Errorf("rename session: read %q: %w", oldID, err)
	}
	if present == 0 {
		return nil
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE id = ?`, newID).Scan(&taken); err != nil {
		return fmt.Errorf("rename session: read %q: %w", newID, err)
	}
	if taken != 0 {
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("rename session: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	// The child rows reference sessions(id) and the schema declares no
	// ON UPDATE action, so every reference is rewritten inside the same
	// transaction and the constraint is checked once, at COMMIT.
	if _, err := tx.Exec(`PRAGMA defer_foreign_keys = 1`); err != nil {
		return fmt.Errorf("rename session: defer fks: %w", err)
	}
	for _, stmt := range []struct {
		query string
		args  []any
	}{
		{`UPDATE sessions SET id = ? WHERE id = ?`, []any{newID, oldID}},
		{`UPDATE sessions SET parent_id = ? WHERE parent_id = ?`, []any{newID, oldID}},
		{`UPDATE sessions SET delegate_parent = ? WHERE delegate_parent = ?`, []any{newID, oldID}},
		{`UPDATE messages SET session_id = ? WHERE session_id = ?`, []any{newID, oldID}},
		{`UPDATE pending_calls SET session_id = ? WHERE session_id = ?`, []any{newID, oldID}},
		// The label/summary index keys on session_id and its triggers
		// fire on label/summary writes only - an id rename must reindex
		// itself or search keeps answering the old id.
		{`DELETE FROM sessions_fts WHERE session_id IN (?, ?)`, []any{oldID, newID}},
	} {
		if _, err := tx.Exec(stmt.query, stmt.args...); err != nil {
			return fmt.Errorf("rename session %q -> %q: %w", oldID, newID, err)
		}
	}
	if _, err := tx.Exec(
		`INSERT INTO sessions_fts(session_id, label, summary) SELECT id, label, summary FROM sessions WHERE id = ?`,
		newID); err != nil {
		return fmt.Errorf("rename session: reindex %q: %w", newID, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("rename session %q -> %q: commit: %w", oldID, newID, err)
	}
	// messages_fts is an external-content index: its UPDATE trigger
	// reindexes a row, but a body-less rebuild is the cheap way to make
	// the whole index agree with the table after a mass rewrite.
	if _, err := db.Exec(`INSERT INTO messages_fts(messages_fts) VALUES('rebuild')`); err != nil {
		return fmt.Errorf("rename session: rebuild message index: %w", err)
	}
	return nil
}
