package store

// agentrows.go: the one-time boot migration out of the shared
// sessions-store shape (card t_72ecca67, ruling 4 - full data
// isolation). Under the retired shape a delegated child wrote its
// sessions into the main store tagged with a sessions.agent column;
// the settled shape gives every agent home its OWN store at
// <agent>/data/omega.db. This move relocates each agent-tagged session
// row - with its message history, FTS indexes and read state intact -
// into that agent's store, leaving the main store with main sessions
// only. Copy-then-verify-then-delete inside one transaction: a failed
// move leaves both stores untouched, never truncated.

import (
	"context"
	"database/sql"
	"fmt"
)

// MigrateAgentRows moves agent-tagged rows out of mainDSN into their
// per-agent stores. agentDSNs maps agent name -> that agent's
// omega.db path. An agent with no rows in the main store is skipped
// entirely - no store file appears for an agent that never ran.
// Idempotent: an already-migrated home has no rows left, so the second
// boot is a no-op.
func MigrateAgentRows(mainDSN string, agentDSNs map[string]string) error {
	if len(agentDSNs) == 0 {
		return nil
	}
	// The pre-count runs on its own short-lived handle, closed before
	// any move: the attach-side copy below wants the main db lock-free
	// (DETACH fails with "database maindb is locked" when another
	// pooled handle holds a read transaction).
	mainDB, err := sql.Open("sqlite", mainDSN+"?_pragma=foreign_keys(1)&_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return fmt.Errorf("agent rows migration: open main store: %w", err)
	}
	pending := map[string]string{}
	for name, agentDSN := range agentDSNs {
		var count int
		if err := mainDB.QueryRow(`SELECT COUNT(*) FROM sessions WHERE agent = ?`, name).Scan(&count); err != nil {
			mainDB.Close()
			return fmt.Errorf("agent rows migration: count %s: %w", name, err)
		}
		if count == 0 {
			continue // never wrote as this agent: no store file for it
		}
		pending[name] = agentDSN
	}
	if err := mainDB.Close(); err != nil {
		return fmt.Errorf("agent rows migration: close main store: %w", err)
	}
	for name, agentDSN := range pending {
		if err := moveAgentRows(mainDSN, name, agentDSN); err != nil {
			return err
		}
	}
	return nil
}

// sessionInsertCols / sessionSelectCols share one column order. The
// SELECT keeps the row's own parent_id when the parent moves to the
// same agent store and NULLs a stale reference into the main store -
// an FK violation on insert would otherwise abort the whole move.
const sessionInsertCols = `id, parent_id, label, created_at, updated_at,
	prompt_tokens, completion_tokens,
	pinned, archived, title_state, last_read_at,
	agent, last_run, model, workspace`

const sessionSelectCols = `id,
	CASE WHEN parent_id IS NULL OR parent_id IN (SELECT id FROM maindb.sessions WHERE agent = ?1)
	     THEN parent_id ELSE NULL END,
	label, created_at, updated_at,
	prompt_tokens, completion_tokens,
	pinned, archived, title_state, last_read_at,
	agent, last_run, model, workspace`

// moveAgentRows performs one agent's copy-verify-delete move. The agent
// store is opened first (its schema migrates into existence); the whole
// move then runs on the AGENT connection, maindb ATTACHed read-through.
func moveAgentRows(mainDSN, name, agentDSN string) error {
	agentStore, err := Open(agentDSN)
	if err != nil {
		return fmt.Errorf("agent rows migration: open %s store: %w", name, err)
	}
	defer agentStore.Close()
	conn, err := agentStore.db.Conn(context.Background())
	if err != nil {
		return fmt.Errorf("agent rows migration: %s: conn: %w", name, err)
	}
	defer conn.Close()
	tx, err := conn.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("agent rows migration: %s: begin: %w", name, err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`ATTACH DATABASE ? AS maindb`, mainDSN); err != nil {
		return fmt.Errorf("agent rows migration: %s: attach main: %w", name, err)
	}
	stmt := `INSERT INTO sessions (` + sessionInsertCols + `)
		SELECT ` + sessionSelectCols + ` FROM maindb.sessions WHERE agent = ?1`
	if _, err := tx.Exec(stmt, name); err != nil {
		return fmt.Errorf("agent rows migration: %s: copy sessions: %w", name, err)
	}
	if _, err := tx.Exec(`INSERT INTO messages (id, session_id, role, payload, created_at)
		SELECT id, session_id, role, payload, created_at FROM maindb.messages
		WHERE session_id IN (SELECT id FROM maindb.sessions WHERE agent = ?1)`, name); err != nil {
		return fmt.Errorf("agent rows migration: %s: copy messages: %w", name, err)
	}
	// Verify before deleting: moved row counts must equal the source
	// counts, message histories inclusive. On mismatch the whole move
	// rolls back and both stores stand untouched.
	var movedSessions, sourceSessions, movedMessages, sourceMessages int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&movedSessions); err != nil {
		return fmt.Errorf("agent rows migration: %s: count moved: %w", name, err)
	}
	if err := tx.QueryRow(`SELECT COUNT(*) FROM maindb.sessions WHERE agent = ?1`, name).Scan(&sourceSessions); err != nil {
		return fmt.Errorf("agent rows migration: %s: count source: %w", name, err)
	}
	if err := tx.QueryRow(`SELECT COUNT(*) FROM messages WHERE session_id IN (SELECT id FROM maindb.sessions WHERE agent = ?1)`, name).Scan(&movedMessages); err != nil {
		return fmt.Errorf("agent rows migration: %s: count moved messages: %w", name, err)
	}
	if err := tx.QueryRow(`SELECT COUNT(*) FROM maindb.messages WHERE session_id IN (SELECT id FROM maindb.sessions WHERE agent = ?1)`, name).Scan(&sourceMessages); err != nil {
		return fmt.Errorf("agent rows migration: %s: count source messages: %w", name, err)
	}
	if movedSessions != sourceSessions || movedMessages != sourceMessages {
		return fmt.Errorf("agent rows migration: %s: moved %d/%d sessions, %d/%d messages - aborting, both stores untouched",
			name, movedSessions, sourceSessions, movedMessages, sourceMessages)
	}
	if _, err := tx.Exec(`DELETE FROM maindb.messages WHERE session_id IN (SELECT id FROM maindb.sessions WHERE agent = ?1)`, name); err != nil {
		return fmt.Errorf("agent rows migration: %s: clear source messages: %w", name, err)
	}
	if _, err := tx.Exec(`DELETE FROM maindb.sessions WHERE agent = ?1`, name); err != nil {
		return fmt.Errorf("agent rows migration: %s: clear source sessions: %w", name, err)
	}
	// SQLite refuses a DETACH inside an open transaction: the move
	// commits first, the alias is dropped after on the same conn.
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("agent rows migration: %s: commit: %w", name, err)
	}
	if _, err := conn.ExecContext(context.Background(), `DETACH DATABASE maindb`); err != nil {
		return fmt.Errorf("agent rows migration: %s: detach: %w", name, err)
	}
	return nil
}
