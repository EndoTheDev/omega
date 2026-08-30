package store

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/ai"
)

// ---------------------------------------------------------------------------
// HandleSessionsCommand
// ---------------------------------------------------------------------------

func TestHandleSessionsCommandNilStore(t *testing.T) {
	res, err := HandleSessionsCommand(context.Background(), nil, "")
	if err == nil {
		t.Fatal("expected error for nil store, got nil")
	}
	if !strings.Contains(err.Error(), "no store available") {
		t.Fatalf("err = %q, want 'no store available'", err.Error())
	}
	if res.Text != "" {
		t.Fatalf("Text = %q, want empty on error", res.Text)
	}
}

func TestHandleSessionsCommandEmpty(t *testing.T) {
	s := newTestStore(t)
	res, err := HandleSessionsCommand(context.Background(), s, "")
	if err != nil {
		t.Fatalf("HandleSessionsCommand empty: %v", err)
	}
	if res.Text != "[no sessions yet]" {
		t.Fatalf("Text = %q, want '[no sessions yet]'", res.Text)
	}
}

func TestHandleSessionsCommandPopulated(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.CreateSession(ctx, "s1", "", "alpha"); err != nil {
		t.Fatalf("create s1: %v", err)
	}
	if err := s.CreateSession(ctx, "s2", "", ""); err != nil {
		t.Fatalf("create s2: %v", err)
	}
	// Add messages so counts are non-zero.
	for _, m := range []ai.Message{
		ai.NewUser("hello one"),
		ai.NewAssistant("hi one"),
		ai.NewUser("hello two"),
	} {
		if err := s.AppendMessage(ctx, "s1", m); err != nil {
			t.Fatalf("append s1: %v", err)
		}
	}
	if err := s.AppendMessage(ctx, "s2", ai.NewUser("solo")); err != nil {
		t.Fatalf("append s2: %v", err)
	}

	res, err := HandleSessionsCommand(ctx, s, "")
	if err != nil {
		t.Fatalf("HandleSessionsCommand populated: %v", err)
	}
	if res.Text == "" {
		t.Fatal("Text empty, want table output")
	}
	// Table header present.
	if !strings.Contains(res.Text, "NAME") || !strings.Contains(res.Text, "MSGS") || !strings.Contains(res.Text, "SESSION ID") {
		t.Fatalf("Text missing table header:\n%s", res.Text)
	}
	// Label appears for s1.
	if !strings.Contains(res.Text, "alpha") {
		t.Fatalf("Text missing label 'alpha':\n%s", res.Text)
	}
	// Message count 3 appears for s1.
	if !strings.Contains(res.Text, "3") {
		t.Fatalf("Text missing message count '3':\n%s", res.Text)
	}
	// Session IDs present.
	if !strings.Contains(res.Text, "s1") || !strings.Contains(res.Text, "s2") {
		t.Fatalf("Text missing session ids:\n%s", res.Text)
	}
	// Line-number column header '#'.
	if !strings.Contains(res.Text, "#") {
		t.Fatalf("Text missing '#' column:\n%s", res.Text)
	}
}

// ---------------------------------------------------------------------------
// HandleTreeCommand
// ---------------------------------------------------------------------------

func TestHandleTreeCommandNilStore(t *testing.T) {
	res, err := HandleTreeCommand(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil store, got nil")
	}
	if !strings.Contains(err.Error(), "no store available") {
		t.Fatalf("err = %q, want 'no store available'", err.Error())
	}
	if res.Text != "" {
		t.Fatalf("Text = %q, want empty on error", res.Text)
	}
}

func TestHandleTreeCommandEmpty(t *testing.T) {
	s := newTestStore(t)
	res, err := HandleTreeCommand(context.Background(), s)
	if err != nil {
		t.Fatalf("HandleTreeCommand empty: %v", err)
	}
	if res.Text != "[no sessions yet]" {
		t.Fatalf("Text = %q, want '[no sessions yet]'", res.Text)
	}
}

