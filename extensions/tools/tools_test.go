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

func TestEditMany(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(a, []byte("foo bar\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("baz qux\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tp := &ToolProvider{}
	ctx := context.Background()
	edits := []any{
		map[string]any{"path": a, "old_string": "foo", "new_string": "FOO"},
		map[string]any{"path": b, "old_string": "qux", "new_string": "QUX"},
	}
	out, err := tp.RunEditMany(ctx, map[string]any{"edits": edits})
	if err != nil {
		t.Fatalf("RunEditMany: %v", err)
	}
	if !strings.Contains(out, "patched "+a) || !strings.Contains(out, "patched "+b) {
		t.Fatalf("output = %q, want both files patched", out)
	}
	ca, _ := os.ReadFile(a)
	cb, _ := os.ReadFile(b)
	if string(ca) != "FOO bar\n" {
		t.Fatalf("a.txt = %q, want 'FOO bar'", string(ca))
	}
	if string(cb) != "baz QUX\n" {
		t.Fatalf("b.txt = %q, want 'baz QUX'", string(cb))
	}
}

func TestEditManyFailsOnMissingFile(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(a, []byte("foo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tp := &ToolProvider{}
	ctx := context.Background()
	edits := []any{
		map[string]any{"path": a, "old_string": "foo", "new_string": "FOO"},
		map[string]any{"path": filepath.Join(dir, "missing.txt"), "old_string": "x", "new_string": "y"},
	}
	_, err := tp.RunEditMany(ctx, map[string]any{"edits": edits})
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}
