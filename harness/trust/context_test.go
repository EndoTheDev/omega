package trust

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadProjectContextReadsAGENTS(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# project\nrules"), 0o600); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}
	got := LoadProjectContext(dir)
	if !strings.Contains(got, "# project\nrules") {
		t.Fatalf("LoadProjectContext = %q, want it to contain AGENTS.md contents", got)
	}
}

func TestLoadProjectContextMissingReturnsEmpty(t *testing.T) {
	if got := LoadProjectContext(t.TempDir()); got != "" {
		t.Fatalf("LoadProjectContext on empty dir = %q, want \"\"", got)
	}
}

func TestLoadProjectContextAncestorWalk(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "sub")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("root rules"), 0o600); err != nil {
		t.Fatalf("write root AGENTS.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(child, "AGENTS.md"), []byte("child rules"), 0o600); err != nil {
		t.Fatalf("write child AGENTS.md: %v", err)
	}
	got := LoadProjectContext(child)
	rootIdx := strings.Index(got, "root rules")
	childIdx := strings.Index(got, "child rules")
	if rootIdx == -1 || childIdx == -1 {
		t.Fatalf("LoadProjectContext = %q, want both root and child rules", got)
	}
	if rootIdx > childIdx {
		t.Fatalf("root rules should come before child rules, got root at %d, child at %d", rootIdx, childIdx)
	}
}

func TestLoadProjectContextResourceDiagnostics(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(path, []byte("secret"), 0o000); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}
	defer os.Chmod(path, 0o600)
	if _, err := os.ReadFile(path); err == nil {
		t.Skip("current OS/user can read 0o000 files, cannot test resource diagnostics")
	}
	got := LoadProjectContext(dir)
	if !strings.Contains(got, "[warning:") {
		t.Fatalf("LoadProjectContext = %q, want warning about unreadable file", got)
	}
}

func TestProjectRoot(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "sub", "deeper")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("root"), 0o600); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}
	if got := ProjectRoot(child); got != root {
		t.Fatalf("ProjectRoot(%q) = %q, want %q", child, got, root)
	}
	if got := ProjectRoot(root); got != root {
		t.Fatalf("ProjectRoot(%q) = %q, want %q", root, got, root)
	}
}

func TestProjectRootNone(t *testing.T) {
	if got := ProjectRoot(t.TempDir()); got != "" {
		t.Fatalf("ProjectRoot on empty dir = %q, want \"\"", got)
	}
}