func TestHandleTreeCommandWithParentChild(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// root -> child -> grandchild, plus a second root with a label.
	if err := s.CreateSession(ctx, "root", "", "rootlabel"); err != nil {
		t.Fatalf("create root: %v", err)
	}
	if err := s.CreateSession(ctx, "child", "root", ""); err != nil {
		t.Fatalf("create child: %v", err)
	}
	if err := s.CreateSession(ctx, "grand", "child", ""); err != nil {
		t.Fatalf("create grand: %v", err)
	}
	if err := s.CreateSession(ctx, "other", "", "otherlabel"); err != nil {
		t.Fatalf("create other: %v", err)
	}
	// Add a message to root so count > 0.
	if err := s.AppendMessage(ctx, "root", ai.NewUser("root msg")); err != nil {
		t.Fatalf("append root: %v", err)
	}

	res, err := HandleTreeCommand(ctx, s)
	if err != nil {
		t.Fatalf("HandleTreeCommand: %v", err)
	}
	if res.Text == "" {
		t.Fatal("Text empty, want tree output")
	}
	// Header present.
	if !strings.Contains(res.Text, "NAME") || !strings.Contains(res.Text, "MSGS") {
		t.Fatalf("Text missing tree header:\n%s", res.Text)
	}
	// Labels present.
	if !strings.Contains(res.Text, "rootlabel") {
		t.Fatalf("Text missing 'rootlabel':\n%s", res.Text)
	}
	if !strings.Contains(res.Text, "otherlabel") {
		t.Fatalf("Text missing 'otherlabel':\n%s", res.Text)
	}
	// Tree glyphs present (child rows use ├─ or └─).
	if !strings.Contains(res.Text, "└─") && !strings.Contains(res.Text, "├─") {
		t.Fatalf("Text missing tree glyphs:\n%s", res.Text)
	}
	// All session ids appear.
	if !strings.Contains(res.Text, "root") || !strings.Contains(res.Text, "child") || !strings.Contains(res.Text, "grand") {
		t.Fatalf("Text missing session ids:\n%s", res.Text)
	}
}

// ---------------------------------------------------------------------------
// HandleSearchCommand
// ---------------------------------------------------------------------------

func TestHandleSearchCommandNilStore(t *testing.T) {
	res, err := HandleSearchCommand(context.Background(), nil, "query")
	if err == nil {
		t.Fatal("expected error for nil store, got nil")
	}
	if !strings.Contains(err.Error(), "no session store available") {
		t.Fatalf("err = %q, want 'no session store available'", err.Error())
	}
	if res.Text != "" {
		t.Fatalf("Text = %q, want empty on error", res.Text)
	}
}

func TestHandleSearchCommandEmptyQuery(t *testing.T) {
	s := newTestStore(t)
	_, err := HandleSearchCommand(context.Background(), s, "   ")
	if err == nil {
		t.Fatal("expected error for empty query, got nil")
	}
	if !strings.Contains(err.Error(), "usage: /search") {
		t.Fatalf("err = %q, want usage error", err.Error())
	}
}

func TestHandleSearchCommandNoResults(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, "s1", "", ""); err != nil {
		t.Fatalf("create s1: %v", err)
	}
	if err := s.AppendMessage(ctx, "s1", ai.NewUser("hello world")); err != nil {
		t.Fatalf("append: %v", err)
	}
	res, err := HandleSearchCommand(ctx, s, "nonexistentterm12345")
	if err != nil {
		t.Fatalf("HandleSearchCommand no results: %v", err)
	}
	if res.Text != "[no results]" {
		t.Fatalf("Text = %q, want '[no results]'", res.Text)
	}
}

