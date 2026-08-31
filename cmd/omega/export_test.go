package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/ai"
	"github.com/EndoTheDev/omega/extensions/store"
)

func TestExportMessages(t *testing.T) {
	messages := []ai.Message{
		ai.NewUser("hello"),
		ai.NewAssistant("hi there"),
	}
	var buf bytes.Buffer
	if err := exportMessages(messages, &buf); err != nil {
		t.Fatalf("exportMessages: %v", err)
	}
	output := buf.String()
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	if !strings.Contains(lines[0], `"role":"user"`) || !strings.Contains(lines[0], `"content":"hello"`) {
		t.Errorf("line 0 = %q, want user/hello", lines[0])
	}
	if !strings.Contains(lines[1], `"role":"assistant"`) || !strings.Contains(lines[1], `"content":"hi there"`) {
		t.Errorf("line 1 = %q, want assistant/hi there", lines[1])
	}
}

func TestExportMessagesEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := exportMessages(nil, &buf); err != nil {
		t.Fatalf("exportMessages: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("expected empty output, got %q", buf.String())
	}
}

// newTestExportStore creates an in-memory store for export CLI tests.
func newTestExportStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestResolveSessionCLIExactID(t *testing.T) {
	s := newTestExportStore(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, "s1", "", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}
	id, err := resolveSessionCLI(s, "s1")
	if err != nil {
		t.Fatalf("resolveSessionCLI: %v", err)
	}
	if id != "s1" {
		t.Fatalf("id = %q, want s1", id)
	}
}

func TestResolveSessionCLILabelPrefix(t *testing.T) {
	s := newTestExportStore(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, "s1", "", "my-session"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	id, err := resolveSessionCLI(s, "my-sess")
	if err != nil {
		t.Fatalf("resolveSessionCLI: %v", err)
	}
	if id != "s1" {
		t.Fatalf("id = %q, want s1", id)
	}
}

func TestResolveSessionCLILabelCaseInsensitive(t *testing.T) {
	s := newTestExportStore(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, "s1", "", "my-session"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	id, err := resolveSessionCLI(s, "MY-SESS")
	if err != nil {
		t.Fatalf("resolveSessionCLI: %v", err)
	}
	if id != "s1" {
		t.Fatalf("id = %q, want s1", id)
	}
}

func TestResolveSessionCLINotFound(t *testing.T) {
	s := newTestExportStore(t)
	_, err := resolveSessionCLI(s, "nonexistent")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "session not found") {
		t.Fatalf("err = %q, want error containing %q", err.Error(), "session not found")
	}
}

func TestResolveSessionCLIMultipleMatches(t *testing.T) {
	s := newTestExportStore(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, "a", "", "test-one"); err != nil {
		t.Fatalf("create session a: %v", err)
	}
	if err := s.CreateSession(ctx, "b", "", "test-two"); err != nil {
		t.Fatalf("create session b: %v", err)
	}
	_, err := resolveSessionCLI(s, "test")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "multiple sessions match") {
		t.Fatalf("err = %q, want error containing %q", err.Error(), "multiple sessions match")
	}
}

func TestMessageRole(t *testing.T) {
	tests := []struct {
		msg  ai.Message
		want string
	}{
		{ai.NewUser("x"), "user"},
		{ai.NewAssistant("x"), "assistant"},
		{ai.NewSystem("x"), "system"},
		{ai.ToolResult{Content: "x"}, "tool"},
	}
	for _, tt := range tests {
		if got := ai.MessageRole(tt.msg); got != tt.want {
			t.Errorf("ai.MessageRole(%T) = %q, want %q", tt.msg, got, tt.want)
		}
	}
}

// setupExportEnv prepares an OMEGA_HOME temp dir with a config.yaml and a
// store containing a session with two messages. Returns the home dir,
// session ID, and a cleanup function.
func setupExportEnv(t *testing.T) (home, sessionID string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("OMEGA_HOME", home)

	// Write a minimal config.yaml that passes Validate (model_name + port).
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(
		"provider:\n  model_name: test-model\nserver:\n  port: 8099\n",
	), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// resolveHomePaths rewrites the default "omega.db" to home/omega.db.
	s, err := store.Open(filepath.Join(home, "omega.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	sessionID = "test-session"
	if err := s.CreateSession(ctx, sessionID, "", "export-test"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := s.AppendMessage(ctx, sessionID, ai.NewUser("hello world")); err != nil {
		t.Fatalf("append user message: %v", err)
	}
	if err := s.AppendMessage(ctx, sessionID, ai.NewAssistant("hi there")); err != nil {
		t.Fatalf("append assistant message: %v", err)
	}
	return home, sessionID
}

func TestCmdExportToFile(t *testing.T) {
	_, sessionID := setupExportEnv(t)
	outputPath := filepath.Join(t.TempDir(), "out.jsonl")

	if err := cmdExport("", []string{sessionID, outputPath}); err != nil {
		t.Fatalf("cmdExport: %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	var first, second map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("parse line 0: %v", err)
	}
	if err := json.Unmarshal([]byte(lines[1]), &second); err != nil {
		t.Fatalf("parse line 1: %v", err)
	}
	if first["role"] != "user" || first["content"] != "hello world" {
		t.Errorf("line 0 = %v, want role=user content=hello world", first)
	}
	if second["role"] != "assistant" || second["content"] != "hi there" {
		t.Errorf("line 1 = %v, want role=assistant content=hi there", second)
	}
}

func TestCmdExportDefaultOutputPath(t *testing.T) {
	home, sessionID := setupExportEnv(t)

	// With only one arg, output defaults to "<sessionID>.jsonl" in CWD.
	// Save and restore cwd so the test doesn't leak working-directory changes.
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	if err := os.Chdir(home); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	if err := cmdExport("", []string{sessionID}); err != nil {
		t.Fatalf("cmdExport: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(home, sessionID+".jsonl"))
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !strings.Contains(string(data), "hello world") {
		t.Errorf("output missing expected content, got %q", string(data))
	}
}

func TestCmdExportNoArgs(t *testing.T) {
	t.Setenv("OMEGA_HOME", t.TempDir())
	err := cmdExport("", nil)
	if err == nil {
		t.Fatal("expected error for no args, got nil")
	}
	if !strings.Contains(err.Error(), "usage") {
		t.Fatalf("err = %q, want error containing %q", err.Error(), "usage")
	}
}

func TestCmdExportSessionNotFound(t *testing.T) {
	setupExportEnv(t)
	outputPath := filepath.Join(t.TempDir(), "out.jsonl")
	err := cmdExport("", []string{"does-not-exist", outputPath})
	if err == nil {
		t.Fatal("expected error for nonexistent session, got nil")
	}
	if !strings.Contains(err.Error(), "session not found") {
		t.Fatalf("err = %q, want error containing %q", err.Error(), "session not found")
	}
}
