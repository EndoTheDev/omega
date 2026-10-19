package store

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/ai"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestCreateAndGetSession(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.CreateSession(ctx, "s1", "", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}
	sess, err := s.GetSession(ctx, "s1")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if sess.ID != "s1" {
		t.Fatalf("id = %q, want s1", sess.ID)
	}
	if sess.CreatedAt == "" || sess.UpdatedAt == "" {
		t.Fatalf("timestamps not set: %+v", sess)
	}
	if sess.ParentID != "" || sess.Label != "" {
		t.Fatalf("new session should have empty parent and label: %+v", sess)
	}
}

func TestGetSessionNotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetSession(context.Background(), "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestCreateSessionDuplicate(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, "s1", "", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := s.CreateSession(ctx, "s1", "", ""); err == nil {
		t.Fatalf("duplicate create should error")
	}
}

func TestListSessions(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for _, id := range []string{"a", "b", "c"} {
		if err := s.CreateSession(ctx, id, "", ""); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}
	sessions, err := s.ListSessions(ctx)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 3 {
		t.Fatalf("len = %d, want 3", len(sessions))
	}
}

func TestDeleteSession(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, "s1", "", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := s.DeleteSession(ctx, "s1"); err != nil {
		t.Fatalf("delete session: %v", err)
	}
	if _, err := s.GetSession(ctx, "s1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound after delete", err)
	}
}

func TestBranchSession(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, "root", "", ""); err != nil {
		t.Fatalf("create root: %v", err)
	}
	if err := s.CreateSession(ctx, "child", "root", ""); err != nil {
		t.Fatalf("branch: %v", err)
	}
	child, err := s.GetSession(ctx, "child")
	if err != nil {
		t.Fatalf("get child: %v", err)
	}
	if child.ParentID != "root" {
		t.Fatalf("child parent = %q, want root", child.ParentID)
	}
	// Branching from a missing parent must fail.
	if err := s.CreateSession(ctx, "orphan", "missing", ""); err == nil {
		t.Fatal("branch from missing parent should error")
	}
}

func TestSetLabel(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, "s1", "", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := s.UpdateSession(ctx, "s1", "my label"); err != nil {
		t.Fatalf("set label: %v", err)
	}
	sess, err := s.GetSession(ctx, "s1")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if sess.Label != "my label" {
		t.Fatalf("label = %q, want my label", sess.Label)
	}
	// Empty label clears it.
	if err := s.UpdateSession(ctx, "s1", ""); err != nil {
		t.Fatalf("clear label: %v", err)
	}
	sess, err = s.GetSession(ctx, "s1")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if sess.Label != "" {
		t.Fatalf("label = %q, want empty after clear", sess.Label)
	}
}

func TestGetSessionTree(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	// root -> child -> grandchild, plus a second root.
	if err := s.CreateSession(ctx, "root", "", ""); err != nil {
		t.Fatalf("create root: %v", err)
	}
	if err := s.CreateSession(ctx, "child", "root", ""); err != nil {
		t.Fatalf("create child: %v", err)
	}
	if err := s.CreateSession(ctx, "grand", "child", ""); err != nil {
		t.Fatalf("create grand: %v", err)
	}
	if err := s.CreateSession(ctx, "other", "", ""); err != nil {
		t.Fatalf("create other: %v", err)
	}
	roots, err := s.GetSessionTree(ctx)
	if err != nil {
		t.Fatalf("get tree: %v", err)
	}
	if len(roots) != 2 {
		t.Fatalf("roots = %d, want 2", len(roots))
	}
	// Find the root with a child.
	var root *agent.SessionNode
	for _, r := range roots {
		if r.ID == "root" {
			root = r
		}
	}
	if root == nil {
		t.Fatal("root session missing from tree")
	}
	if len(root.Children) != 1 || root.Children[0].ID != "child" {
		t.Fatalf("root children = %v, want [child]", root.Children)
	}
	if len(root.Children[0].Children) != 1 || root.Children[0].Children[0].ID != "grand" {
		t.Fatalf("child children = %v, want [grand]", root.Children[0].Children)
	}
}

func TestDeleteSessionCascadesToChildren(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, "root", "", ""); err != nil {
		t.Fatalf("create root: %v", err)
	}
	if err := s.CreateSession(ctx, "child", "root", ""); err != nil {
		t.Fatalf("create child: %v", err)
	}
	if err := s.DeleteSession(ctx, "root"); err != nil {
		t.Fatalf("delete root: %v", err)
	}
	if _, err := s.GetSession(ctx, "child"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("child err = %v, want ErrNotFound after parent delete", err)
	}
}

func TestGetAncestorMessages(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, "root", "", ""); err != nil {
		t.Fatalf("create root: %v", err)
	}
	if err := s.CreateSession(ctx, "child", "root", ""); err != nil {
		t.Fatalf("create child: %v", err)
	}
	if err := s.AppendMessage(ctx, "root", ai.NewUser("root msg")); err != nil {
		t.Fatalf("append root: %v", err)
	}
	if err := s.AppendMessage(ctx, "child", ai.NewUser("child msg")); err != nil {
		t.Fatalf("append child: %v", err)
	}
	got, err := s.GetAncestorMessages(ctx, "child")
	if err != nil {
		t.Fatalf("get ancestor messages: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	assertUser(t, got[0], "root msg")
	assertUser(t, got[1], "child msg")
}

func TestAppendAndGetMessages(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, "s1", "", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}

	messages := []ai.Message{
		ai.NewSystem("you are helpful"),
		ai.NewUser("hello"),
		ai.NewAssistant("hi there"),
		ai.NewToolResult("ok", "call_1", false),
	}
	for _, m := range messages {
		if err := s.AppendMessage(ctx, "s1", m); err != nil {
			t.Fatalf("append message: %v", err)
		}
	}

	got, err := s.GetMessages(ctx, "s1")
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(got) != len(messages) {
		t.Fatalf("len = %d, want %d", len(got), len(messages))
	}

	// Verify each round-trips to the same concrete type and content.
	assertUser(t, got[1], "hello")
	assertAssistant(t, got[2], "hi there")
	assertToolResult(t, got[3], "ok", "call_1", false)
}

