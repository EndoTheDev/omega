package datadir

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ENDO'S LAW 2026-10-18: the tool layer refuses the accidental class of
// database access — file paths and shell commands that resolve into the
// instance database (omega.db + -wal/-shm), symlinks included. These
// tests pin the matcher; the consuming tool capsules (harness/files,
// harness/terminal) pin the four guarded tool entry points.

// guardFor builds a Guard against a fake instance db in dir.
func guardFor(t *testing.T, dir string) *Guard {
	t.Helper()
	return NewGuard(filepath.Join(dir, "omega.db"))
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
		if got := g.DenyFile(tc.path); got != tc.deny {
			t.Errorf("%s: DenyFile(%q) = %v, want %v", tc.name, tc.path, got, tc.deny)
		}
	}
}

// TestDBGuardDenyShellBasename: the raw command string is grepped for
// the db basename — the heuristic ceiling of a deny-list.
func TestDBGuardDenyShellBasename(t *testing.T) {
	dir := t.TempDir()
	g := guardFor(t, dir)

	cases := map[string]bool{
		"sqlite3 /home/x/.omega/omega.db 'select 1'":           true,
		"cp /home/x/.omega/omega.db-wal /tmp/bak":              true,
		"cat /home/x/.omega/omega.db-shm":                      true,
		"ls ~/.omega":                                          false,
		"echo omega":                                           false,
		"touch /tmp/omega.config":                              false,
		"grep omega /etc/passwd":                               false,
		"cat /home/x/omega.db-notes.md":                        true, // basename grep: dishonest ceiling, but loud
		"sqlite3 ${HOME}/.omega/omega.db 'select * from task'": true,
	}
	for cmd, want := range cases {
		if got := g.DenyShell(cmd); got != want {
			t.Errorf("DenyShell(%q) = %v, want %v", cmd, got, want)
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
		if got := g.DenyShell(cmd); got != want {
			t.Errorf("DenyShell(%q) = %v, want %v", cmd, got, want)
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
	if !g.DenyFile(link) {
		t.Error("symlink to omega.db was not denied")
	}
}

// TestDBGuardHomelessInstall: the fallback Guard against the standard
// install layout (<home>/data/omega.db, data/ under OMEGA_HOME or
// ~/.omega) still denies.
func TestDBGuardHomelessInstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	g := NewGuard("") // store config absent — standard layout fallback
	if !g.DenyShell("sqlite3 " + home + "/data/omega.db 'select 1'") {
		t.Error("OMEGA_HOME fallback guard missed the data db path")
	}
	if !g.DenyFile(home + "/data/omega.db-wal") {
		t.Error("OMEGA_HOME fallback guard missed the WAL sibling")
	}
	if g.DenyShell("ls " + home) {
		t.Error("OMEGA_HOME fallback guard denied the home directory itself")
	}
}

// TestDBDeniedNamesTheLaw: a denial error carries the law string and
// the loud ENDO'S LAW marker, so a refusal is never a bare error.
func TestDBDeniedNamesTheLaw(t *testing.T) {
	if err := DBDenied("/x"); err == nil ||
		!strings.Contains(err.Error(), DBDenyLaw) ||
		!strings.Contains(err.Error(), "ENDO'S LAW") {
		t.Errorf("DBDenied err = %v, want loud refusal naming the law", err)
	}
}