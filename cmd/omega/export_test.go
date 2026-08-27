package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/ai"
	"github.com/EndoTheDev/omega/gateway"
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
func newTestExportStore(t *testing.T) *gateway.Store {
	t.Helper()
	s, err := gateway.Open(":memory:")
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
		if got := messageRole(tt.msg); got != tt.want {
			t.Errorf("messageRole(%T) = %q, want %q", tt.msg, got, tt.want)
		}
	}
}