func TestHandleSearchCommandMatchingWithLabel(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, "s1", "", "mylabel"); err != nil {
		t.Fatalf("create s1: %v", err)
	}
	if err := s.AppendMessage(ctx, "s1", ai.NewUser("the quick brown fox")); err != nil {
		t.Fatalf("append: %v", err)
	}
	res, err := HandleSearchCommand(ctx, s, "fox")
	if err != nil {
		t.Fatalf("HandleSearchCommand matching: %v", err)
	}
	if res.Text == "" {
		t.Fatal("Text empty, want search results")
	}
	// Label should appear (preferred over session id when label set).
	if !strings.Contains(res.Text, "mylabel") {
		t.Fatalf("Text missing label 'mylabel':\n%s", res.Text)
	}
	// Snippet content present.
	if !strings.Contains(res.Text, "fox") {
		t.Fatalf("Text missing snippet 'fox':\n%s", res.Text)
	}
}

// ---------------------------------------------------------------------------
// HandleInsightsCommand
// ---------------------------------------------------------------------------

func TestHandleInsightsCommandNilStore(t *testing.T) {
	res, err := HandleInsightsCommand(context.Background(), nil, "")
	if err == nil {
		t.Fatal("expected error for nil store, got nil")
	}
	if !strings.Contains(err.Error(), "no session store available") {
		t.Fatalf("err = %q, want 'no session store available'", err.Error())
	}
	if res.Text != "" {
		t.Fatalf("Text = %q, want empty on error", res.Text)
	}
}

func TestHandleInsightsCommandEmptyStore(t *testing.T) {
	s := newTestStore(t)
	res, err := HandleInsightsCommand(context.Background(), s, "")
	if err != nil {
		t.Fatalf("HandleInsightsCommand empty: %v", err)
	}
	// Default days=30; empty store → "[no sessions in the last 30 days]".
	if !strings.Contains(res.Text, "[no sessions in the last 30 days]") {
		t.Fatalf("Text = %q, want '[no sessions in the last 30 days]'", res.Text)
	}
}

func TestHandleInsightsCommandPopulated(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.CreateSession(ctx, "s1", "", "first"); err != nil {
		t.Fatalf("create s1: %v", err)
	}
	if err := s.AppendMessage(ctx, "s1", ai.NewUser("hello world one")); err != nil {
		t.Fatalf("append s1 u1: %v", err)
	}
	if err := s.AppendMessage(ctx, "s1", ai.NewAssistant("response one")); err != nil {
		t.Fatalf("append s1 a1: %v", err)
	}

	if err := s.CreateSession(ctx, "s2", "", "second"); err != nil {
		t.Fatalf("create s2: %v", err)
	}
	if err := s.AppendMessage(ctx, "s2", ai.NewUser("hello world two")); err != nil {
		t.Fatalf("append s2 u1: %v", err)
	}

	// Use a large days window to ensure sessions are included.
	res, err := HandleInsightsCommand(ctx, s, "365")
	if err != nil {
		t.Fatalf("HandleInsightsCommand populated: %v", err)
	}
	if res.Text == "" {
		t.Fatal("Text empty, want insights output")
	}
	// Should NOT be the empty-store message.
	if strings.Contains(res.Text, "[no sessions in the last") {
		t.Fatalf("Text says no sessions, but store is populated:\n%s", res.Text)
	}
	// Insights report header present.
	if !strings.Contains(res.Text, "omega insights") {
		t.Fatalf("Text missing 'omega insights' header:\n%s", res.Text)
	}
	// Overview section with session count.
	if !strings.Contains(res.Text, "Sessions:") {
		t.Fatalf("Text missing 'Sessions:' line:\n%s", res.Text)
	}
	// Activity by Day present.
	if !strings.Contains(res.Text, "Activity by Day") {
		t.Fatalf("Text missing 'Activity by Day':\n%s", res.Text)
	}
}

// ---------------------------------------------------------------------------
// HandleResumeCommand
// ---------------------------------------------------------------------------

func TestHandleResumeCommandNilStore(t *testing.T) {
	res, err := HandleResumeCommand(context.Background(), nil, "1")
	if err == nil {
		t.Fatal("expected error for nil store, got nil")
	}
	if !strings.Contains(err.Error(), "no store available") {
		t.Fatalf("err = %q, want 'no store available'", err.Error())
	}
	if res.Text != "" {
		t.Fatalf("Text = %q, want empty on error", res.Text)
	}
}

