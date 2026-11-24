package store

// legacy_migrate_test.go: the one-way move out of the SQLite store. The contract is the
// read-back gate: a session counts as migrated only when the files read back identical to
// the rows they came from, and the old store is archived as ONE dated .bak only once
// every session passes.

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeLegacyStore builds a pre-JSONL store by hand: the old schema, before the JSONL
// layout and without the watermark table.
func writeLegacyStore(t *testing.T, dsn string) {
	t.Helper()
	legacy, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	defer legacy.Close()
	_, err = legacy.Exec(`
CREATE TABLE sessions (
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
	last_run TEXT NOT NULL DEFAULT '',
	model TEXT NOT NULL DEFAULT '',
	provider TEXT NOT NULL DEFAULT '',
	workspace TEXT NOT NULL DEFAULT '',
	summary TEXT NOT NULL DEFAULT '',
	listed    INTEGER NOT NULL DEFAULT 1,
	context_start   TEXT NOT NULL DEFAULT '',
	context_summary TEXT NOT NULL DEFAULT ''
);
CREATE TABLE messages (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
	role       TEXT NOT NULL,
	payload    TEXT NOT NULL,
	created_at TEXT NOT NULL
);
CREATE TABLE image_captions (image_id TEXT PRIMARY KEY, caption TEXT NOT NULL, created_at TEXT NOT NULL);
CREATE TABLE pending_calls (
	session_id TEXT NOT NULL, call_id TEXT NOT NULL, tool_name TEXT NOT NULL,
	arguments TEXT NOT NULL DEFAULT '', dispatched_at TEXT NOT NULL,
	PRIMARY KEY (session_id, call_id)
);`)
	if err != nil {
		t.Fatalf("legacy schema: %v", err)
	}
	rows := []struct {
		stmt string
		args []any
	}{
		{`INSERT INTO sessions (id, label, created_at, updated_at, prompt_tokens, completion_tokens, pinned, title_state, last_read_at, agent, model, provider, workspace, summary, listed, context_start, context_summary)
		  VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			[]any{"old-root", "pi setup", "2026-11-01T10:00:00Z", "2026-11-02T11:30:00Z", 120, 30, 1, 2,
				"2026-11-02T11:30:00Z", "default", "claude-sonnet", "primary", "/home/endo", "a pi bring-up", 1, "3", "compacted head"}},
		{`INSERT INTO sessions (id, parent_id, label, created_at, updated_at, title_state)
		  VALUES (?, ?, ?, ?, ?, ?)`,
			[]any{"old-child", "old-root", "child of setup", "2026-11-02T11:31:00Z", "2026-11-02T11:31:00Z", 0}},
		{`INSERT INTO messages (session_id, role, payload, created_at) VALUES (?, ?, ?, ?)`,
			[]any{"old-root", "user", `{"content":"does the pi boot","timestamp":"2026-11-01T10:00:00Z"}`, "2026-11-01T10:00:00Z"}},
		{`INSERT INTO messages (session_id, role, payload, created_at) VALUES (?, ?, ?, ?)`,
			[]any{"old-root", "assistant", `{"content":"it boots now","timestamp":"2026-11-01T10:01:00Z","parent_id":"1"}`, "2026-11-01T10:01:00Z"}},
		{`INSERT INTO messages (session_id, role, payload, created_at) VALUES (?, ?, ?, ?)`,
			[]any{"old-child", "user", `{"content":"child asks about syncthing","timestamp":"2026-11-02T11:31:00Z"}`, "2026-11-02T11:31:00Z"}},
		{`INSERT INTO image_captions (image_id, caption, created_at) VALUES (?, ?, ?)`,
			[]any{"abc123", "a screenshot of the pairing dialog", "2026-11-01T10:00:00Z"}},
		{`INSERT INTO pending_calls (session_id, call_id, tool_name, arguments, dispatched_at) VALUES (?, ?, ?, ?, ?)`,
			[]any{"old-root", "call-9", "shell.run", `{"cmd":"uname -a"}`, "2026-11-02T11:00:00Z"}},
	}
	for _, row := range rows {
		if _, err := legacy.Exec(row.stmt, row.args...); err != nil {
			t.Fatalf("seed legacy store: %v", err)
		}
	}
}

func TestLegacyStoreMigratesWithReadBackAndArchivesOnce(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "legacy.db")
	writeLegacyStore(t, dsn)
	ctx := context.Background()

	s, err := Open(dsn)
	if err != nil {
		t.Fatalf("open legacy store: %v", err)
	}
	defer s.Close()

	// Every session became a file, beside the index.
	for _, id := range []string{"old-root", "old-child"} {
		path, err := s.sessionPath(id)
		if err != nil {
			t.Fatalf("session path %s: %v", id, err)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("session file %s: %v", path, err)
		}
	}

	// The rows read back exactly: metadata, lineage, tokens, and the payload bytes.
	root, err := s.GetSession(ctx, "old-root")
	if err != nil {
		t.Fatalf("get migrated session: %v", err)
	}
	if root.Label != "pi setup" || !root.Pinned || root.TitleState != 2 ||
		root.PromptTokens != 120 || root.CompletionTokens != 30 ||
		root.Model != "claude-sonnet" || root.ProviderName != "primary" ||
		root.Workspace != "/home/endo" || root.Summary != "a pi bring-up" ||
		root.ContextStart != "3" || root.ContextSummary != "compacted head" {
		t.Fatalf("migrated metadata drifted: %+v", root)
	}
	child, err := s.GetSession(ctx, "old-child")
	if err != nil {
		t.Fatalf("get migrated child: %v", err)
	}
	if child.ParentID != "old-root" {
		t.Fatalf("child lineage = %q, want old-root", child.ParentID)
	}
	msgs, err := s.GetMessages(ctx, "old-root")
	if err != nil {
		t.Fatalf("get migrated messages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("migrated messages = %d, want 2", len(msgs))
	}
	// Byte-identical payloads: the assistant line's parent pointer still names row 1.
	folded, err := foldFile(filepath.Join(s.sessionsDir, "old-root.jsonl"))
	if err != nil {
		t.Fatalf("fold migrated file: %v", err)
	}
	wantPayload := `{"content":"it boots now","timestamp":"2026-11-01T10:01:00Z","parent_id":"1"}`
	if got := string(folded.messages[1].Payload); got != wantPayload {
		t.Fatalf("payload read back = %s, want %s", got, wantPayload)
	}

	// The auxiliary tables came forward: captions and pending calls are not session
	// content and must not be lost to the archive.
	if caption, ok, err := s.GetImageCaption(ctx, "abc123"); err != nil || !ok || caption == "" {
		t.Fatalf("caption carried: %q ok=%v err=%v", caption, ok, err)
	}
	if pending := s.All(); len(pending) != 1 || pending[0].CallID != "call-9" {
		t.Fatalf("pending calls carried = %+v", pending)
	}

	// ONE dated archive beside the index.
	backs, err := filepath.Glob(dsn + ".*.bak")
	if err != nil {
		t.Fatalf("glob archives: %v", err)
	}
	if len(backs) != 1 {
		t.Fatalf("archives = %v, want exactly one dated .bak", backs)
	}
	stamp := time.Now().UTC().Format("2006-01-02")
	if !strings.HasSuffix(backs[0], "."+stamp+".bak") && !strings.Contains(backs[0], "."+stamp) {
		t.Fatalf("archive %s is not dated %s", backs[0], stamp)
	}
	// Search works off the rebuilt index.
	results, err := s.SearchSessions(ctx, "syncthing")
	if err != nil {
		t.Fatalf("search after migration: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("migrated content is not searchable")
	}

	// A second open does not migrate again: already-migrated stores are index-era.
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	s2, err := Open(dsn)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	backs, err = filepath.Glob(dsn + ".*.bak")
	if err != nil {
		t.Fatalf("glob archives: %v", err)
	}
	if len(backs) != 1 {
		t.Fatalf("archives after a second open = %v, want still one", backs)
	}
}

// TestLegacyMigrationKeepsARowWhoseUpdatedAtPredatesItsLastMessage: an old writer did not
// refresh sessions.updated_at when it appended messages, so a real row can carry a stamp
// from creation while its messages run hours later. The migrated file must fold back to
// the ROW's stamp - the read-back gate compares it byte for byte, and a file that
// upgrades it to the last message's time kills the boot.
func TestLegacyMigrationKeepsARowWhoseUpdatedAtPredatesItsLastMessage(t *testing.T) {
	const (
		id      = "72863811adb93cd5ca47642c9b8393f6"
		stamp   = "2026-10-12T19:45:35.496622592Z"
		lastMsg = "2026-10-12T23:07:44.038102813Z"
	)
	dsn := filepath.Join(t.TempDir(), "prod.db")
	writeLegacyStore(t, dsn)
	seed, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("reopen legacy db: %v", err)
	}
	for _, stmt := range []struct {
		q    string
		args []any
	}{
		{`INSERT INTO sessions (id, created_at, updated_at) VALUES (?, ?, ?)`, []any{id, stamp, stamp}},
		{`INSERT INTO messages (session_id, role, payload, created_at) VALUES (?, ?, ?, ?)`,
			[]any{id, "user", `{"content":"after the row was written"}`, lastMsg}},
	} {
		if _, err := seed.Exec(stmt.q, stmt.args...); err != nil {
			t.Fatalf("seed prod-shaped session: %v", err)
		}
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("close seed handle: %v", err)
	}

	ctx := context.Background()
	s, err := Open(dsn)
	if err != nil {
		t.Fatalf("the migration refused a store whose row stamp predates its messages: %v", err)
	}
	defer s.Close()

	got, err := s.GetSession(ctx, id)
	if err != nil {
		t.Fatalf("get migrated session: %v", err)
	}
	if got.UpdatedAt != stamp {
		t.Fatalf("index updated_at = %q, want the row's stamp %q", got.UpdatedAt, stamp)
	}
	f, err := foldFile(filepath.Join(s.sessionsDir, id+".jsonl"))
	if err != nil {
		t.Fatalf("fold migrated file: %v", err)
	}
	if f.session.UpdatedAt != stamp {
		t.Fatalf("file folds to updated_at %q, want the row's stamp %q", f.session.UpdatedAt, stamp)
	}
	// The message keeps its own timestamp: the row's stamp is not allowed to rewrite it.
	if len(f.messages) != 1 || f.messages[0].CreatedAt != lastMsg {
		t.Fatalf("migrated messages = %+v, want one line created at %s", f.messages, lastMsg)
	}
}

// TestLegacyMigrationRefusesToHalfMigrate: a verification failure leaves the old store
// untouched and no session file behind.
func TestLegacyMigrationRefusesToHalfMigrate(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "bad.db")
	legacy, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := legacy.Exec(`
CREATE TABLE sessions (id TEXT PRIMARY KEY, created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE messages (id INTEGER PRIMARY KEY AUTOINCREMENT, session_id TEXT NOT NULL, role TEXT NOT NULL, payload TEXT NOT NULL, created_at TEXT NOT NULL);`); err != nil {
		t.Fatalf("schema: %v", err)
	}
	// A session id that cannot become a file name: the migration must refuse the whole
	// store rather than silently drop the session.
	if _, err := legacy.Exec(`INSERT INTO sessions (id, created_at, updated_at) VALUES ('../escape', '2026-11-01T00:00:00Z', '2026-11-01T00:00:00Z')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatalf("close legacy: %v", err)
	}

	if _, err := Open(dsn); err == nil {
		t.Fatal("open accepted a store it cannot migrate")
	}
	if _, err := os.Stat(dsn); err != nil {
		t.Fatalf("the source store must survive a refused migration: %v", err)
	}
	backs, err := filepath.Glob(dsn + ".*.bak")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(backs) != 0 {
		t.Fatalf("a refused migration archived the store: %v", backs)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dsn), sessionsDirName, "../escape.jsonl")); err == nil {
		t.Fatal("a refused migration wrote a session file outside the sessions directory")
	}
}
