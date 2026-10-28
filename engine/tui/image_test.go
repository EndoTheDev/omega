package tui

import (
	"strings"
	"testing"
)

// The @-mention expansion tests moved with the seam: core/atmention_test.go
// owns them now (card t_07aed3f9 - one expansion law, two callers). What
// stays here is the TUI-owned highlight machinery this file also covered.

func TestHighlightCodeGo(t *testing.T) {
	code := "func main() {\n    fmt.Println(\"hello\")\n}"
	result := highlightCode(code, "go", "dark")
	if result == code {
		t.Fatal("expected highlighted output to differ from input")
	}
	// Should contain ANSI escape codes.
	if !strings.Contains(result, "\x1b[") {
		t.Fatal("expected ANSI escape codes in highlighted output")
	}
}

func TestHighlightCodeEmptyLang(t *testing.T) {
	code := "plain text content"
	result := highlightCode(code, "", "dark")
	if result != code {
		t.Fatalf("expected unchanged output for empty lang, got %q", result)
	}
}

func TestHighlightCodeInvalidLang(t *testing.T) {
	code := "some text"
	result := highlightCode(code, "nonexistent-lang-xyz", "dark")
	// Should fall back to plain text.
	if result == "" {
		t.Fatal("expected non-empty fallback")
	}
}

func TestLangForTool(t *testing.T) {
	tests := []struct {
		toolName string
		args     map[string]any
		want     string
	}{
		{"files.read", map[string]any{"path": "main.go"}, "go"},
		{"files.read", map[string]any{"path": "app.py"}, "python"},
		{"files.read", map[string]any{"path": "index.ts"}, "typescript"},
		{"files.read", map[string]any{"path": "main.rs"}, "rust"},
		{"files.read", map[string]any{"path": "Dockerfile"}, "dockerfile"},
		{"files.read", map[string]any{"path": "Makefile"}, "makefile"},
		{"files.read", map[string]any{"path": "notes.txt"}, ""},
		{"files.read", map[string]any{"path": "noext"}, ""},
		{"shell.run", map[string]any{"command": "ls -la"}, "bash"},
		{"files.write", map[string]any{"path": "x.go", "content": "..."}, ""},
		{"unknown", nil, ""},
	}
	for _, tt := range tests {
		got := langForTool(tt.toolName, tt.args)
		if got != tt.want {
			t.Errorf("langForTool(%q, %v) = %q, want %q", tt.toolName, tt.args, got, tt.want)
		}
	}
}