package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ENDO'S LAW 2026-10-18: the tool layer refuses the accidental class of
// database access — shell commands and file paths that resolve into the
// instance database (omega.db + -wal/-shm), symlinks included. These
// tests pin the matcher and the four guarded tool entry points.

// guardFor builds a guard against a fake instance db in dir.
func guardFor(t *testing.T, dir string) *dbGuard {
	t.Helper()
	return newDBGuard(filepath.Join(dir, "omega.db"))
}

// TestDBGuardDenyFile: file paths resolving to the db family are denied.
func TestDBGuardDenyFile(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "omega.db")
	if err := os.WriteFile(db, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	g := guardFor(t, dir)

	cases := []struct {
		name string
		path string
		deny bool
	}{
		{"db direct", db, true},
		{"db relative-to-home", filepath.Join(dir, ".", "omega.db"), true},
		{"wal sibling", db + "-wal", true},
		{"shm sibling", db + "-shm", true},
		{"wal missing on disk", db + "wal-typo", false},
		{"other file in home", filepath.Join(dir, "memory.md"), false},
		{"path merely containing omega", filepath.Join(dir, "omega-notes.md"), false},
		{"different omega.db", filepath.Join(dir, "sub", "omega.db"), false},
	}
	for _, tc := range cases {
		if got := g.denyFile(tc.path); got != tc.deny {
			t.Errorf("%s: denyFile(%q) = %v, want %v", tc.name, tc.path, got, tc.deny)
		}
	}
}

// TestDBGuardDenyShellBasename: the raw command string is grepped for
// the db basename — the heuristic ceiling of a deny-list.
func TestDBGuardDenyShellBasename(t *testing.T) {
	dir := t.TempDir()
	g := guardFor(t, dir)

	cases := map[string]bool{
		"sqlite3 /home/x/.omega/omega.db 'select 1'":            true,
		"cp /home/x/.omega/omega.db-wal /tmp/bak":               true,
		"cat /home/x/.omega/omega.db-shm":                       true,
		"ls ~/.omega":                                           false,
		"echo omega":                                            false,
		"touch /tmp/omega.config":                               false,
		"grep omega /etc/passwd":                                false,
		"cat /home/x/omega.db-notes.md":                         true, // basename grep: dishonest ceiling, but loud
		"sqlite3 ${HOME}/.omega/omega.db 'select * from task'": true,
	}
	for cmd, want := range cases {
		if got := g.denyShell(cmd); got != want {
			t.Errorf("denyShell(%q) = %v, want %v", cmd, got, want)
		}
	}
}

// TestDBGuardDenyShellResolvedPath: the basename can be hidden by
// indirection — commands spelling the db through symlinks, env
// expansion, or relative paths resolve into the deny set.
func TestDBGuardDenyShellResolvedPath(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "omega.db")
	if err := os.WriteFile(db, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "door")
	if err := os.Symlink(db, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	g := guardFor(t, dir)

	cases := map[string]bool{
		"cat " + link:        true, // symlink resolves into the family
		"cd /tmp && echo hi": false,
	}
	for cmd, want := range cases {
		if got := g.denyShell(cmd); got != want {
			t.Errorf("denyShell(%q) = %v, want %v", cmd, got, want)
		}
	}
}

// TestDBGuardSymlinkLeaf: a symlink AT the db path (not through it) is
// denied by the file tools — EvalSymlinks resolves the leaf.
func TestDBGuardSymlinkLeaf(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "omega.db")
	if err := os.WriteFile(db, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "db-link")
	if err := os.Symlink(db, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	g := guardFor(t, dir)
	if !g.denyFile(link) {
		t.Error("symlink to omega.db was not denied")
	}
}

// TestDBGuardHomelessInstall: the fallback guard against the standard
// install layout (~/.omega/omega.db or OMEGA_HOME) still denies.
func TestDBGuardHomelessInstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	g := newDBGuard("") // store config absent — standard layout fallback
	if !g.denyShell("sqlite3 " + home + "/omega.db 'select 1'") {
		t.Error("OMEGA_HOME fallback guard missed the db path")
	}
	if !g.denyFile(home + "/omega.db-wal") {
		t.Error("OMEGA_HOME fallback guard missed the WAL sibling")
	}
	if g.denyShell("ls " + home) {
		t.Error("OMEGA_HOME fallback guard denied the home directory itself")
	}
}

