package main

import (
	"os"
	"path/filepath"
	"testing"
)

// setTestHome points omegaHome at a temp dir for the duration of a test.
func setTestHome(t *testing.T) {
	t.Helper()
	t.Setenv("OMEGA_HOME", t.TempDir())
}

func TestTrustStoreRoundTrip(t *testing.T) {
	setTestHome(t)
	entries := []TrustEntry{
		{Path: "/a/b", Level: "parent"},
		{Path: "/a/b/c", Level: "exact"},
	}
	if err := saveTrusted(entries); err != nil {
		t.Fatalf("saveTrusted: %v", err)
	}
	got := loadTrusted()
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
	setTestHome(t)
	if got := loadTrusted(); got != nil {
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
		{"/a/b", true},          // parent matches itself
		{"/a/b/c", true},        // parent matches child
		{"/a/b/c/d/e", true},    // parent matches deep child
		{"/a", false},           // parent does not match ancestor
		{"/a/bc", false},        // prefix but not a path boundary
		{"/x/y", true},          // exact matches itself
		{"/x/y/z", false},       // exact does not match child
		{"/unrelated", false},   // no match
	}
	for _, tt := range tests {
		if got := isTrusted(entries, tt.dir); got != tt.want {
			t.Errorf("isTrusted(%q) = %v, want %v", tt.dir, got, tt.want)
		}
	}
}

func TestParseTrustArgs(t *testing.T) {
	tests := []struct {
		args []string
		want trustFlags
	}{
		{[]string{"hello"}, trustFlags{}},
		{[]string{"--approve"}, trustFlags{approve: true}},
		{[]string{"--no-approve"}, trustFlags{noApprove: true}},
		{[]string{"--approve", "--no-approve"}, trustFlags{approve: true, noApprove: true}},
	}
	for _, tt := range tests {
		got := parseTrustArgs(tt.args)
		if got != tt.want {
			t.Errorf("parseTrustArgs(%v) = %+v, want %+v", tt.args, got, tt.want)
		}
	}
}

func TestStripTrustArgs(t *testing.T) {
	got := stripTrustArgs([]string{"--approve", "what", "--no-approve", "is", "up"})
	want := []string{"what", "is", "up"}
	if len(got) != len(want) {
		t.Fatalf("stripTrustArgs = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestResolveProjectContextNoAGENTS(t *testing.T) {
	setTestHome(t)
	// A temp dir with no AGENTS.md: no gate, empty context.
	if got := resolveProjectContext(t.TempDir(), false, false, false); got != "" {
		t.Fatalf("resolveProjectContext on empty dir = %q, want \"\"", got)
	}
}

func TestResolveProjectContextApprove(t *testing.T) {
	setTestHome(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# rules"), 0o600); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}
	// --approve loads context and persists an exact entry.
	got := resolveProjectContext(dir, true, false, false)
	if got == "" {
		t.Fatal("resolveProjectContext with --approve returned empty, want context")
	}
	entries := loadTrusted()
	if len(entries) != 1 || entries[0].Level != "exact" {
		t.Fatalf("trust store after --approve = %+v, want one exact entry", entries)
	}
}

func TestResolveProjectContextNoApprove(t *testing.T) {
	setTestHome(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# rules"), 0o600); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}
	// --no-approve skips context even when --approve is also set.
	if got := resolveProjectContext(dir, true, true, false); got != "" {
		t.Fatalf("resolveProjectContext with --no-approve = %q, want \"\"", got)
	}
}

func TestResolveProjectContextUntrustedNonInteractive(t *testing.T) {
	setTestHome(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# rules"), 0o600); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}
	// Untrusted, non-interactive: skip with no context.
	if got := resolveProjectContext(dir, false, false, false); got != "" {
		t.Fatalf("resolveProjectContext untrusted = %q, want \"\"", got)
	}
}

func TestResolveProjectContextTrustedParent(t *testing.T) {
	setTestHome(t)
	root := t.TempDir()
	child := filepath.Join(root, "sub")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("# rules"), 0o600); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}
	// Trust the root as parent; the child inherits.
	if err := saveTrusted([]TrustEntry{{Path: root, Level: "parent"}}); err != nil {
		t.Fatalf("saveTrusted: %v", err)
	}
	if got := resolveProjectContext(child, false, false, false); got == "" {
		t.Fatal("resolveProjectContext with trusted parent returned empty, want context")
	}
}

func TestTrustState(t *testing.T) {
	setTestHome(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# rules"), 0o600); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}

	// No AGENTS.md anywhere: empty state.
	if got := trustState(t.TempDir(), false, false); got != "" {
		t.Fatalf("trustState(no AGENTS) = %q, want \"\"", got)
	}
	// Untrusted, no flag.
	if got := trustState(dir, false, false); got != "untrusted" {
		t.Fatalf("trustState(untrusted) = %q, want untrusted", got)
	}
	// --approve.
	if got := trustState(dir, true, false); got != "trusted" {
		t.Fatalf("trustState(--approve) = %q, want trusted", got)
	}
	// --no-approve wins.
	if got := trustState(dir, true, true); got != "untrusted" {
		t.Fatalf("trustState(--no-approve) = %q, want untrusted", got)
	}
	// Already trusted.
	if err := saveTrusted([]TrustEntry{{Path: dir, Level: "exact"}}); err != nil {
		t.Fatalf("saveTrusted: %v", err)
	}
	if got := trustState(dir, false, false); got != "trusted" {
		t.Fatalf("trustState(trusted) = %q, want trusted", got)
	}
}