func TestHandleResumeCommandEmptyArg(t *testing.T) {
	s := newTestStore(t)
	_, err := HandleResumeCommand(context.Background(), s, "   ")
	if err == nil {
		t.Fatal("expected error for empty arg, got nil")
	}
	if !strings.Contains(err.Error(), "usage: /resume") {
		t.Fatalf("err = %q, want usage error", err.Error())
	}
}

func TestHandleResumeCommandNumberResolution(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, "s1", "", ""); err != nil {
		t.Fatalf("create s1: %v", err)
	}
	if err := s.CreateSession(ctx, "s2", "", ""); err != nil {
		t.Fatalf("create s2: %v", err)
	}

	// "#1" resolves to first session.
	res, err := HandleResumeCommand(ctx, s, "#1")
	if err != nil {
		t.Fatalf("HandleResumeCommand #1: %v", err)
	}
	if len(res.Actions) != 1 || res.Actions[0].Type != "load_session" {
		t.Fatalf("actions = %+v, want one load_session", res.Actions)
	}
	if res.Actions[0].Value != "s1" {
		t.Fatalf("#1 resolved to %q, want s1", res.Actions[0].Value)
	}

	// "2" (no #) resolves to second session.
	res, err = HandleResumeCommand(ctx, s, "2")
	if err != nil {
		t.Fatalf("HandleResumeCommand 2: %v", err)
	}
	if res.Actions[0].Value != "s2" {
		t.Fatalf("2 resolved to %q, want s2", res.Actions[0].Value)
	}
}

func TestHandleResumeCommandExactID(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, "abc123", "", ""); err != nil {
		t.Fatalf("create: %v", err)
	}
	res, err := HandleResumeCommand(ctx, s, "abc123")
	if err != nil {
		t.Fatalf("HandleResumeCommand exact id: %v", err)
	}
	if res.Actions[0].Type != "load_session" || res.Actions[0].Value != "abc123" {
		t.Fatalf("actions = %+v, want load_session abc123", res.Actions)
	}
}

func TestHandleResumeCommandLabelPrefix(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, "s1", "", "my-cool-session"); err != nil {
		t.Fatalf("create: %v", err)
	}
	// Case-insensitive label prefix match.
	res, err := HandleResumeCommand(ctx, s, "MY-COOL")
	if err != nil {
		t.Fatalf("HandleResumeCommand label prefix: %v", err)
	}
	if res.Actions[0].Value != "s1" {
		t.Fatalf("label prefix resolved to %q, want s1", res.Actions[0].Value)
	}
}

func TestHandleResumeCommandNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, "s1", "", ""); err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err := HandleResumeCommand(ctx, s, "nonexistent")
	if err == nil {
		t.Fatal("expected error for not found, got nil")
	}
	if !strings.Contains(err.Error(), "session not found") {
		t.Fatalf("err = %q, want 'session not found'", err.Error())
	}
}

// ---------------------------------------------------------------------------
// HandleBranchCommand
// ---------------------------------------------------------------------------

func TestHandleBranchCommandNilStore(t *testing.T) {
	res, err := HandleBranchCommand(context.Background(), nil, "parent", "")
	if err == nil {
		t.Fatal("expected error for nil store, got nil")
	}
	if !strings.Contains(err.Error(), "no store available") {
		t.Fatalf("err = %q, want 'no store available'", err.Error())
	}
	if res.Text != "" {
		t.Fatalf("Text = %q, want empty on error", res.Text)
	}
}

func TestHandleBranchCommandEmptyParent(t *testing.T) {
	s := newTestStore(t)
	_, err := HandleBranchCommand(context.Background(), s, "", "")
	if err == nil {
		t.Fatal("expected error for empty parentID, got nil")
	}
	if !strings.Contains(err.Error(), "no current session to branch") {
		t.Fatalf("err = %q, want 'no current session to branch'", err.Error())
	}
}