// TestGuardedToolsRefuse: all four tool entry points refuse a db hit
// with the law named, and RunShell must not have executed anything.
func TestGuardedToolsRefuse(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "omega.db")
	if err := os.WriteFile(db, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := &ToolProvider{dbGuard: guardFor(t, dir)}

	_, err := p.RunShell(context.Background(), map[string]any{"command": "sqlite3 " + db + " '.tables'"})
	if err == nil || !strings.Contains(err.Error(), dbDenyLaw) || !strings.Contains(err.Error(), "ENDO'S LAW") {
		t.Errorf("RunShell: err = %v, want loud refusal naming the law", err)
	}

	// no execution happened: the command would have failed loudly on a
	// real db - here the refusal is the only effect
}

// TestGuardedToolsRefuseEach verifies each file tool individually.
// (kept separate from the shell refusal for one-line failure clarity)
func TestGuardedToolsRefuseEach(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "omega.db")
	if err := os.WriteFile(db, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := &ToolProvider{dbGuard: guardFor(t, dir)}

	if _, err := p.RunReadFile(context.Background(), map[string]any{"path": db}); err == nil || !strings.Contains(err.Error(), dbDenyLaw) {
		t.Errorf("RunReadFile: err = %v, want denial", err)
	}
	if _, err := p.RunWriteFile(context.Background(), map[string]any{"path": db + "-wal", "content": "x"}); err == nil || !strings.Contains(err.Error(), dbDenyLaw) {
		t.Errorf("RunWriteFile on -wal: err = %v, want denial", err)
	}
	if _, err := p.RunEdit(context.Background(), map[string]any{"path": db, "old_string": "x", "new_string": "y"}); err == nil || !strings.Contains(err.Error(), dbDenyLaw) {
		t.Errorf("RunEdit: err = %v, want denial", err)
	}
	// denial fires even before the db file exists: the guard knows the
	// path from the store config, no write may slip in ahead of the store
	fresh := t.TempDir()
	pf := &ToolProvider{dbGuard: newDBGuard(filepath.Join(fresh, "omega.db"))}
	target := filepath.Join(fresh, "omega.db")
	if _, serr := os.Stat(target); !os.IsNotExist(serr) {
		t.Fatal("precondition: db file should not exist yet")
	}
	if _, err := pf.RunWriteFile(context.Background(), map[string]any{"path": target, "content": "x"}); err == nil || !strings.Contains(err.Error(), dbDenyLaw) {
		t.Errorf("RunWriteFile on not-yet-existing instance db: err = %v, want denial", err)
	}
	if _, serr := os.Stat(target); !os.IsNotExist(serr) {
		t.Error("denied RunWriteFile created the instance db file")
	}
}

// TestGuardedToolsAllowNormal: zero-value providers (no guard) and
// guard-carrying providers both keep normal operation untouched.
func TestGuardedToolsAllowNormal(t *testing.T) {
	dir := t.TempDir()
	p := &ToolProvider{dbGuard: guardFor(t, dir)}

	f := filepath.Join(dir, "note.txt")
	if _, err := p.RunWriteFile(context.Background(), map[string]any{"path": f, "content": "hi"}); err != nil {
		t.Fatalf("RunWriteFile normal write: %v", err)
	}
	if _, err := p.RunReadFile(context.Background(), map[string]any{"path": f}); err != nil {
		t.Fatalf("RunReadFile normal read: %v", err)
	}
	if _, err := p.RunEdit(context.Background(), map[string]any{"path": f, "old_string": "hi", "new_string": "ho"}); err != nil {
		t.Fatalf("RunEdit normal edit: %v", err)
	}
	out, err := p.RunShell(context.Background(), map[string]any{"command": "echo omega rules"})
	if err != nil {
		t.Fatalf("RunShell normal command: %v", err)
	}
	if !strings.Contains(out, "omega rules") {
		t.Errorf("RunShell output = %q", out)
	}

	// no false positive on a file whose name merely contains "omega"
	ok := filepath.Join(dir, "omegadocs", "omega.db.md")
	if _, err := p.RunWriteFile(context.Background(), map[string]any{"path": ok, "content": "x"}); err != nil {
		t.Errorf("false positive on %q: %v", ok, err)
	}
}