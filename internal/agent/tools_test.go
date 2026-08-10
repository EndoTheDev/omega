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
	for _, name := range []string{"shell", "read_file", "write_file", "search_files"} {
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

func TestSearchFilesContent(t *testing.T) {
	dir := inTempDir(t)
	files := map[string]string{
		"a.go":     "package main\nfunc hello() {}\n",
		"b.go":     "package main\nvar x = 1\n",
		"notes.md": "hello world\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write fixture %s: %v", name, err)
		}
	}

	tool := requireTool(t, "search_files")
	out, err := tool.Run(context.Background(), map[string]any{
		"pattern": "hello",
		"path":    dir,
	})
	if err != nil {
		t.Fatalf("search_files failed: %v", err)
	}
	if !strings.Contains(out, "a.go") || !strings.Contains(out, "notes.md") {
		t.Fatalf("content search output = %q, want matches for a.go and notes.md", out)
	}
	if strings.Contains(out, "b.go") {
		t.Fatalf("content search matched b.go, which has no hello: %q", out)
	}
}

func TestSearchFilesNames(t *testing.T) {
	dir := inTempDir(t)
	for _, name := range []string{"one_test.go", "two.txt", "three_test.go"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("write fixture %s: %v", name, err)
		}
	}

	tool := requireTool(t, "search_files")
	out, err := tool.Run(context.Background(), map[string]any{
		"pattern": `_test\.go$`,
		"target":  "files",
		"path":    dir,
	})
	if err != nil {
		t.Fatalf("search_files failed: %v", err)
	}
	if !strings.Contains(out, "one_test.go") || !strings.Contains(out, "three_test.go") {
		t.Fatalf("filename search output = %q, want both _test.go files", out)
	}
	if strings.Contains(out, "two.txt") {
		t.Fatalf("filename search matched two.txt: %q", out)
	}
}

func TestSearchFilesRejectsInvalidTarget(t *testing.T) {
	tool := requireTool(t, "search_files")
	if _, err := tool.Run(context.Background(), map[string]any{
		"pattern": "x",
		"target":  "bogus",
	}); err == nil {
		t.Fatal("expected error for invalid target, got nil")
	}
}
