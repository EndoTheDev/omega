package gateway

import (
	"context"
	"errors"
	"testing"

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