func TestGetMessagesWithTimes(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, "s1", "", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}
	messages := []ai.Message{
		ai.NewUser("hello"),
		ai.NewAssistant("hi there"),
	}
	for _, m := range messages {
		if err := s.AppendMessage(ctx, "s1", m); err != nil {
			t.Fatalf("append message: %v", err)
		}
	}

	got, err := s.GetMessagesWithTimes(ctx, "s1")
	if err != nil {
		t.Fatalf("get messages with times: %v", err)
	}
	if len(got) != len(messages) {
		t.Fatalf("len = %d, want %d", len(got), len(messages))
	}
	for i, sm := range got {
		if sm.CreatedAt == "" {
			t.Fatalf("got[%d].CreatedAt empty, want the stored created_at", i)
		}
		if _, err := time.Parse(time.RFC3339Nano, sm.CreatedAt); err != nil {
			t.Fatalf("got[%d].CreatedAt = %q, not RFC3339: %v", i, sm.CreatedAt, err)
		}
	}
	assertUser(t, got[0].Message, "hello")
	assertAssistant(t, got[1].Message, "hi there")

	if times, err := s.GetMessagesWithTimes(ctx, "missing"); err != nil || times != nil {
		t.Fatalf("missing session = (%v, %v), want (nil, nil)", times, err)
	}
}

func TestMessagesPersistAcrossStoreReopen(t *testing.T) {
	// A file-backed store proves persistence across Close/Open.
	dir := t.TempDir()
	dsn := dir + "/test.db"

	s1, err := Open(dsn)
	if err != nil {
		t.Fatalf("open store 1: %v", err)
	}
	ctx := context.Background()
	if err := s1.CreateSession(ctx, "s1", "", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := s1.AppendMessage(ctx, "s1", ai.NewUser("persist me")); err != nil {
		t.Fatalf("append message: %v", err)
	}
	s1.Close()

	s2, err := Open(dsn)
	if err != nil {
		t.Fatalf("open store 2: %v", err)
	}
	defer s2.Close()
	got, err := s2.GetMessages(ctx, "s1")
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	assertUser(t, got[0], "persist me")
}

func TestDeleteSessionCascadesMessages(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, "s1", "", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := s.AppendMessage(ctx, "s1", ai.NewUser("bye")); err != nil {
		t.Fatalf("append message: %v", err)
	}
	if err := s.DeleteSession(ctx, "s1"); err != nil {
		t.Fatalf("delete session: %v", err)
	}
	got, err := s.GetMessages(ctx, "s1")
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("len = %d, want 0 after cascade", len(got))
	}
}

