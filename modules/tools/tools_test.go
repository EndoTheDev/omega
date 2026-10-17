package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
)

// self-check: compile-time interface satisfaction, plugin contract,
// and basic tool behavior (write → read → edit → read).
func TestToolProviderImplementsAgentToolProvider(t *testing.T) {
	var _ agent.ToolProvider = (*ToolProvider)(nil)
	var _ agent.Plugin = (*Plugin)(nil)
}

func TestPluginContract(t *testing.T) {
	p := NewPlugin()
	if p.Name() != "tools" {
		t.Fatalf("Name = %q, want %q", p.Name(), "tools")
	}
	if len(p.Requires()) != 0 {
		t.Fatalf("Requires = %v, want empty", p.Requires())
	}
	provides := p.Provides()
	if len(provides) != 1 || provides[0] != "tools" {
		t.Fatalf("Provides = %v, want [tools]", provides)
	}
}

func TestMountAppendsToolProvider(t *testing.T) {
	p := NewPlugin()
	ctx := &agent.Context{}
	if err := p.Mount(ctx); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	if len(ctx.ToolProviders) != 1 {
		t.Fatalf("ToolProviders len = %d, want 1", len(ctx.ToolProviders))
	}
	tools := ctx.ToolProviders[0].Tools()
	for _, name := range []string{"shell.run", "files.read", "files.write", "files.edit"} {
		if _, ok := tools[name]; !ok {
			t.Errorf("missing tool %q", name)
		}
	}
}

func TestWriteReadEditCycle(t *testing.T) {
	tp := &ToolProvider{}
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")

	// write
	out, err := tp.RunWriteFile(ctx, map[string]any{
		"path":    path,
		"content": "hello\nworld\n",
	})
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if !strings.Contains(out, "wrote") {
		t.Fatalf("write output = %q", out)
	}

	// read
	out, err = tp.RunReadFile(ctx, map[string]any{"path": path})
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(out, "1|hello") || !strings.Contains(out, "2|world") {
		t.Fatalf("read output = %q", out)
	}

	// edit
	out, err = tp.RunEdit(ctx, map[string]any{
		"path":       path,
		"old_string": "hello",
		"new_string": "hey",
	})
	if err != nil {
		t.Fatalf("Edit: %v", err)
	}
	if !strings.Contains(out, "patched") {
		t.Fatalf("edit output = %q", out)
	}

	// verify edit
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("final read: %v", err)
	}
	if !strings.Contains(string(data), "hey") {
		t.Fatalf("file content after edit = %q", string(data))
	}
}

func TestEditNonUniqueOldString(t *testing.T) {
	tp := &ToolProvider{}
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "dup.txt")

	_, err := tp.RunWriteFile(ctx, map[string]any{
		"path":    path,
		"content": "aaa\naaa\n",
	})
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err = tp.RunEdit(ctx, map[string]any{
		"path":       path,
		"old_string": "aaa",
		"new_string": "bbb",
	})
	if err == nil {
		t.Fatal("expected error for non-unique old_string, got nil")
	}
}

func TestShellRun(t *testing.T) {
	tp := &ToolProvider{}
	ctx := context.Background()

	// Use a command that works on both Windows and Unix via the shell.
	out, err := tp.RunShell(ctx, map[string]any{"command": "echo hello"})
	if err != nil {
		t.Fatalf("RunShell: %v", err)
	}
	if !strings.Contains(strings.TrimSpace(out), "hello") {
		t.Fatalf("shell output = %q", out)
	}
}

func TestShellEmptyCommand(t *testing.T) {
	tp := &ToolProvider{}
	ctx := context.Background()

	_, err := tp.RunShell(ctx, map[string]any{"command": "   "})
	if err == nil {
		t.Fatal("expected error for empty command, got nil")
	}
}

// writeFileLines is a small helper that creates a file at path with the
// given newline-separated lines (a trailing newline is added).
func writeFileLines(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write helper: %v", err)
	}
}

func TestRunReadFileOffset(t *testing.T) {
	tp := &ToolProvider{}
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "lines.txt")
	writeFileLines(t, path, "one\ntwo\nthree\nfour\nfive\n")

	out, err := tp.RunReadFile(ctx, map[string]any{
		"path":   path,
		"offset": float64(3),
	})
	if err != nil {
		t.Fatalf("RunReadFile offset: %v", err)
	}
	if !strings.Contains(out, "3|three") || !strings.Contains(out, "4|four") || !strings.Contains(out, "5|five") {
		t.Fatalf("expected lines 3-5, got %q", out)
	}
	if strings.Contains(out, "1|one") || strings.Contains(out, "2|two") {
		t.Fatalf("offset should skip first 2 lines, got %q", out)
	}
}