func TestHandleBranchCommandCreatesChild(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, "root", "", ""); err != nil {
		t.Fatalf("create root: %v", err)
	}
	res, err := HandleBranchCommand(ctx, s, "root", "")
	if err != nil {
		t.Fatalf("HandleBranchCommand: %v", err)
	}
	if len(res.Actions) != 1 || res.Actions[0].Type != "branch_session" {
		t.Fatalf("actions = %+v, want one branch_session", res.Actions)
	}
	childID := res.Actions[0].Value
	if childID == "" {
		t.Fatal("branch_session value empty")
	}
	// Child exists and has root as parent.
	child, err := s.GetSession(ctx, childID)
	if err != nil {
		t.Fatalf("get child: %v", err)
	}
	if child.ParentID != "root" {
		t.Fatalf("child.ParentID = %q, want root", child.ParentID)
	}
}

func TestHandleBranchCommandMissingParent(t *testing.T) {
	s := newTestStore(t)
	_, err := HandleBranchCommand(context.Background(), s, "missing-parent", "")
	if err == nil {
		t.Fatal("expected error for missing parent, got nil")
	}
	if !strings.Contains(err.Error(), "branch:") {
		t.Fatalf("err = %q, want 'branch:' error", err.Error())
	}
}

// ---------------------------------------------------------------------------
// HandleLabelCommand
// ---------------------------------------------------------------------------

func TestHandleLabelCommandNilStore(t *testing.T) {
	res, err := HandleLabelCommand(context.Background(), nil, "s1", "label")
	if err == nil {
		t.Fatal("expected error for nil store, got nil")
	}
	if !strings.Contains(err.Error(), "no store available") {
		t.Fatalf("err = %q, want 'no store available'", err.Error())
	}
	if res.Text != "" {
		t.Fatalf("Text = %q, want empty on error", res.Text)
	}
}

func TestHandleLabelCommandEmptySessionID(t *testing.T) {
	s := newTestStore(t)
	_, err := HandleLabelCommand(context.Background(), s, "", "label")
	if err == nil {
		t.Fatal("expected error for empty sessionID, got nil")
	}
	if !strings.Contains(err.Error(), "no current session to label") {
		t.Fatalf("err = %q, want 'no current session to label'", err.Error())
	}
}

func TestHandleLabelCommandSetLabel(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, "s1", "", ""); err != nil {
		t.Fatalf("create: %v", err)
	}
	res, err := HandleLabelCommand(ctx, s, "s1", "my-label")
	if err != nil {
		t.Fatalf("HandleLabelCommand set: %v", err)
	}
	if !strings.Contains(res.Text, "my-label") {
		t.Fatalf("Text = %q, want to contain 'my-label'", res.Text)
	}
	if len(res.Actions) != 1 || res.Actions[0].Type != "set_label" || res.Actions[0].Value != "my-label" {
		t.Fatalf("actions = %+v, want set_label my-label", res.Actions)
	}
	// Verify persisted.
	sess, err := s.GetSession(ctx, "s1")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if sess.Label != "my-label" {
		t.Fatalf("persisted label = %q, want my-label", sess.Label)
	}
}

func TestHandleLabelCommandClearLabel(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, "s1", "", "existing"); err != nil {
		t.Fatalf("create: %v", err)
	}
	res, err := HandleLabelCommand(ctx, s, "s1", "   ")
	if err != nil {
		t.Fatalf("HandleLabelCommand clear: %v", err)
	}
	if res.Text != "[label cleared]" {
		t.Fatalf("Text = %q, want '[label cleared]'", res.Text)
	}
	if len(res.Actions) != 1 || res.Actions[0].Type != "set_label" || res.Actions[0].Value != "" {
		t.Fatalf("actions = %+v, want set_label empty", res.Actions)
	}
	// Verify cleared in store.
	sess, err := s.GetSession(ctx, "s1")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if sess.Label != "" {
		t.Fatalf("label = %q, want empty after clear", sess.Label)
	}
}

// ---------------------------------------------------------------------------
// HandleExportCommand
// ---------------------------------------------------------------------------