func TestCountMessages(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, "s1", "", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if n, err := s.CountMessages(ctx, "s1"); err != nil || n != 0 {
		t.Fatalf("count empty = %d, err %v; want 0", n, err)
	}
	for _, m := range []ai.Message{ai.NewUser("a"), ai.NewAssistant("b"), ai.NewUser("c")} {
		if err := s.AppendMessage(ctx, "s1", m); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	if n, err := s.CountMessages(ctx, "s1"); err != nil || n != 3 {
		t.Fatalf("count = %d, err %v; want 3", n, err)
	}
}

// TestSessionCounts covers the read-time counts: sessions with N
// messages and M tool results report exactly those numbers, bookkeeping
// entries are excluded from message_count, and empty sessions stay out
// of the map.
func TestSessionCounts(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for _, id := range []string{"s1", "s2"} {
		if err := s.CreateSession(ctx, id, "", ""); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}
	// s1: 5 stored messages (2 user, 1 assistant, 2 tool results) + 2
	// bookkeeping entries -> message_count 5, tool_count 2.
	for _, m := range []ai.Message{
		ai.NewUser("a"),
		ai.NewAssistant("b"),
		ai.ToolResult{Content: "t1"},
		ai.ToolResult{Content: "t2"},
		ai.NewUser("c"),
		ai.ModelChange{Model: "m"},
		ai.ThinkingLevelChange{Level: "high"},
	} {
		if err := s.AppendMessage(ctx, "s1", m); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	counts, err := s.SessionCounts(ctx)
	if err != nil {
		t.Fatalf("session counts: %v", err)
	}
	got := counts["s1"]
	if got.MessageCount != 5 || got.ToolCount != 2 {
		t.Fatalf("s1 = %+v, want 5 messages / 2 tools", got)
	}
	if _, ok := counts["s2"]; ok {
		t.Fatalf("empty session s2 should be absent, got %+v", counts["s2"])
	}
}

// TestMigrationAddsTokenColumns upgrades a pre-migration database and
// verifies the new totals default to 0 instead of breaking on load.
func TestMigrationAddsTokenColumns(t *testing.T) {
	dir := t.TempDir()
	dsn := dir + "/old.db"
	ctx := context.Background()

	// Build the pre-migration schema by hand.
	pre, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open pre-migration db: %v", err)
	}
	_, err = pre.Exec(`
CREATE TABLE IF NOT EXISTS sessions (
	id         TEXT PRIMARY KEY,
	parent_id  TEXT REFERENCES sessions(id) ON DELETE CASCADE,
	label      TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS messages (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
	role       TEXT NOT NULL,
	payload    TEXT NOT NULL,
	created_at TEXT NOT NULL
);`)
	if err != nil {
		t.Fatalf("create pre-migration schema: %v", err)
	}
	if _, err := pre.Exec(`INSERT INTO sessions (id, label, created_at, updated_at) VALUES ('old', '', '2026-01-01', '2026-01-01')`); err != nil {
		t.Fatalf("insert old session: %v", err)
	}
	if _, err := pre.Exec(`INSERT INTO messages (session_id, role, payload, created_at) VALUES ('old', 'user', '{"content":"hi"}', '2026-01-01')`); err != nil {
		t.Fatalf("insert old message: %v", err)
	}
	if err := pre.Close(); err != nil {
		t.Fatalf("close pre-migration db: %v", err)
	}

	// Opening through the store migrates in place.
	s, err := Open(dsn)
	if err != nil {
		t.Fatalf("open post-migration: %v", err)
	}
	defer s.Close()
	sess, err := s.GetSession(ctx, "old")
	if err != nil {
		t.Fatalf("get old session: %v", err)
	}
	if sess.PromptTokens != 0 || sess.CompletionTokens != 0 {
		t.Fatalf("migrated totals = %d/%d, want 0/0", sess.PromptTokens, sess.CompletionTokens)
	}
	msgs, err := s.GetMessages(ctx, "old")
	if err != nil {
		t.Fatalf("get old messages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("len = %d, want 1", len(msgs))
	}

	// Fresh sessions also default to 0 and accept token adds.
	if err := s.AddSessionTokens(ctx, "old", 12, 3); err != nil {
		t.Fatalf("add tokens: %v", err)
	}
	sess, err = s.GetSession(ctx, "old")
	if err != nil {
		t.Fatalf("reget: %v", err)
	}
	if sess.PromptTokens != 12 || sess.CompletionTokens != 3 {
		t.Fatalf("totals = %d/%d, want 12/3", sess.PromptTokens, sess.CompletionTokens)
	}
	if err := s.AddSessionTokens(ctx, "old", 8, 0); err != nil {
		t.Fatalf("add tokens 2: %v", err)
	}
	sess, err = s.GetSession(ctx, "old")
	if err != nil {
		t.Fatalf("reget: %v", err)
	}
	if sess.PromptTokens != 20 || sess.CompletionTokens != 3 {
		t.Fatalf("accumulated totals = %d/%d, want 20/3", sess.PromptTokens, sess.CompletionTokens)
	}
	// Zero usage is a no-op.
	if err := s.AddSessionTokens(ctx, "old", 0, 0); err != nil {
		t.Fatalf("add zero tokens: %v", err)
	}
}

// TestMigrationAddsPinnedColumn upgrades a database that predates the
// pinned flag and verifies old sessions come through unpinned, then
// covers the pin/unpin round-trip.
func TestMigrationAddsPinnedColumn(t *testing.T) {
	dir := t.TempDir()
	dsn := dir + "/old.db"
	ctx := context.Background()

	pre, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open pre-migration db: %v", err)
	}
	_, err = pre.Exec(`
CREATE TABLE IF NOT EXISTS sessions (
	id         TEXT PRIMARY KEY,
	parent_id  TEXT REFERENCES sessions(id) ON DELETE CASCADE,
	label      TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);`)
	if err != nil {
		t.Fatalf("create pre-migration schema: %v", err)
	}
	if _, err := pre.Exec(`INSERT INTO sessions (id, label, created_at, updated_at) VALUES ('old', '', '2026-01-01', '2026-01-01')`); err != nil {
		t.Fatalf("insert old session: %v", err)
	}
	if err := pre.Close(); err != nil {
		t.Fatalf("close pre-migration db: %v", err)
	}

	// Opening through the store migrates in place; old sessions default
	// to unpinned.
	s, err := Open(dsn)
	if err != nil {
		t.Fatalf("open post-migration: %v", err)
	}
	defer s.Close()
	before, err := s.GetSession(ctx, "old")
	if err != nil {
		t.Fatalf("get old session: %v", err)
	}
	if before.Pinned {
		t.Fatalf("migrated session pinned = true, want false")
	}
	updatedAt := before.UpdatedAt

	// Pin round-trip: set, list, unset. updated_at must not move - the
	// time buckets read it, and pinning is not activity.
	if err := s.SetSessionPinned(ctx, "old", true); err != nil {
		t.Fatalf("pin: %v", err)
	}
	pinned, err := s.GetSession(ctx, "old")
	if err != nil {
		t.Fatalf("get pinned: %v", err)
	}
	if !pinned.Pinned {
		t.Fatalf("pinned = false after pin, want true")
	}
	if pinned.UpdatedAt != updatedAt {
		t.Fatalf("pin moved updated_at: %q -> %q", updatedAt, pinned.UpdatedAt)
	}
	list, err := s.ListSessions(ctx)
	if err != nil || len(list) != 1 || !list[0].Pinned {
		t.Fatalf("list = %+v err %v, want one pinned session", list, err)
	}
	if err := s.SetSessionPinned(ctx, "old", false); err != nil {
		t.Fatalf("unpin: %v", err)
	}
	if after, _ := s.GetSession(ctx, "old"); after.Pinned {
		t.Fatalf("pinned = true after unpin, want false")
	}
}

// TestMigrationAddsArchivedColumn upgrades a database that predates
// the archived flag and verifies old sessions come through unarchived,
// then covers the archive/unarchive round-trip.
func TestMigrationAddsArchivedColumn(t *testing.T) {
	dir := t.TempDir()
	dsn := dir + "/old.db"
	ctx := context.Background()

	pre, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open pre-migration db: %v", err)
	}
	_, err = pre.Exec(`
CREATE TABLE IF NOT EXISTS sessions (
	id         TEXT PRIMARY KEY,
	parent_id  TEXT REFERENCES sessions(id) ON DELETE CASCADE,
	label      TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);`)
	if err != nil {
		t.Fatalf("create pre-migration schema: %v", err)
	}
	if _, err := pre.Exec(`INSERT INTO sessions (id, label, created_at, updated_at) VALUES ('old', '', '2026-01-01', '2026-01-01')`); err != nil {
		t.Fatalf("insert old session: %v", err)
	}
	if err := pre.Close(); err != nil {
		t.Fatalf("close pre-migration db: %v", err)
	}

	// Opening through the store migrates in place; old sessions default
	// to unarchived.
	s, err := Open(dsn)
	if err != nil {
		t.Fatalf("open post-migration: %v", err)
	}
	defer s.Close()
	before, err := s.GetSession(ctx, "old")
	if err != nil {
		t.Fatalf("get old session: %v", err)
	}
	if before.Archived {
		t.Fatalf("migrated session archived = true, want false")
	}
	updatedAt := before.UpdatedAt

	// Archive round-trip: set, get, unset. updated_at must not move -
	// the time buckets read it, and archiving is not activity.
	if err := s.SetSessionArchived(ctx, "old", true); err != nil {
		t.Fatalf("archive: %v", err)
	}
	archived, err := s.GetSession(ctx, "old")
	if err != nil {
		t.Fatalf("get archived: %v", err)
	}
	if !archived.Archived {
		t.Fatalf("archived = false after archive, want true")
	}
	if archived.UpdatedAt != updatedAt {
		t.Fatalf("archive moved updated_at: %q -> %q", updatedAt, archived.UpdatedAt)
	}
	if err := s.SetSessionArchived(ctx, "old", false); err != nil {
		t.Fatalf("unarchive: %v", err)
	}
	if after, _ := s.GetSession(ctx, "old"); after.Archived {
		t.Fatalf("archived = true after unarchive, want false")
	}
}

