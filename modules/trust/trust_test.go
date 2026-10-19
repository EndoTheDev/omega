package trust

import (
	"os"
	"path/filepath"
	"strings"
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

// TestTrustPathConfigLayout is the Endo 2026-10-18 layout: with no
// home-root trust.yaml the live store lives in config/, and writes
// (new trust grants) land there too.
func TestTrustPathConfigLayout(t *testing.T) {
	home := t.TempDir()
	p := NewProvider(home)
	want := filepath.Join(home, "config", "trust.yaml")
	if got := p.trustPath(); got != want {
		t.Fatalf("trustPath = %q, want %q", got, want)
	}
	if err := p.saveTrusted([]TrustEntry{{Path: "/a", Level: "exact"}}); err != nil {
		t.Fatalf("saveTrusted: %v", err)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("store not written to %s: %v", want, err)
	}
	if _, err := os.Stat(filepath.Join(home, "trust.yaml")); !os.IsNotExist(err) {
		t.Fatalf("root trust.yaml created on new-layout home, want none")
	}
	if got := p.loadTrusted(); len(got) != 1 || got[0].Path != "/a" {
		t.Fatalf("loadTrusted = %+v, want one /a entry", got)
	}
}

// TestTrustPathRootWins is the upgrade law: when a legacy home-root
// trust.yaml exists it resolves first and every write goes to it -
// the config/ copy (or absence of one) is never touched.
func TestTrustPathRootWins(t *testing.T) {
	home := t.TempDir()
	rootPath := filepath.Join(home, "trust.yaml")
	if err := os.WriteFile(rootPath, []byte("trusted: [{path: /legacy, level: exact}]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := NewProvider(home)
	if got := p.trustPath(); got != rootPath {
		t.Fatalf("trustPath = %q, want root %q (legacy wins)", got, rootPath)
	}
	// A new grant lands in the root store, the live authoritative one.
	if err := p.saveTrusted([]TrustEntry{{Path: "/legacy", Level: "exact"}, {Path: "/new", Level: "exact"}}); err != nil {
		t.Fatalf("saveTrusted: %v", err)
	}
	if data, err := os.ReadFile(rootPath); err != nil || !strings.Contains(string(data), "/new") {
		t.Fatalf("root store not updated: %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(home, "config", "trust.yaml")); !os.IsNotExist(err) {
		t.Fatalf("config/ trust.yaml created despite authoritative root store")
	}
}

// TestBootstrapMigratesRootStore covers the (a) verify case: a home
// with only a root trust.yaml gets a config/ copy on boot, root stays
// byte-identical, and the root file remains authoritative for reads.
func TestBootstrapMigratesRootStore(t *testing.T) {
	home := t.TempDir()
	content := []byte("trusted:\n  - path: /proj\n    level: parent\n")
	if err := os.WriteFile(filepath.Join(home, "trust.yaml"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	Bootstrap(home)
	cfgCopy, err := os.ReadFile(filepath.Join(home, "config", "trust.yaml"))
	if err != nil {
		t.Fatalf("config/trust.yaml not migrated: %v", err)
	}
	if string(cfgCopy) != string(content) {
		t.Fatalf("migrated copy = %q, want %q", cfgCopy, content)
	}
	before, err := os.ReadFile(filepath.Join(home, "trust.yaml"))
	if err != nil || string(before) != string(content) {
		t.Fatalf("root trust.yaml touched by migration: %q, %v", before, err)
	}
	// Root still wins: reads see the root entries even after migration.
	p := NewProvider(home)
	if got, want := p.trustPath(), filepath.Join(home, "trust.yaml"); got != want {
		t.Fatalf("trustPath after migration = %q, want root %q", got, want)
	}
	// Idempotent: a second boot rewrites nothing.
	Bootstrap(home)
	if before2, err := os.ReadFile(filepath.Join(home, "trust.yaml")); err != nil || string(before2) != string(content) {
		t.Fatalf("second boot changed root trust.yaml: %q, %v", before2, err)
	}
}

// TestBootstrapBothPresentRootWins covers the (b) verify case: both
// stores present means nothing is copied, overwritten or deleted - the
// diverging config/ content stays untouched and the root content stays
// untouched.
func TestBootstrapBothPresentRootWins(t *testing.T) {
	home := t.TempDir()
	rootContent := []byte("trusted: [{path: /root, level: exact}]\n")
	cfgContent := []byte("trusted: [{path: /config, level: exact}]\n")
	if err := os.WriteFile(filepath.Join(home, "trust.yaml"), rootContent, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "config", "trust.yaml"), cfgContent, 0o600); err != nil {
		t.Fatal(err)
	}
	Bootstrap(home)
	if data, err := os.ReadFile(filepath.Join(home, "trust.yaml")); err != nil || string(data) != string(rootContent) {
		t.Fatalf("root trust.yaml changed: %q, %v", data, err)
	}
	if data, err := os.ReadFile(filepath.Join(home, "config", "trust.yaml")); err != nil || string(data) != string(cfgContent) {
		t.Fatalf("config/trust.yaml changed: %q, %v", data, err)
	}
	// And the root is the one that resolves.
	p := NewProvider(home)
	if got := p.loadTrusted(); len(got) != 1 || got[0].Path != "/root" {
		t.Fatalf("loadTrusted = %+v, want the /root entry (root wins)", got)
	}
}

// TestBootstrapConfigOnlyNoop covers the (c) verify case: existing
// config/-only installs are untouched by migration.
func TestBootstrapConfigOnlyNoop(t *testing.T) {
	home := t.TempDir()
	cfgContent := []byte("trusted: [{path: /config, level: exact}]\n")
	if err := os.MkdirAll(filepath.Join(home, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "config", "trust.yaml"), cfgContent, 0o600); err != nil {
		t.Fatal(err)
	}
	Bootstrap(home)
	if data, err := os.ReadFile(filepath.Join(home, "config", "trust.yaml")); err != nil || string(data) != string(cfgContent) {
		t.Fatalf("config/trust.yaml changed: %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(home, "trust.yaml")); !os.IsNotExist(err) {
		t.Fatalf("Bootstrap invented a root trust.yaml")
	}
}

// TestBootstrapWithoutTrustFileNoop: no stores anywhere, migration is
// a no-op (and must not create config/ or trust.yaml out of nowhere).
func TestBootstrapWithoutTrustFileNoop(t *testing.T) {
	home := t.TempDir()
	Bootstrap(home)
	if _, err := os.Stat(filepath.Join(home, "config", "trust.yaml")); !os.IsNotExist(err) {
		t.Fatalf("config/trust.yaml created without any source")
	}
	if _, err := os.Stat(filepath.Join(home, "config")); !os.IsNotExist(err) {
		t.Fatalf("config/ created by Bootstrap alone: %v", err)
	}
}

// TestResolveContextRootStore covers the (d) verify case end to end at
// the trust boundary: an existing install's root trust.yaml still
// resolves a project as trusted and its AGENTS.md context is loaded.
func TestResolveContextRootStore(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A config/ store exists (migrated) but does NOT cover our project;
	// the authoritative root store does.
	if err := os.WriteFile(filepath.Join(home, "config", "trust.yaml"), []byte("trusted: [{path: /elsewhere, level: exact}]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := NewProvider(home)
	// A legacy root store exists: it must resolve ahead of config/.
	if err := os.WriteFile(filepath.Join(home, "trust.yaml"), []byte("trusted: [{path: /ancient, level: exact}]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, want := p.trustPath(), filepath.Join(home, "trust.yaml"); got != want {
		t.Fatalf("trustPath = %q, want root %q (legacy wins)", got, want)
	}
	proj := filepath.Join(t.TempDir(), "sub")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "AGENTS.md"), []byte("# rules"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The root store does NOT cover proj yet - add the grant to it.
	if err := p.saveTrusted([]TrustEntry{{Path: proj, Level: "parent"}}); err != nil {
		t.Fatalf("saveTrusted: %v", err)
	}
	if got := p.ResolveContext(proj, false, false, false); got == "" {
		t.Fatal("ResolveContext with root-store parent trust = \"\", want context (root store resolves)")
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