func TestHandleExportCommandNilStore(t *testing.T) {
	res, err := HandleExportCommand(context.Background(), nil, "s1", "")
	if err == nil {
		t.Fatal("expected error for nil store, got nil")
	}
	if !strings.Contains(err.Error(), "no active session to export") {
		t.Fatalf("err = %q, want 'no active session to export'", err.Error())
	}
	if res.Text != "" {
		t.Fatalf("Text = %q, want empty on error", res.Text)
	}
}

func TestHandleExportCommandEmptySessionID(t *testing.T) {
	s := newTestStore(t)
	_, err := HandleExportCommand(context.Background(), s, "", "")
	if err == nil {
		t.Fatal("expected error for empty sessionID, got nil")
	}
	if !strings.Contains(err.Error(), "no active session to export") {
		t.Fatalf("err = %q, want 'no active session to export'", err.Error())
	}
}

func TestHandleExportCommandDefaultPath(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, "s1", "", ""); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.AppendMessage(ctx, "s1", ai.NewUser("hello")); err != nil {
		t.Fatalf("append user: %v", err)
	}
	if err := s.AppendMessage(ctx, "s1", ai.NewAssistant("world")); err != nil {
		t.Fatalf("append assistant: %v", err)
	}

	// Use a temp dir as cwd so the default-path file lands there and is cleaned.
	dir := t.TempDir()
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWd) })

	res, err := HandleExportCommand(ctx, s, "s1", "")
	if err != nil {
		t.Fatalf("HandleExportCommand default path: %v", err)
	}
	if !strings.Contains(res.Text, "exported 2 messages") {
		t.Fatalf("Text = %q, want 'exported 2 messages'", res.Text)
	}
	// Default path is sessionID + ".jsonl".
	path := filepath.Join(dir, "s1.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read export file: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 JSONL lines, got %d", len(lines))
	}
	// Validate each line is JSON with role + content.
	var first, second map[string]string
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("unmarshal line 1: %v", err)
	}
	if err := json.Unmarshal([]byte(lines[1]), &second); err != nil {
		t.Fatalf("unmarshal line 2: %v", err)
	}
	if first["role"] != "user" || first["content"] != "hello" {
		t.Fatalf("line 1 = %+v, want role=user content=hello", first)
	}
	if second["role"] != "assistant" || second["content"] != "world" {
		t.Fatalf("line 2 = %+v, want role=assistant content=world", second)
	}
}

func TestHandleExportCommandCustomPath(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, "s1", "", ""); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.AppendMessage(ctx, "s1", ai.NewUser("custom export")); err != nil {
		t.Fatalf("append: %v", err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "custom-export.jsonl")

	res, err := HandleExportCommand(ctx, s, "s1", path)
	if err != nil {
		t.Fatalf("HandleExportCommand custom path: %v", err)
	}
	if !strings.Contains(res.Text, path) {
		t.Fatalf("Text = %q, want to contain path %q", res.Text, path)
	}
	if !strings.Contains(res.Text, "exported 1 messages") {
		t.Fatalf("Text = %q, want 'exported 1 messages'", res.Text)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read custom export: %v", err)
	}
	var entry map[string]string
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &entry); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if entry["role"] != "user" || entry["content"] != "custom export" {
		t.Fatalf("entry = %+v, want role=user content='custom export'", entry)
	}
}

func TestHandleExportCommandEmptySession(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, "s1", "", ""); err != nil {
		t.Fatalf("create: %v", err)
	}
	// No messages appended.
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.jsonl")
	res, err := HandleExportCommand(ctx, s, "s1", path)
	if err != nil {
		t.Fatalf("HandleExportCommand empty session: %v", err)
	}
	if !strings.Contains(res.Text, "exported 0 messages") {
		t.Fatalf("Text = %q, want 'exported 0 messages'", res.Text)
	}
	// File should exist but be empty.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Size() != 0 {
		t.Fatalf("file size = %d, want 0", info.Size())
	}
}

// ---------------------------------------------------------------------------
// Compile-time: *Store satisfies agent.StoreProvider.
// ---------------------------------------------------------------------------

var _ agent.StoreProvider = (*Store)(nil)