// TestSessionReadAndUnread covers the server-side unread state: the
// last_read_at migration stamps existing rows read, CreateSession
// births sessions read, a message landing after last_read_at flips
// unread on, and SetSessionRead clears it without touching updated_at.
func TestSessionReadAndUnread(t *testing.T) {
	dir := t.TempDir()
	dsn := dir + "/old.db"
	ctx := context.Background()

	// Pre-migration schema: sessions without last_read_at, one old row.
	pre, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open pre-migration db: %v", err)
	}
	_, err = pre.Exec(`
CREATE TABLE IF NOT EXISTS sessions (
	id         TEXT PRIMARY KEY,
	parent_id  TEXT REFERENCES sessions(id) ON DELETE CASCADE,
	label      TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);`)
	if err != nil {
		t.Fatalf("create pre-migration schema: %v", err)
	}
	if _, err := pre.Exec(`INSERT INTO sessions (id, label, created_at, updated_at) VALUES ('old', '', '2026-01-01', '2026-01-01')`); err != nil {
		t.Fatalf("insert old session: %v", err)
	}
	if err := pre.Close(); err != nil {
		t.Fatalf("close pre-migration db: %v", err)
	}

	s, err := Open(dsn)
	if err != nil {
		t.Fatalf("open post-migration: %v", err)
	}
	defer s.Close()

	// Migration data-fix: the old row's stamp is its own updated_at,
	// so everything the user already saw reads as seen.
	old, err := s.GetSession(ctx, "old")
	if err != nil {
		t.Fatalf("get old session: %v", err)
	}
	if old.LastReadAt != old.UpdatedAt {
		t.Fatalf("migrated last_read_at = %q, want updated_at %q", old.LastReadAt, old.UpdatedAt)
	}
	if agent.SessionUnread(old) {
		t.Fatalf("migrated session unread = true, want false")
	}

	// New sessions are born read; unread only comes from later output.
	if err := s.CreateSession(ctx, "s1", "", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}
	sess, err := s.GetSession(ctx, "s1")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if sess.LastReadAt == "" || sess.LastReadAt != sess.CreatedAt {
		t.Fatalf("born session last_read_at = %q, want created time %q", sess.LastReadAt, sess.CreatedAt)
	}
	if agent.SessionUnread(sess) {
		t.Fatalf("never-touched session unread = true, want false")
	}

	// A message landing after last_read_at flips unread on.
	if err := s.AppendMessage(ctx, "s1", ai.NewUser("hi")); err != nil {
		t.Fatalf("append: %v", err)
	}
	sess, err = s.GetSession(ctx, "s1")
	if err != nil {
		t.Fatalf("reget: %v", err)
	}
	if !agent.SessionUnread(sess) {
		t.Fatalf("session after new message unread = false, want true")
	}

	// SetSessionRead clears unread; reading is not activity, updated_at
	// does not move.
	updatedAt := sess.UpdatedAt
	if err := s.SetSessionRead(ctx, "s1"); err != nil {
		t.Fatalf("mark read: %v", err)
	}
	sess, err = s.GetSession(ctx, "s1")
	if err != nil {
		t.Fatalf("reget after read: %v", err)
	}
	if agent.SessionUnread(sess) {
		t.Fatalf("session after read unread = true, want false")
	}
	if sess.UpdatedAt != updatedAt {
		t.Fatalf("read moved updated_at: %q -> %q", updatedAt, sess.UpdatedAt)
	}

	// A generated title landing after the read stamp is cosmetic - the
	// title write must not touch updated_at and resurrect unread.
	if err := s.SetSessionTitle(ctx, "s1", "generated title", false); err != nil {
		t.Fatalf("set title: %v", err)
	}
	sess, err = s.GetSession(ctx, "s1")
	if err != nil {
		t.Fatalf("reget after title: %v", err)
	}
	if agent.SessionUnread(sess) {
		t.Fatalf("session unread after title write = true, want false")
	}
	if sess.UpdatedAt != updatedAt {
		t.Fatalf("title write moved updated_at: %q -> %q", updatedAt, sess.UpdatedAt)
	}
}

// TestSessionModels covers the read-time last-model map: the latest
// model_change wins, sessions without a model entry stay out of the map.
func TestSessionModels(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for _, id := range []string{"s1", "s2"} {
		if err := s.CreateSession(ctx, id, "", ""); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}
	if err := s.AppendMessage(ctx, "s1", ai.NewUser("hi")); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := s.AppendMessage(ctx, "s1", ai.NewModelChange("glm-5.2")); err != nil {
		t.Fatalf("append model change: %v", err)
	}
	if err := s.AppendMessage(ctx, "s1", ai.NewModelChange("glm-5.3-flash")); err != nil {
		t.Fatalf("append model change 2: %v", err)
	}
	if err := s.AppendMessage(ctx, "s2", ai.NewUser("untouched")); err != nil {
		t.Fatalf("append s2: %v", err)
	}
	models, err := s.SessionModels(ctx)
	if err != nil {
		t.Fatalf("session models: %v", err)
	}
	// latest model_change wins; model-less sessions are absent
	if models["s1"] != "glm-5.3-flash" {
		t.Fatalf("s1 model = %q, want glm-5.3-flash", models["s1"])
	}
	if m, ok := models["s2"]; ok {
		t.Fatalf("model-less s2 present: %q", m)
	}
}

func assertUser(t *testing.T, m ai.Message, want string) {
	t.Helper()
	u, ok := m.(ai.User)
	if !ok {
		t.Fatalf("type = %T, want ai.User", m)
	}
	if u.Content != want {
		t.Fatalf("content = %q, want %q", u.Content, want)
	}
}

func assertAssistant(t *testing.T, m ai.Message, want string) {
	t.Helper()
	a, ok := m.(ai.Assistant)
	if !ok {
		t.Fatalf("type = %T, want ai.Assistant", m)
	}
	if a.Content != want {
		t.Fatalf("content = %q, want %q", a.Content, want)
	}
}

func assertToolResult(t *testing.T, m ai.Message, wantContent, wantID string, wantErr bool) {
	t.Helper()
	tr, ok := m.(ai.ToolResult)
	if !ok {
		t.Fatalf("type = %T, want ai.ToolResult", m)
	}
	if tr.Content != wantContent || tr.ToolCallID != wantID || tr.IsError != wantErr {
		t.Fatalf("tool result = %+v, want content=%q id=%q is_error=%v",
			tr, wantContent, wantID, wantErr)
	}
}

