package trust

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTrustStoreRoundTrip(t *testing.T) {
	p := NewProvider(t.TempDir())
	entries := []TrustEntry{
		{Path: "/a/b", Level: "parent"},
		{Path: "/a/b/c", Level: "exact"},
	}
	if err := p.saveTrusted(entries); err != nil {
		t.Fatalf("saveTrusted: %v", err)
	}
	got := p.loadTrusted()
	if len(got) != 2 {
		t.Fatalf("loadTrusted = %d entries, want 2", len(got))
	}
	if got[0].Path != "/a/b" || got[0].Level != "parent" {
		t.Errorf("entry[0] = %+v, want {/a/b parent}", got[0])
	}
	if got[1].Path != "/a/b/c" || got[1].Level != "exact" {
		t.Errorf("entry[1] = %+v, want {/a/b/c exact}", got[1])
	}
}

func TestLoadTrustedMissingReturnsEmpty(t *testing.T) {
	p := NewProvider(t.TempDir())
	if got := p.loadTrusted(); got != nil {
		t.Fatalf("loadTrusted on missing store = %v, want nil", got)
	}
}

func TestIsTrusted(t *testing.T) {
	entries := []TrustEntry{
		{Path: "/a/b", Level: "parent"},
		{Path: "/x/y", Level: "exact"},
	}
	tests := []struct {
		dir  string
		want bool
	}{
		{"/a/b", true},
		{"/a/b/c", true},
		{"/a/b/c/d/e", true},
		{"/a", false},
		{"/a/bc", false},
		{"/x/y", true},
		{"/x/y/z", false},
		{"/unrelated", false},
	}
	for _, tt := range tests {
		if got := isTrusted(entries, tt.dir); got != tt.want {
			t.Errorf("isTrusted(%q) = %v, want %v", tt.dir, got, tt.want)
		}
	}
}

func TestResolveProjectContextNoAGENTS(t *testing.T) {
	p := NewProvider(t.TempDir())
	if got := p.ResolveContext(t.TempDir(), false, false, false); got != "" {
		t.Fatalf("ResolveContext on empty dir = %q, want \"\"", got)
	}
}

func TestResolveProjectContextApprove(t *testing.T) {
	p := NewProvider(t.TempDir())
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# rules"), 0o600); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}
	got := p.ResolveContext(dir, true, false, false)
	if got == "" {
		t.Fatal("ResolveContext with --approve returned empty, want context")
	}
	entries := p.loadTrusted()
	if len(entries) != 1 || entries[0].Level != "exact" {
		t.Fatalf("trust store after --approve = %+v, want one exact entry", entries)
	}
}

func TestResolveProjectContextNoApprove(t *testing.T) {
	p := NewProvider(t.TempDir())
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# rules"), 0o600); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}
	if got := p.ResolveContext(dir, true, true, false); got != "" {
		t.Fatalf("ResolveContext with --no-approve = %q, want \"\"", got)
	}
}

func TestResolveProjectContextUntrustedNonInteractive(t *testing.T) {
	p := NewProvider(t.TempDir())
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# rules"), 0o600); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}
	if got := p.ResolveContext(dir, false, false, false); got != "" {
		t.Fatalf("ResolveContext untrusted = %q, want \"\"", got)
	}
}

func TestResolveProjectContextTrustedParent(t *testing.T) {
	p := NewProvider(t.TempDir())
	root := t.TempDir()
	child := filepath.Join(root, "sub")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("# rules"), 0o600); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}
	if err := p.saveTrusted([]TrustEntry{{Path: root, Level: "parent"}}); err != nil {
		t.Fatalf("saveTrusted: %v", err)
	}
	if got := p.ResolveContext(child, false, false, false); got == "" {
		t.Fatal("ResolveContext with trusted parent returned empty, want context")
	}
}

func TestTrustState(t *testing.T) {
	p := NewProvider(t.TempDir())
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# rules"), 0o600); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}

	if got := p.State(t.TempDir(), false, false); got != "" {
		t.Fatalf("State(no AGENTS) = %q, want \"\"", got)
	}
	if got := p.State(dir, false, false); got != "untrusted" {
		t.Fatalf("State(untrusted) = %q, want untrusted", got)
	}
	if got := p.State(dir, true, false); got != "trusted" {
		t.Fatalf("State(--approve) = %q, want trusted", got)
	}
	if got := p.State(dir, true, true); got != "untrusted" {
		t.Fatalf("State(--no-approve) = %q, want untrusted", got)
	}
	if err := p.saveTrusted([]TrustEntry{{Path: dir, Level: "exact"}}); err != nil {
		t.Fatalf("saveTrusted: %v", err)
	}
	if got := p.State(dir, false, false); got != "trusted" {
		t.Fatalf("State(trusted) = %q, want trusted", got)
	}
}
