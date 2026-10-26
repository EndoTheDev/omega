package store

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

// TestSessionContextRoundTrip pins the model + workspace setters (card
// t_9de5b009): a value written on a session row survives reopen, and a
// setter on a missing row does not panic.
func TestSessionContextRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/rt.db"
	s, err := Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	ctx := context.Background()
	if err := s.CreateSession(ctx, "s1", "", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := s.SetSessionModel(ctx, "s1", "glm-5.3"); err != nil {
		t.Fatalf("set model: %v", err)
	}
	if err := s.SetSessionWorkspace(ctx, "s1", t.TempDir()); err != nil {
		t.Fatalf("set workspace: %v", err)
	}
	s.Close()

	// Reopen from disk: both fields restore.
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer s2.Close()
	sess, err := s2.GetSession(ctx, "s1")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if sess.Model != "glm-5.3" {
		t.Errorf("model round-trip: got %q, want \"glm-5.3\"", sess.Model)
	}
	if sess.Workspace == "" {
		t.Errorf("workspace round-trip: got empty, want the set folder")
	}

	// Clear paths: empty strings overwrite the row values.
	if err := s2.SetSessionModel(ctx, "s1", ""); err != nil {
		t.Fatalf("clear model: %v", err)
	}
	if err := s2.SetSessionWorkspace(ctx, "s1", ""); err != nil {
		t.Fatalf("clear workspace: %v", err)
	}
	sess, err = s2.GetSession(ctx, "s1")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if sess.Model != "" || sess.Workspace != "" {
		t.Errorf("clear: got model %q workspace %q, want both empty", sess.Model, sess.Workspace)
	}

	// A setter on a nonexistent row is a silent no-op (same contract
	// as the pinned/archived setters - rows only).
	if err := s2.SetSessionModel(ctx, "gone", "m"); err != nil {
		t.Errorf("set model on missing row: %v (want nil)", err)
	}
	if err := s2.SetSessionWorkspace(ctx, "gone", "/x"); err != nil {
		t.Errorf("set workspace on missing row: %v (want nil)", err)
	}
}

// TestSessionContextDefaultsEmpty pins the legacy contract: sessions
// created before the session-context columns existed (and every session
// since) boot with empty model and workspace - today's behavior.
func TestSessionContextDefaultsEmpty(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, "legacy", "", "label"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	sess, err := s.GetSession(ctx, "legacy")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if sess.Model != "" || sess.Workspace != "" {
		t.Errorf("fresh session: model %q workspace %q, want both empty", sess.Model, sess.Workspace)
	}
}

// TestSessionContextMigration pins the schema migration: a database
// created by an OLDER schema (no model / workspace columns) opens, and
// the columns land empty - no rewrite, no loss.
func TestSessionContextMigration(t *testing.T) {
	path := t.TempDir() + "/old.db"
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	// The pre-session-context sessions table (every column up to
	// last_run - the layout the model/workspace migration upgrades).
	_, err = db.Exec(`CREATE TABLE sessions (
	id         TEXT PRIMARY KEY,
	parent_id  TEXT REFERENCES sessions(id) ON DELETE CASCADE,
	label      TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	prompt_tokens INTEGER NOT NULL DEFAULT 0,
	completion_tokens INTEGER NOT NULL DEFAULT 0,
	pinned INTEGER NOT NULL DEFAULT 0,
	archived INTEGER NOT NULL DEFAULT 0,
	title_state INTEGER NOT NULL DEFAULT 0,
	last_read_at TEXT NOT NULL DEFAULT '',
	agent TEXT NOT NULL DEFAULT 'default',
	last_run TEXT NOT NULL DEFAULT ''
)`)
	if err != nil {
		t.Fatalf("create legacy table: %v", err)
	}
	now := "2026-10-26T00:00:00Z"
	if _, err := db.Exec(`INSERT INTO sessions (id, label, created_at, updated_at, last_read_at) VALUES ('old1', 'L', ?, ?, ?)`, now, now, now); err != nil {
		t.Fatalf("insert legacy row: %v", err)
	}
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatalf("open store on legacy db: %v", err)
	}
	defer s.Close()
	ctx := context.Background()
	sess, err := s.GetSession(ctx, "old1")
	if err != nil {
		t.Fatalf("get legacy session: %v", err)
	}
	if sess.Model != "" || sess.Workspace != "" {
		t.Errorf("migrated row: model %q workspace %q, want both empty", sess.Model, sess.Workspace)
	}
	if err := s.SetSessionModel(ctx, "old1", "glm-5.3"); err != nil {
		t.Fatalf("set model on migrated row: %v", err)
	}
}