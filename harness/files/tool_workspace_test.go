package files

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/datadir"
)

// TestWorkspaceFilesRoot pins the session-workspace rooting (card
// t_9de5b009) for the file tool (the shell half is pinned in
// harness/terminal): a run scoped to a folder resolves RELATIVE paths
// against it, absolute paths pass through, and runs without a
// workspace keep today's behavior exactly.
func TestWorkspaceFilesRoot(t *testing.T) {
	ws := t.TempDir()
	ctx := core.WithRunWorkspace(context.Background(), ws, "")
	plain := context.Background()
	p := &FileProvider{}

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
	if _, err := p.Tools()["files.batch"].Run(ctx, map[string]any{"operations": []map[string]any{
		{"action": "write", "path": "a.txt", "content": "A"},
		{"action": "write", "path": "b.txt", "content": "B"},
	}}); err != nil {
		t.Fatalf("batch: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ws, "a.txt")); err != nil {
		t.Fatalf("batch a.txt not in workspace: %v", err)
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
	// not-exist via errors.Is, never the posix "no such file" text:
	// windows os errors word it "The system cannot find the path
	// specified." and the words are not a contract, the sentinel is
	if err == nil || !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("plain read: %v (got %q)", err, out)
	}
	if _, statErr := os.Stat(filepath.Clean(ws + "/definitely")); !os.IsNotExist(statErr) {
		t.Fatalf("plain run touched the workspace dir: %v", statErr)
	}
}

// TestUploadsTreeReachable pins the card t_2eb2d274 finding: an
// attachment under <home>/uploads/<session>/ is readable through
// files.read with NO new code - absolute paths pass through the
// workspace rooting untouched, and the only path wall on the tool is
// the instance-db guard, which names the store's own file. The uploads
// tree therefore needs no allow-list, no new tool and no new seam.
// A run rooted at a DIFFERENT workspace (the normal case: the agent
// works in a project folder while uploads live in the home) reaches it
// the same way.
func TestUploadsTreeReachable(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "uploads", "s1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "report.csv")
	if err := os.WriteFile(path, []byte("a,b\n1,2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A realistic guard: the instance db lives beside the uploads dir.
	p := &FileProvider{guard: datadir.NewGuard(filepath.Join(home, "omega.db"))}

	for _, ctx := range []context.Context{
		context.Background(),
		core.WithRunWorkspace(context.Background(), t.TempDir(), ""),
	} {
		out, err := p.RunReadFile(ctx, map[string]any{"path": path})
		if err != nil {
			t.Fatalf("files.read on an upload: %v", err)
		}
		if !strings.Contains(out, "1|a,b") {
			t.Fatalf("files.read returned %q, want the csv with line numbers", out)
		}
	}
}
