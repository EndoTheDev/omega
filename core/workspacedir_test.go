package core

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The workspace-path validator's self-check (the wire guard the session create/PATCH.
// Pinned: refusal shapes (empty, relative, not-a-dir), the accepted happy path, and
// hidden-directory tolerance (a dot directory is a legal workspace root - the browse
// LISTING filters, the validator never does).
func TestResolveWorkspaceDir(t *testing.T) {
	dir := t.TempDir()

	abs, err := ResolveWorkspaceDir(dir)
	if err != nil {
		t.Fatalf("valid dir: %v", err)
	}
	if filepath.Clean(abs) != filepath.Clean(dir) {
		t.Errorf("valid dir = %q, want %q", abs, dir)
	}

	for _, probe := range []struct {
		name, path string
		wantErr    string
	}{
		{"empty", "", "path must not be empty"},
		{"relative", "rel/dir", "workspace must be an absolute path"},
		{"missing", filepath.Join(dir, "definitely-not-here"), ""},
	} {
		if _, err := ResolveWorkspaceDir(probe.path); err == nil {
			t.Errorf("%s: accepted, want refusal", probe.name)
		} else if probe.wantErr != "" && !strings.Contains(err.Error(), probe.wantErr) {
			t.Errorf("%s: err = %q, want it to name %q", probe.name, err, probe.wantErr)
		}
	}

	file := filepath.Join(dir, "f.md")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveWorkspaceDir(file); err == nil {
		t.Errorf("file path accepted, want refusal")
	}

	hidden := filepath.Join(dir, ".local")
	if err := os.Mkdir(hidden, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveWorkspaceDir(hidden); err != nil {
		t.Errorf("hidden root refused: %v", err)
	}

	// Windows: user-supplied volume-less rooted paths stay refused. The volume awareness
	// lives on the browse listing's emitted row paths, never on this validator.
	if runtime.GOOS == "windows" {
		for _, raw := range []string{"\\Users", "/Users"} {
			if _, err := ResolveWorkspaceDir(raw); err == nil || !strings.Contains(err.Error(), "workspace must be an absolute path") {
				t.Errorf("volume-less %q: err = %v, want the absolute-path refusal", raw, err)
			}
		}
	}
}