// TestDeleteSessionCascades verifies deleting a session removes its
// messages and child branches via foreign key cascade.
func TestDeleteSessionCascades(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.CreateSession(ctx, "root", "", "root"); err != nil {
		t.Fatalf("create root: %v", err)
	}
	if err := s.CreateSession(ctx, "child", "root", "child"); err != nil {
		t.Fatalf("create child: %v", err)
	}
	if err := s.AppendMessage(ctx, "root", ai.NewUser("hi")); err != nil {
		t.Fatalf("append root: %v", err)
	}
	if err := s.AppendMessage(ctx, "child", ai.NewUser("yo")); err != nil {
		t.Fatalf("append child: %v", err)
	}

	if err := s.DeleteSession(ctx, "root"); err != nil {
		t.Fatalf("delete root: %v", err)
	}

	// Parent gone.
	if _, err := s.GetSession(ctx, "root"); err == nil {
		t.Fatal("root still exists after delete")
	}
	// Child gone (cascade).
	if _, err := s.GetSession(ctx, "child"); err == nil {
		t.Fatal("child still exists after parent delete (cascade failed)")
	}
	// Messages gone (cascade).
	msgs, err := s.GetMessages(ctx, "root")
	if err == nil && len(msgs) != 0 {
		t.Fatalf("root messages remain: %d", len(msgs))
	}
}

// TestDeleteSessionMissingIsNoOp verifies deleting a nonexistent session
// is not an error.
func TestDeleteSessionMissingIsNoOp(t *testing.T) {
	s := newTestStore(t)
	if err := s.DeleteSession(context.Background(), "nope"); err != nil {
		t.Fatalf("delete missing session: %v", err)
	}
}

