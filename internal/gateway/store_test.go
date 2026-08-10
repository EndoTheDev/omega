package gateway

import (
	"context"
	"errors"
	"testing"

	"github.com/EndoTheDev/omega-agent/internal/ai"
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

	if err := s.CreateSession(ctx, "s1"); err != nil {
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
	if err := s.CreateSession(ctx, "s1"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := s.CreateSession(ctx, "s1"); err == nil {
		t.Fatalf("duplicate create should error")
	}
}

func TestListSessions(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for _, id := range []string{"a", "b", "c"} {
		if err := s.CreateSession(ctx, id); err != nil {
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
	if err := s.CreateSession(ctx, "s1"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := s.DeleteSession(ctx, "s1"); err != nil {
		t.Fatalf("delete session: %v", err)
	}
	if _, err := s.GetSession(ctx, "s1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound after delete", err)
	}
}

func TestAppendAndGetMessages(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, "s1"); err != nil {
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
	if err := s1.CreateSession(ctx, "s1"); err != nil {
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
	if err := s.CreateSession(ctx, "s1"); err != nil {
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
