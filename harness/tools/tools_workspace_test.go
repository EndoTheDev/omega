package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/EndoTheDev/omega/core"
)

// TestWorkspaceToolsRoot pins the session-workspace rooting (card
// t_9de5b009): a run scoped to a folder resolves RELATIVE paths against
// it (files and shell alike), absolute paths pass through, and runs
// without a workspace keep today's behavior exactly.
func TestWorkspaceToolsRoot(t *testing.T) {
	ws := t.TempDir()
	ctx := core.WithRunWorkspace(context.Background(), ws, "")
	plain := context.Background()
	p := &ToolProvider{}

	// Write rooted: a relative path lands inside the workspace.
	if _, err := p.RunWriteFile(ctx, map[string]any{"path": "nested/out.txt", "content": "rooted"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(ws, "nested/out.txt"))
	if err != nil || string(b) != "rooted" {
		t.Fatalf("workspace write missing: %v (got %q)", err, string(b))
	}
	if _, err := os.Stat("nested/out.txt"); err == nil {
		t.Fatalf("write leaked outside the workspace (process cwd)")
	}

	// Read rooted: the relative path reads from the workspace.
	out, err := p.RunReadFile(ctx, map[string]any{"path": "nested/out.txt"})
	if err != nil || !strings.Contains(out, "rooted") {
		t.Fatalf("read: %v (got %q)", err, out)
	}

	// Edit rooted.
	if _, err := p.RunEdit(ctx, map[string]any{"path": "nested/out.txt", "old_string": "rooted", "new_string": "edited"}); err != nil {
		t.Fatalf("edit: %v", err)
	}
	b, _ = os.ReadFile(filepath.Join(ws, "nested/out.txt"))
	if string(b) != "edited" {
		t.Fatalf("edit: got %q, want \"edited\"", string(b))
	}

	// Atomic batch rooted: both ops of operations[].path resolve in
	// the workspace.
	if _, err := p.RunFiles(ctx, map[string]any{"operations": []map[string]any{
		{"action": "write", "path": "a.txt", "content": "A"},
		{"action": "write", "path": "b.txt", "content": "B"},
	}}); err != nil {
		t.Fatalf("batch: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ws, "a.txt")); err != nil {
		t.Fatalf("batch a.txt not in workspace: %v", err)
	}

	// Shell rooted: the command's working directory IS the workspace
	// (pwd output contains it; the output may carry truncation notes).
	out, err = p.RunShell(ctx, map[string]any{"command": "pwd"})
	if err != nil || !strings.Contains(out, ws) {
		t.Fatalf("shell pwd: %v (got %q, want it to contain %q)", err, out, ws)
	}

	// Absolute paths pass through untouched, workspace or not.
	abs := filepath.Join(t.TempDir(), "abs.txt")
	if _, err := p.RunWriteFile(ctx, map[string]any{"path": abs, "content": "abs"}); err != nil {
		t.Fatalf("abs write: %v", err)
	}
	if b, err := os.ReadFile(abs); err != nil || !utf8.ValidString(string(b)) {
		t.Fatalf("abs path ignored the workspace: %v", err)
	}

	// No workspace: relative paths behave today - they resolve against
	// the process cwd, where the path simply does not exist.
	out, err = p.RunReadFile(plain, map[string]any{"path": "definitely/missing/nowhere.txt"})
	if err == nil || !strings.Contains(err.Error(), "no such file") {
		t.Fatalf("plain read: %v (got %q)", err, out)
	}
	if _, statErr := os.Stat(filepath.Clean(ws + "/definitely")); !os.IsNotExist(statErr) {
		t.Fatalf("plain run touched the workspace dir: %v", statErr)
	}
}