func TestSearchMessages(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.CreateSession(ctx, "s1", "", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := s.AppendMessage(ctx, "s1", ai.NewUser("the quick brown fox jumps")); err != nil {
		t.Fatalf("append user: %v", err)
	}
	if err := s.AppendMessage(ctx, "s1", ai.NewAssistant("the lazy dog sleeps")); err != nil {
		t.Fatalf("append assistant: %v", err)
	}

	// Second session with different content.
	if err := s.CreateSession(ctx, "s2", "", ""); err != nil {
		t.Fatalf("create session 2: %v", err)
	}
	if err := s.AppendMessage(ctx, "s2", ai.NewUser("completely unrelated content here")); err != nil {
		t.Fatalf("append s2: %v", err)
	}

	// Search for a term only in s1.
	results, err := s.SearchMessages(ctx, "fox")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("no results for 'fox', want at least 1")
	}
	var found bool
	for _, r := range results {
		if r.SessionID == "s1" && r.Snippet != "" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no result for s1 with non-empty snippet; got %+v", results)
	}

	// Query matching nothing returns empty, no error.
	empty, err := s.SearchMessages(ctx, "nonexistentterm12345")
	if err != nil {
		t.Fatalf("search no-match: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected 0 results for no-match query, got %d", len(empty))
	}

	// Apostrophe in the term is a literal, not FTS syntax.
	if err := s.AppendMessage(ctx, "s1", ai.NewUser("what's the plan for today")); err != nil {
		t.Fatalf("append apostrophe msg: %v", err)
	}
	results, err = s.SearchMessages(ctx, "what's")
	if err != nil {
		t.Fatalf("search apostrophe: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("no results for literal apostrophe search, want >= 1")
	}

	// A term containing FTS keywords is matched literally.
	if err := s.AppendMessage(ctx, "s2", ai.NewUser("we say foo AND bar out loud")); err != nil {
		t.Fatalf("append AND msg: %v", err)
	}
	results, err = s.SearchMessages(ctx, "foo AND bar")
	if err != nil {
		t.Fatalf("search AND keyword: %v", err)
	}
	if len(results) != 1 || results[0].SessionID != "s2" {
		t.Fatalf("expected literal 'foo AND bar' to hit s2 only, got %+v", results)
	}

	// Embedded double quotes are escaped, not a syntax error.
	if err := s.AppendMessage(ctx, "s1", ai.NewUser(`the "quoted" section`)); err != nil {
		t.Fatalf("append quote msg: %v", err)
	}
	results, err = s.SearchMessages(ctx, `"quoted"`)
	if err != nil {
		t.Fatalf("search embedded quotes: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("no results for term with embedded double quotes, want >= 1")
	}
}

// helperSessionWithContent creates a session and appends one user message.
func helperSessionWithContent(t *testing.T, s *Store, id, label, content string) {
	t.Helper()
	if err := s.CreateSession(context.Background(), id, "", label); err != nil {
		t.Fatalf("create session %s: %v", id, err)
	}
	if err := s.AppendMessage(context.Background(), id, ai.NewUser(content)); err != nil {
		t.Fatalf("append message to %s: %v", id, err)
	}
}

// TestSearchSessions covers the unified search: one pass over the label
// index and the message-content index, merged to one hit per session
// with label (title) hits first. Progressive typing must never empty
// mid-word: fib -> fibo -> fibonacci each keep hitting (quoteFTS
// trailing star).
func TestSearchSessions(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// s1 matches by BOTH label and content; title must win (one hit,
	// Title=true, snippet is the label itself).
	helperSessionWithContent(t, s, "s1", "Fibonacci series",
		"why does the fibonacci sequence grow so fast")
	// s2 matches by content only.
	helperSessionWithContent(t, s, "s2", "Weather notes",
		"recursion homework: fibonacci numbers again")
	// s3 matches by label only.
	helperSessionWithContent(t, s, "s3", "Fibonacci proof sketch", "no useful terms")

	// Title hits lead, content hits follow, one hit per session.
	results, err := s.SearchSessions(ctx, "fib")
	if err != nil {
		t.Fatalf("search fib: %v", err)
	}
	ids := make([]string, 0, len(results))
	var s1Hit *agent.SearchResult
	for i, r := range results {
		ids = append(ids, r.SessionID)
		if r.SessionID == "s1" {
			if s1Hit != nil {
				t.Fatalf("s1 hit twice: %+v", results)
			}
			s1Hit = &results[i]
		}
	}
	if len(ids) != 3 {
		t.Fatalf("want 3 hits for 'fib', got %+v", results)
	}
	if ids[0] != "s1" || s1Hit == nil || !s1Hit.Title || s1Hit.Snippet != "Fibonacci series" {
		t.Fatalf("want s1 titled-label hit first, got %+v", results)
	}
	for _, r := range results {
		if r.SessionID == "s2" && r.Title {
			t.Fatalf("content-only hit mislabeled as title: %+v", results)
		}
		if r.SessionID == "s3" && !r.Title {
			t.Fatalf("label-only hit (s3) must be a title hit: %+v", results)
		}
	}

	// Progressive typing never empties mid-word.
	for _, q := range []string{"fib", "fibo", "fibonacci"} {
		hits, err := s.SearchSessions(ctx, q)
		if err != nil {
			t.Fatalf("progressive search %q: %v", q, err)
		}
		if len(hits) == 0 {
			t.Fatalf("progressive typing emptied results at %q", q)
		}
	}

	// Title-only search: "weather" finds s2 by label alone.
	hits, err := s.SearchSessions(ctx, "weather")
	if err != nil {
		t.Fatalf("title-only search: %v", err)
	}
	if len(hits) != 1 || hits[0].SessionID != "s2" || !hits[0].Title ||
		hits[0].Snippet != "Weather notes" {
		t.Fatalf("title-only search for 'weather', got %+v", hits)
	}

	// No-match query returns empty, no error.
	none, err := s.SearchSessions(ctx, "nonexistentterm12345")
	if err != nil {
		t.Fatalf("no-match search: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("want 0 hits for no-match, got %+v", none)
	}

	// Renaming the label updates the title index both ways: the new
	// label hits as a title, the old one falls back to content-only.
	if err := s.UpdateSession(ctx, "s1", "Prime numbers"); err != nil {
		t.Fatalf("rename s1: %v", err)
	}
	hits, err = s.SearchSessions(ctx, "prime")
	if err != nil {
		t.Fatalf("search renamed label: %v", err)
	}
	if len(hits) != 1 || hits[0].SessionID != "s1" || !hits[0].Title ||
		hits[0].Snippet != "Prime numbers" {
		t.Fatalf("renamed label must hit as title, got %+v", hits)
	}
	hits, err = s.SearchSessions(ctx, "fibonacci")
	if err != nil {
		t.Fatalf("search after rename: %v", err)
	}
	for _, h := range hits {
		if h.SessionID == "s1" && h.Title {
			t.Fatalf("stale label still a title hit after rename: %+v", hits)
		}
	}
}

// helperAssistantWithTools builds an Assistant message with tool calls.
func helperAssistantWithTools(content string, calls ...ai.ToolCall) ai.Assistant {
	a := ai.NewAssistant(content)
	a.ToolCalls = calls
	return a
}

func TestComputeInsightsAllTime(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Session 1: 2 user messages + 1 assistant with 2 tool calls.
	if err := s.CreateSession(ctx, "s1", "", "first session"); err != nil {
		t.Fatalf("create s1: %v", err)
	}
	if err := s.AppendMessage(ctx, "s1", ai.NewUser("hello world one")); err != nil {
		t.Fatalf("append s1 u1: %v", err)
	}
	if err := s.AppendMessage(ctx, "s1", ai.NewUser("hello world two")); err != nil {
		t.Fatalf("append s1 u2: %v", err)
	}
	if err := s.AppendMessage(ctx, "s1", helperAssistantWithTools("response one",
		ai.ToolCall{ID: "tc1", Name: "shell.run", Arguments: map[string]any{"cmd": "ls"}},
		ai.ToolCall{ID: "tc2", Name: "files.read", Arguments: map[string]any{"path": "x.go"}},
	)); err != nil {
		t.Fatalf("append s1 a1: %v", err)
	}

	// Session 2: 1 user message + 1 assistant with 1 tool call.
	if err := s.CreateSession(ctx, "s2", "", "second session"); err != nil {
		t.Fatalf("create s2: %v", err)
	}
	if err := s.AppendMessage(ctx, "s2", ai.NewUser("another message here")); err != nil {
		t.Fatalf("append s2 u1: %v", err)
	}
	if err := s.AppendMessage(ctx, "s2", helperAssistantWithTools("response two",
		ai.ToolCall{ID: "tc3", Name: "shell.run", Arguments: map[string]any{"cmd": "pwd"}},
	)); err != nil {
		t.Fatalf("append s2 a1: %v", err)
	}

	// Add a metadata entry to verify it's skipped from message counts.
	if err := s.AppendMessage(ctx, "s1", ai.NewModelChange("gpt-4o")); err != nil {
		t.Fatalf("append s1 model change: %v", err)
	}

	insights, err := s.ComputeInsights(ctx, 0) // days=0 → all time
	if err != nil {
		t.Fatalf("ComputeInsights(0): %v", err)
	}

	// Period label.
	if insights.Period != "All time" {
		t.Errorf("Period = %q, want %q", insights.Period, "All time")
	}
	if insights.Days != 0 {
		t.Errorf("Days = %d, want 0", insights.Days)
	}

	// Sessions.
	if insights.Sessions != 2 {
		t.Errorf("Sessions = %d, want 2", insights.Sessions)
	}

	// Messages: s1 has 3 conversation + 1 model_change (skipped) = 3,
	// s2 has 2, total = 5.
	if insights.Messages != 5 {
		t.Errorf("Messages = %d, want 5", insights.Messages)
	}

	// User messages: s1=2, s2=1, total=3.
	if insights.UserMessages != 3 {
		t.Errorf("UserMessages = %d, want 3", insights.UserMessages)
	}

	// Tool calls: s1=2, s2=1, total=3.
	if insights.ToolCalls != 3 {
		t.Errorf("ToolCalls = %d, want 3", insights.ToolCalls)
	}

	// Total tokens should be > 0 (content is non-empty).
	if insights.TotalTokens <= 0 {
		t.Errorf("TotalTokens = %d, want > 0", insights.TotalTokens)
	}

	// AvgSessionMsgs = 5 / 2 = 2.5.
	if insights.AvgSessionMsgs != 2.5 {
		t.Errorf("AvgSessionMsgs = %f, want 2.5", insights.AvgSessionMsgs)
	}

	// Tools list should have 2 distinct tools: shell.run (2), files.read (1).
	// Sorted by count desc → shell.run first.
	if len(insights.Tools) != 2 {
		t.Fatalf("Tools len = %d, want 2", len(insights.Tools))
	}
	// Verify sorted by count desc.
	if !sort.SliceIsSorted(insights.Tools, func(i, j int) bool {
		return insights.Tools[i].Count > insights.Tools[j].Count
	}) {
		t.Errorf("Tools not sorted by count desc: %+v", insights.Tools)
	}
	// shell.run should have count 2 (first by desc sort).
	if insights.Tools[0].Name != "shell.run" || insights.Tools[0].Count != 2 {
		t.Errorf("Tools[0] = %+v, want shell.run/2", insights.Tools[0])
	}

	// Daily activity should have 7 entries.
	for i, d := range insights.Daily {
		if d.Day == "" {
			t.Errorf("Daily[%d].Day empty", i)
		}
	}

	// Notable stats should be populated (s1 has most msgs/tokens, s2 has fewest).
	if insights.NotableMsgs.Value != 4 {
		// s1 has 3 conversation msgs + 1 model_change = 4 msgs in GetMessages,
		// but sessMsgs counts all msgs from GetMessages BEFORE the type switch
		// filter — actually the code computes sessMsgs=len(msgs) which includes
		// model_change. So s1 has 4, s2 has 2.
		t.Errorf("NotableMsgs.Value = %d, want 4", insights.NotableMsgs.Value)
	}
	if insights.NotableMsgs.Detail == "" {
		t.Error("NotableMsgs.Detail empty")
	}
	if insights.NotableTokens.Value <= 0 {
		t.Errorf("NotableTokens.Value = %d, want > 0", insights.NotableTokens.Value)
	}
	if insights.NotableTools.Value != 2 {
		// s1 has 2 tool calls (max).
		t.Errorf("NotableTools.Value = %d, want 2", insights.NotableTools.Value)
	}
}

func TestComputeInsightsFilteredByDays(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Create a session and record its creation timestamp in the past.
	if err := s.CreateSession(ctx, "old", "", "old session"); err != nil {
		t.Fatalf("create old: %v", err)
	}
	// Manually update the created_at to 10 days ago.
	tenDaysAgo := time.Now().AddDate(0, 0, -10).Format(time.RFC3339)
	if _, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET created_at = ? WHERE id = ?`, tenDaysAgo, "old"); err != nil {
		t.Fatalf("update old session time: %v", err)
	}
	if err := s.AppendMessage(ctx, "old", ai.NewUser("old message")); err != nil {
		t.Fatalf("append old: %v", err)
	}

	// Recent session (created now).
	if err := s.CreateSession(ctx, "recent", "", "recent session"); err != nil {
		t.Fatalf("create recent: %v", err)
	}
	if err := s.AppendMessage(ctx, "recent", ai.NewUser("recent message")); err != nil {
		t.Fatalf("append recent: %v", err)
	}

	// days=1 should exclude the old session.
	insights, err := s.ComputeInsights(ctx, 1)
	if err != nil {
		t.Fatalf("ComputeInsights(1): %v", err)
	}

	if insights.Period != "Last 1 days" {
		t.Errorf("Period = %q, want %q", insights.Period, "Last 1 days")
	}
	if insights.Days != 1 {
		t.Errorf("Days = %d, want 1", insights.Days)
	}
	if insights.Sessions != 1 {
		t.Errorf("Sessions = %d, want 1 (old session should be excluded)", insights.Sessions)
	}
	if insights.Messages != 1 {
		t.Errorf("Messages = %d, want 1", insights.Messages)
	}
	// PeriodStart should be set (cutoff date).
	if insights.PeriodStart == "" {
		t.Error("PeriodStart empty, should be set for days > 0")
	}
	if insights.PeriodEnd == "" {
		t.Error("PeriodEnd empty")
	}

	// days=0 (all time) should include both.
	allInsights, err := s.ComputeInsights(ctx, 0)
	if err != nil {
		t.Fatalf("ComputeInsights(0): %v", err)
	}
	if allInsights.Sessions != 2 {
		t.Errorf("All-time Sessions = %d, want 2", allInsights.Sessions)
	}
	if allInsights.Messages != 2 {
		t.Errorf("All-time Messages = %d, want 2", allInsights.Messages)
	}
}

func TestComputeInsightsEmpty(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	insights, err := s.ComputeInsights(ctx, 0)
	if err != nil {
		t.Fatalf("ComputeInsights(0) on empty store: %v", err)
	}
	if insights.Sessions != 0 {
		t.Errorf("Sessions = %d, want 0", insights.Sessions)
	}
	if insights.Messages != 0 {
		t.Errorf("Messages = %d, want 0", insights.Messages)
	}
	if insights.AvgSessionMsgs != 0 {
		t.Errorf("AvgSessionMsgs = %f, want 0", insights.AvgSessionMsgs)
	}
	if len(insights.Tools) != 0 {
		t.Errorf("Tools len = %d, want 0", len(insights.Tools))
	}
	// Daily should still have 7 entries (all zero counts).
	for i, d := range insights.Daily {
		if d.Count != 0 {
			t.Errorf("Daily[%d].Count = %d, want 0", i, d.Count)
		}
		if d.Day == "" {
			t.Errorf("Daily[%d].Day empty", i)
		}
	}
}

// TestMigrationTitlesLabeledSessions covers the title_state data fix: a
// pre-title-state database with one labeled and one unlabeled session.
// On migration the labeled row lands at final state 2 (never retitled),
// the unlabeled row at 0. A reopen must not re-run the backfill.
func TestMigrationTitlesLabeledSessions(t *testing.T) {
	dir := t.TempDir()
	dsn := dir + "/old.db"

	pre, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open pre-migration db: %v", err)
	}
	_, err = pre.Exec(`
CREATE TABLE IF NOT EXISTS sessions (
	id         TEXT PRIMARY KEY,
	parent_id  TEXT REFERENCES sessions(id) ON DELETE CASCADE,
	label      TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);`)
	if err != nil {
		t.Fatalf("create pre-migration schema: %v", err)
	}
	for _, row := range [][2]string{{"labeled", "old label"}, {"unlabeled", ""}} {
		if _, err := pre.Exec(`INSERT INTO sessions (id, label, created_at, updated_at) VALUES (?, ?, '2026-01-01', '2026-01-01')`, row[0], row[1]); err != nil {
			t.Fatalf("insert old session %s: %v", row[0], err)
		}
	}
	if err := pre.Close(); err != nil {
		t.Fatalf("close pre-migration db: %v", err)
	}

	s, err := Open(dsn)
	if err != nil {
		t.Fatalf("open post-migration: %v", err)
	}
	ctx := context.Background()
	labeled, err := s.GetSession(ctx, "labeled")
	if err != nil {
		t.Fatalf("get labeled: %v", err)
	}
	if labeled.TitleState != 2 {
		t.Fatalf("labeled title_state = %d, want 2", labeled.TitleState)
	}
	unlabeled, err := s.GetSession(ctx, "unlabeled")
	if err != nil {
		t.Fatalf("get unlabeled: %v", err)
	}
	if unlabeled.TitleState != 0 {
		t.Fatalf("unlabeled title_state = %d, want 0", unlabeled.TitleState)
	}

	// Reopen: the column exists, so the backfill must not fire again -
	// a provisional state would otherwise be promoted to final.
	if err := s.SetSessionTitle(ctx, "unlabeled", "generated", false); err != nil {
		t.Fatalf("provisional title: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	s2, err := Open(dsn)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	again, err := s2.GetSession(ctx, "unlabeled")
	if err != nil {
		t.Fatalf("get after reopen: %v", err)
	}
	if again.Label != "generated" || again.TitleState != 1 {
		t.Fatalf("reopen mutated title state: %+v", again)
	}
}

// TestSetSessionTitleTransitions covers the title_state state machine
// end to end: provisional only from 0, final from 0 or 1, and every
// write blocked once the session reached 2.
func TestSetSessionTitleTransitions(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, "s1", "", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Provisional 0 -> 1.
	if err := s.SetSessionTitle(ctx, "s1", "provisional title", false); err != nil {
		t.Fatalf("provisional: %v", err)
	}
	if after, _ := s.GetSession(ctx, "s1"); after.Label != "provisional title" || after.TitleState != 1 {
		t.Fatalf("after provisional: %+v", after)
	}

	// Provisional again at state 1: blocked, label untouched.
	if err := s.SetSessionTitle(ctx, "s1", "second provisional", false); err != nil {
		t.Fatalf("second provisional: %v", err)
	}
	if after, _ := s.GetSession(ctx, "s1"); after.Label != "provisional title" || after.TitleState != 1 {
		t.Fatalf("second provisional overwrote: %+v", after)
	}

	// Final 1 -> 2.
	if err := s.SetSessionTitle(ctx, "s1", "final title", true); err != nil {
		t.Fatalf("final: %v", err)
	}
	if after, _ := s.GetSession(ctx, "s1"); after.Label != "final title" || after.TitleState != 2 {
		t.Fatalf("after final: %+v", after)
	}

	// Everything is blocked at 2.
	for _, tc := range []struct {
		name  string
		final bool
	}{{"provisional at 2", false}, {"final at 2", true}} {
		if err := s.SetSessionTitle(ctx, "s1", "regression", tc.final); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
	}
	if after, _ := s.GetSession(ctx, "s1"); after.Label != "final title" || after.TitleState != 2 {
		t.Fatalf("state-2 write overwrote: %+v", after)
	}

	// Final directly from 0 (provisional never fired, e.g. the session
	// was retired before the request-start call landed).
	if err := s.CreateSession(ctx, "s2", "", ""); err != nil {
		t.Fatalf("create s2: %v", err)
	}
	if err := s.SetSessionTitle(ctx, "s2", "final from zero", true); err != nil {
		t.Fatalf("final from 0: %v", err)
	}
	if after, _ := s.GetSession(ctx, "s2"); after.Label != "final from zero" || after.TitleState != 2 {
		t.Fatalf("after final from 0: %+v", after)
	}
}

// TestSetSessionLastRun covers the last-run stats block: fresh sessions
// carry none (no phantom rates), a written block round-trips through
// Get and List, nil clears it, and the write never touches updated_at
// (telemetry is not activity - the time buckets read it).
func TestSetSessionLastRun(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.CreateSession(ctx, "s1", "", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}
	sess, err := s.GetSession(ctx, "s1")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if sess.LastRun != nil {
		t.Fatalf("fresh session last_run = %+v, want nil", sess.LastRun)
	}
	if sess.LastRun != nil || sess.UpdatedAt != sess.CreatedAt {
		t.Fatalf("unexpected state: %+v", sess)
	}

	stats := agent.LastRunStats{TokensPerSecond: 41, ChunksPerSecond: 17}
	if err := s.SetSessionLastRun(ctx, "s1", &stats); err != nil {
		t.Fatalf("set last run: %v", err)
	}
	sess, err = s.GetSession(ctx, "s1")
	if err != nil {
		t.Fatalf("reget session: %v", err)
	}
	if sess.LastRun == nil || sess.LastRun.TokensPerSecond != 41 || sess.LastRun.ChunksPerSecond != 17 {
		t.Fatalf("last_run round-trip = %+v, want 41/17", sess.LastRun)
	}
	// The write is not activity.
	if sess.UpdatedAt != sess.CreatedAt {
		t.Fatalf("updated_at moved %q -> %q, want untouched", sess.CreatedAt, sess.UpdatedAt)
	}

	list, err := s.ListSessions(ctx)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(list) != 1 || list[0].LastRun == nil {
		t.Fatalf("list last_run missing: %+v", list)
	}

	// nil clears the block - back to no phantom rates.
	if err := s.SetSessionLastRun(ctx, "s1", nil); err != nil {
		t.Fatalf("clear last run: %v", err)
	}
	sess, err = s.GetSession(ctx, "s1")
	if err != nil {
		t.Fatalf("reget after clear: %v", err)
	}
	if sess.LastRun != nil {
		t.Fatalf("cleared last_run = %+v, want nil", sess.LastRun)
	}
}

// TestMigrationAddsLastRunColumn upgrades a database that predates the
// last-run block and verifies old rows come through with none, then let
// a write land normally.
func TestMigrationAddsLastRunColumn(t *testing.T) {
	dir := t.TempDir()
	dsn := dir + "/old.db"
	ctx := context.Background()

	pre, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open pre-migration db: %v", err)
	}
	_, err = pre.Exec(`
CREATE TABLE IF NOT EXISTS sessions (
	id         TEXT PRIMARY KEY,
	label      TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);`)
	if err != nil {
		t.Fatalf("create pre-migration schema: %v", err)
	}
	if _, err := pre.Exec(`INSERT INTO sessions (id, label, created_at, updated_at) VALUES ('old', '', '2026-01-01', '2026-01-01')`); err != nil {
		t.Fatalf("insert old session: %v", err)
	}
	if err := pre.Close(); err != nil {
		t.Fatalf("close pre-migration db: %v", err)
	}

	// Opening through the store migrates in place; old rows have no run.
	s, err := Open(dsn)
	if err != nil {
		t.Fatalf("open post-migration: %v", err)
	}
	defer s.Close()
	old, err := s.GetSession(ctx, "old")
	if err != nil {
		t.Fatalf("get old session: %v", err)
	}
	if old.LastRun != nil {
		t.Fatalf("migrated last_run = %+v, want nil", old.LastRun)
	}
	stats := agent.LastRunStats{TokensPerSecond: 9, ChunksPerSecond: 4}
	if err := s.SetSessionLastRun(ctx, "old", &stats); err != nil {
		t.Fatalf("set after migration: %v", err)
	}
	old, err = s.GetSession(ctx, "old")
	if err != nil {
		t.Fatalf("reget: %v", err)
	}
	if old.LastRun == nil || old.LastRun.TokensPerSecond != 9 {
		t.Fatalf("post-migration write = %+v, want 9 tok/s", old.LastRun)
	}
}
