package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadProjectContextReadsAGENTS(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# project\nrules"), 0o600); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}
	if got := LoadProjectContext(dir); got != "# project\nrules" {
		t.Fatalf("LoadProjectContext = %q, want AGENTS.md contents", got)
	}
}

func TestLoadProjectContextMissingReturnsEmpty(t *testing.T) {
	if got := LoadProjectContext(t.TempDir()); got != "" {
		t.Fatalf("LoadProjectContext on empty dir = %q, want \"\"", got)
	}
}
