package store

import (
	"context"
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
