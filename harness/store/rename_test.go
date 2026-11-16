package store

// rename_test.go: the session-id rename (card t_5df01d0f). The law the
// canonical-id migration stands on: moving a session to another id takes
// its history with it, and asking for a move that cannot be honoured
// (no file, no row, an occupied target) is a no-op, never a merge.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// renameFixture writes one store with "canonical" (one user + one
// assistant message, plus a forked child and a pending call) and
// returns its dsn.
func renameFixture(t *testing.T) string {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "omega.db")
	st, err := Open(dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	ctx := context.Background()
	if err := st.CreateSession(ctx, "canonical", "", "canonical"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := st.AppendMessage(ctx, "canonical", core.NewUser("patrol the journals")); err != nil {
		t.Fatalf("append user: %v", err)
	}
	if err := st.AppendMessage(ctx, "canonical", core.NewAssistant("0 anomalies")); err != nil {
		t.Fatalf("append assistant: %v", err)
	}
	// A fork of the chat: its parent reference must follow the rename,
	// or the tree answers a parent that stopped existing.
	if err := st.CreateSession(ctx, "canonical-fork", "canonical", "fork"); err != nil {
		t.Fatalf("create fork: %v", err)
	}
	if _, err := st.db.Exec(
		`INSERT INTO pending_calls (session_id, call_id, tool_name, arguments, dispatched_at)
		 VALUES ('canonical', 'call-1', 'shell.run', '{}', '2026-11-16T00:00:00Z')`); err != nil {
		t.Fatalf("pending call: %v", err)
	}
	return dsn
}

func TestRenameSessionCarriesTheHistory(t *testing.T) {
	ctx := context.Background()
	dsn := renameFixture(t)
	if err := RenameSession(dsn, "canonical", "scout-canonical"); err != nil {
		t.Fatalf("RenameSession: %v", err)
	}
	st, err := Open(dsn)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer st.Close()

	if _, err := st.GetSession(ctx, "canonical"); err == nil {
		t.Error("the old id still answers - the rename left a second row behind")
	}
	sess, err := st.GetSession(ctx, "scout-canonical")
	if err != nil {
		t.Fatalf("GetSession(new) = %v", err)
	}
	if sess.Label != "canonical" {
		t.Errorf("label = %q, want the untouched label", sess.Label)
	}
	msgs, err := st.GetMessages(ctx, "scout-canonical")
	if err != nil {
		t.Fatalf("GetMessages(new) = %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("messages after rename = %d, want 2 (the history has to move with the id)", len(msgs))
	}
	// The fork follows.
	fork, err := st.GetSession(ctx, "canonical-fork")
	if err != nil {
		t.Fatalf("GetSession(fork) = %v", err)
	}
	if fork.ParentID != "scout-canonical" {
		t.Errorf("fork parent = %q, want the renamed id", fork.ParentID)
	}
	// The pending call follows.
	var n int
	if err := st.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pending_calls WHERE session_id = 'scout-canonical'`).Scan(&n); err != nil {
		t.Fatalf("pending calls: %v", err)
	}
	if n != 1 {
		t.Errorf("pending calls under the new id = %d, want 1", n)
	}
	// The label index follows: the standalone fts table keys on
	// session_id and no trigger fires on an id change.
	var fts int
	if err := st.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sessions_fts WHERE session_id = 'scout-canonical'`).Scan(&fts); err != nil {
		t.Fatalf("sessions_fts: %v", err)
	}
	if fts != 1 {
		t.Errorf("sessions_fts rows for the new id = %d, want 1", fts)
	}
	if err := st.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sessions_fts WHERE session_id = 'canonical'`).Scan(&fts); err != nil {
		t.Fatalf("sessions_fts old: %v", err)
	}
	if fts != 0 {
		t.Errorf("sessions_fts still carries the old id (%d rows)", fts)
	}
	// The message index agrees with the table.
	var hits int
	if err := st.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM messages_fts WHERE messages_fts MATCH 'anomalies'`).Scan(&hits); err != nil {
		t.Fatalf("messages_fts: %v", err)
	}
	if hits != 1 {
		t.Errorf("message search hits = %d, want 1 after the rename", hits)
	}
}

func TestRenameSessionNoOps(t *testing.T) {
	ctx := context.Background()
	dsn := renameFixture(t)

	// A missing file is never created by a rename.
	missing := filepath.Join(t.TempDir(), "never-ran", "omega.db")
	if err := RenameSession(missing, "canonical", "scout-canonical"); err != nil {
		t.Fatalf("RenameSession(missing file) = %v, want nil", err)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Error("the rename created a store file for a home that never ran")
	}
	// An absent old id is the already-migrated case.
	if err := RenameSession(dsn, "ghost", "scout-canonical"); err != nil {
		t.Fatalf("RenameSession(absent id) = %v, want nil", err)
	}
	// An occupied target is never merged into.
	st, err := Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := st.CreateSession(ctx, "scout-canonical", "", ""); err != nil {
		t.Fatalf("create target: %v", err)
	}
	st.Close()
	if err := RenameSession(dsn, "canonical", "scout-canonical"); err != nil {
		t.Fatalf("RenameSession(occupied target) = %v, want nil", err)
	}
	st, err = Open(dsn)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer st.Close()
	if _, err := st.GetSession(ctx, "canonical"); err != nil {
		t.Errorf("the source row vanished into an occupied target: %v", err)
	}
	// Idempotent: the second run of a completed rename changes nothing.
	if err := RenameSession(dsn, "canonical", "scout-canonical-2"); err != nil {
		t.Fatalf("RenameSession(second) = %v", err)
	}
	if _, err := st.GetSession(ctx, "scout-canonical-2"); err != nil {
		t.Fatalf("second rename did not land: %v", err)
	}
	if err := RenameSession(dsn, "scout-canonical-2", "scout-canonical-2"); err != nil {
		t.Errorf("RenameSession(same id) = %v, want nil", err)
	}
}