func TestRunReadFileLimit(t *testing.T) {
	tp := &ToolProvider{}
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "lines.txt")
	writeFileLines(t, path, "one\ntwo\nthree\nfour\nfive\n")

	out, err := tp.RunReadFile(ctx, map[string]any{
		"path":  path,
		"limit": float64(2),
	})
	if err != nil {
		t.Fatalf("RunReadFile limit: %v", err)
	}
	if !strings.Contains(out, "1|one") || !strings.Contains(out, "2|two") {
		t.Fatalf("expected first 2 lines, got %q", out)
	}
	if strings.Contains(out, "3|three") || strings.Contains(out, "4|four") || strings.Contains(out, "5|five") {
		t.Fatalf("limit should stop after 2 lines, got %q", out)
	}
}

func TestRunReadFileOffsetAndLimit(t *testing.T) {
	tp := &ToolProvider{}
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "lines.txt")
	writeFileLines(t, path, "one\ntwo\nthree\nfour\nfive\n")

	out, err := tp.RunReadFile(ctx, map[string]any{
		"path":   path,
		"offset": float64(2),
		"limit":  float64(2),
	})
	if err != nil {
		t.Fatalf("RunReadFile offset+limit: %v", err)
	}
	if !strings.Contains(out, "2|two") || !strings.Contains(out, "3|three") {
		t.Fatalf("expected lines 2-3, got %q", out)
	}
	if strings.Contains(out, "1|one") || strings.Contains(out, "4|four") || strings.Contains(out, "5|five") {
		t.Fatalf("offset+limit should only return lines 2-3, got %q", out)
	}
}

func TestRunReadFileEmptyPath(t *testing.T) {
	tp := &ToolProvider{}
	ctx := context.Background()

	_, err := tp.RunReadFile(ctx, map[string]any{"path": ""})
	if err == nil {
		t.Fatal("expected error for empty path, got nil")
	}
}

func TestRunReadFileMissingFile(t *testing.T) {
	tp := &ToolProvider{}
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "does-not-exist.txt")

	_, err := tp.RunReadFile(ctx, map[string]any{"path": path})
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestRunReadFileInvalidOffsetType(t *testing.T) {
	tp := &ToolProvider{}
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "lines.txt")
	writeFileLines(t, path, "one\ntwo\n")

	_, err := tp.RunReadFile(ctx, map[string]any{
		"path":   path,
		"offset": "3", // string instead of float64
	})
	if err == nil {
		t.Fatal("expected error for invalid offset type, got nil")
	}
}

func TestRunReadFileInvalidLimitType(t *testing.T) {
	tp := &ToolProvider{}
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "lines.txt")
	writeFileLines(t, path, "one\ntwo\n")

	_, err := tp.RunReadFile(ctx, map[string]any{
		"path":  path,
		"limit": "2", // string instead of float64
	})
	if err == nil {
		t.Fatal("expected error for invalid limit type, got nil")
	}
}

func TestRunReadFileOffsetBelowOne(t *testing.T) {
	tp := &ToolProvider{}
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "lines.txt")
	writeFileLines(t, path, "one\ntwo\n")

	_, err := tp.RunReadFile(ctx, map[string]any{
		"path":   path,
		"offset": float64(0),
	})
	if err == nil {
		t.Fatal("expected error for offset < 1, got nil")
	}
}

func TestRunReadFileLimitBelowZero(t *testing.T) {
	tp := &ToolProvider{}
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "lines.txt")
	writeFileLines(t, path, "one\ntwo\n")

	_, err := tp.RunReadFile(ctx, map[string]any{
		"path":  path,
		"limit": float64(-1),
	})
	if err == nil {
		t.Fatal("expected error for limit < 0, got nil")
	}
}

func TestRunReadFileMissingPathArg(t *testing.T) {
	tp := &ToolProvider{}
	ctx := context.Background()

	_, err := tp.RunReadFile(ctx, map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing path argument, got nil")
	}
}

func TestRunReadFileNonStringPath(t *testing.T) {
	tp := &ToolProvider{}
	ctx := context.Background()

	_, err := tp.RunReadFile(ctx, map[string]any{"path": float64(123)})
	if err == nil {
		t.Fatal("expected error for non-string path, got nil")
	}
}