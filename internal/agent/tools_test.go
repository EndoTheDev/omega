package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// requireTool returns the named tool from the built-in registry, failing the
// test if it is missing.
func requireTool(t *testing.T, name string) Tool {
	t.Helper()
	tool, ok := NewRegistry()[name]
	if !ok {
		t.Fatalf("registry missing tool %q", name)
	}
	return tool
}

// inTempDir creates a temp directory, registers its cleanup, and returns its
// absolute path.
func inTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("filepath.Abs: %v", err)
	}
	return abs
}

func TestRegistryContainsAllTools(t *testing.T) {
	registry := NewRegistry()
	for _, name := range []string{"shell", "read_file", "write_file", "edit"} {
		if _, ok := registry[name]; !ok {
			t.Errorf("registry missing tool %q", name)
		}
	}
}

func TestShellRunsCommand(t *testing.T) {
	tool := requireTool(t, "shell")
	out, err := tool.Run(context.Background(), map[string]any{"command": "echo hello"})
	if err != nil {
		t.Fatalf("shell failed: %v", err)
	}
	if !strings.Contains(out, "hello") {
		t.Fatalf("shell output = %q, want it to contain %q", out, "hello")
	}
}

func TestShellRejectsMissingCommand(t *testing.T) {
	tool := requireTool(t, "shell")
	if _, err := tool.Run(context.Background(), map[string]any{}); err == nil {
		t.Fatal("expected error for missing command, got nil")
	}
}

func TestWriteThenReadFile(t *testing.T) {
	dir := inTempDir(t)
	path := filepath.Join(dir, "nested", "file.txt")
	want := "line one\nline two\nline three\n"

	writeTool := requireTool(t, "write_file")
	_, err := writeTool.Run(context.Background(), map[string]any{
		"path":    path,
		"content": want,
	})
	if err != nil {
		t.Fatalf("write_file failed: %v", err)
	}

	readTool := requireTool(t, "read_file")
	out, err := readTool.Run(context.Background(), map[string]any{"path": path})
	if err != nil {
		t.Fatalf("read_file failed: %v", err)
	}
	wantNumbered := "1|line one\n2|line two\n3|line three\n"
	if out != wantNumbered {
		t.Fatalf("read_file = %q, want %q", out, wantNumbered)
	}
}

func TestReadFileOffsetAndLimit(t *testing.T) {
	dir := inTempDir(t)
	path := filepath.Join(dir, "lines.txt")
	content := "a\nb\nc\nd\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	tool := requireTool(t, "read_file")
	out, err := tool.Run(context.Background(), map[string]any{
		"path":   path,
		"offset": float64(2),
		"limit":  float64(2),
	})
	if err != nil {
		t.Fatalf("read_file failed: %v", err)
	}
	want := "2|b\n3|c\n"
	if out != want {
		t.Fatalf("read_file = %q, want %q", out, want)
	}
}

func TestReadFileRejectsInvalidPath(t *testing.T) {
	tool := requireTool(t, "read_file")
	if _, err := tool.Run(context.Background(), map[string]any{"path": ""}); err == nil {
		t.Fatal("expected error for empty path, got nil")
	}
}

func TestWriteFileRejectsMissingContent(t *testing.T) {
	tool := requireTool(t, "write_file")
	if _, err := tool.Run(context.Background(), map[string]any{"path": "x.txt"}); err == nil {
		t.Fatal("expected error for missing content, got nil")
	}
}

func TestEditReplacesUniqueString(t *testing.T) {
	dir := inTempDir(t)
	path := filepath.Join(dir, "file.txt")
	content := "alpha\nbeta\ngamma\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	tool := requireTool(t, "edit")
	out, err := tool.Run(context.Background(), map[string]any{
		"path":       path,
		"old_string": "beta",
		"new_string": "BETA",
	})
	if err != nil {
		t.Fatalf("edit failed: %v", err)
	}
	if !strings.Contains(out, path) {
		t.Fatalf("edit output = %q, want it to contain the path %q", out, path)
	}
	if !strings.Contains(out, "-beta") || !strings.Contains(out, "+BETA") {
		t.Fatalf("edit output = %q, want diff summary with -beta and +BETA", out)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read patched file: %v", err)
	}
	if string(got) != "alpha\nBETA\ngamma\n" {
		t.Fatalf("patched content = %q, want %q", string(got), "alpha\nBETA\ngamma\n")
	}
}

func TestEditRejectsMissingOldString(t *testing.T) {
	dir := inTempDir(t)
	path := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(path, []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	tool := requireTool(t, "edit")
	if _, err := tool.Run(context.Background(), map[string]any{
		"path":       path,
		"old_string": "nope",
		"new_string": "x",
	}); err == nil {
		t.Fatal("expected error for missing old_string, got nil")
	}
}

func TestEditRejectsNonUniqueOldString(t *testing.T) {
	dir := inTempDir(t)
	path := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(path, []byte("dup\ndup\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	tool := requireTool(t, "edit")
	if _, err := tool.Run(context.Background(), map[string]any{
		"path":       path,
		"old_string": "dup",
		"new_string": "x",
	}); err == nil {
		t.Fatal("expected error for non-unique old_string, got nil")
	}
}

func TestEditRejectsMissingArguments(t *testing.T) {
	tool := requireTool(t, "edit")
	if _, err := tool.Run(context.Background(), map[string]any{
		"path": "x.txt",
	}); err == nil {
		t.Fatal("expected error for missing old_string, got nil")
	}
	if _, err := tool.Run(context.Background(), map[string]any{
		"old_string": "a",
		"new_string": "b",
	}); err == nil {
		t.Fatal("expected error for missing path, got nil")
	}
}
