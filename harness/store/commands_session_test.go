package store

// The session-scoped commands' two arg shapes. A terminal caller binds no session, so
// the FIRST WORD names the session and the rest is the command's payload - the shape
// every caller of these handlers has always used.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

func sessionCommandsThrough(t *testing.T, ctx context.Context, s *Store, name, args string) (core.CommandResult, error) {
	t.Helper()
	return (&Plugin{}).handleSessionCommand(s, ctx, name, args)
}

func TestSessionCommandsUseTheBoundSession(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, "tab-1", "", ""); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.AppendMessage(ctx, "tab-1", core.NewUser("hello")); err != nil {
		t.Fatalf("append: %v", err)
	}
	bound := core.WithCommandSession(context.Background(), "tab-1")

	res, err := sessionCommandsThrough(t, bound, s, "/label", "from the palette")
	if err != nil {
		t.Fatalf("/label: %v", err)
	}
	if res.Text != "[label: from the palette]" {
		t.Fatalf("/label text = %q, want the label it set", res.Text)
	}
	if len(res.Actions) != 0 {
		t.Fatalf("/label actions = %+v, want none - set_label has no executor", res.Actions)
	}
	got, err := s.GetSession(ctx, "tab-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Label != "from the palette" {
		t.Fatalf("label = %q, want the bound session labelled", got.Label)
	}

	path := filepath.Join(t.TempDir(), "tab.jsonl")
	res, err = sessionCommandsThrough(t, bound, s, "/export", path)
	if err != nil {
		t.Fatalf("/export: %v", err)
	}
	if !strings.Contains(res.Text, "exported 1 messages") {
		t.Fatalf("/export text = %q, want the bound session's message count", res.Text)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("export file: %v", err)
	}

	res, err = sessionCommandsThrough(t, bound, s, "/branch", "")
	if err != nil {
		t.Fatalf("/branch: %v", err)
	}
	if len(res.Actions) != 0 {
		t.Fatalf("/branch actions = %+v, want none - branch_session has no executor", res.Actions)
	}
	child, err := s.GetSession(ctx, childIDFromLine(t, res.Text, "tab-1"))
	if err != nil {
		t.Fatalf("get child: %v", err)
	}
	if child.ParentID != "tab-1" {
		t.Fatalf("child parent = %q, want the bound session", child.ParentID)
	}

	if err := s.CreateSession(ctx, "other", "", ""); err != nil {
		t.Fatalf("create other: %v", err)
	}
	res, err = sessionCommandsThrough(t, bound, s, "/branch", "other")
	if err != nil {
		t.Fatalf("/branch other: %v", err)
	}
	child, err = s.GetSession(ctx, childIDFromLine(t, res.Text, "other"))
	if err != nil {
		t.Fatalf("get child: %v", err)
	}
	if child.ParentID != "other" {
		t.Fatalf("child parent = %q, want the explicitly given session", child.ParentID)
	}
}

func TestSessionCommandsKeepTheTerminalArgShape(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, "cli-1", "", ""); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.AppendMessage(ctx, "cli-1", core.NewUser("hi")); err != nil {
		t.Fatalf("append: %v", err)
	}
	unbound := context.Background()

	// /label <session-id> <text>: unchanged for a caller that binds none
	if _, err := sessionCommandsThrough(t, unbound, s, "/label", "cli-1 my label"); err != nil {
		t.Fatalf("/label legacy: %v", err)
	}
	got, err := s.GetSession(ctx, "cli-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Label != "my label" {
		t.Fatalf("label = %q, want the given session labelled from the first-word shape", got.Label)
	}

	path := filepath.Join(t.TempDir(), "cli.jsonl")
	res, err := sessionCommandsThrough(t, unbound, s, "/export", "cli-1 "+path)
	if err != nil {
		t.Fatalf("/export legacy: %v", err)
	}
	if !strings.Contains(res.Text, "exported 1 messages") {
		t.Fatalf("/export text = %q", res.Text)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read export: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 1 {
		t.Fatalf("export lines = %d, want 1", len(lines))
	}
	var row map[string]string
	if err := json.Unmarshal([]byte(lines[0]), &row); err != nil {
		t.Fatalf("decode export line: %v", err)
	}
	if row["role"] != "user" || row["content"] != "hi" {
		t.Fatalf("export row = %+v, want the {role, content} shape", row)
	}

	_, err = sessionCommandsThrough(t, unbound, s, "/export", "")
	if err == nil || !strings.Contains(err.Error(), "no active session to export") {
		t.Fatalf("/export bare = %v, want the owner's refusal", err)
	}
}

func TestSessionCommandSwitchMissesForeignNamesStructurally(t *testing.T) {
	s := newTestStore(t)
	_, err := sessionCommandsThrough(t, context.Background(), s, "/not-a-store-command", "")
	if err == nil || !errors.Is(err, core.ErrCommandMiss) {
		t.Fatalf("foreign name = %v, want a core.ErrCommandMiss", err)
	}
	if err.Error() != "no handler for /not-a-store-command" {
		t.Fatalf("text = %q, want the historical miss phrase", err)
	}
}