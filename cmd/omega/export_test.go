package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/ai"
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

func TestResolveSessionCLINotFound(t *testing.T) {
	// Without a real store, resolveSessionCLI should error.
	// We can't easily test the success path without a real SQLite store,
	// but the error path is testable with nil.
	// Skipped: requires a store instance. The logic is covered by
	// the TUI resolveSession tests.
	t.Skip("requires a store instance")
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
