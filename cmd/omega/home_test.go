package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/extensions/memory"
)

// TestCwd verifies that cwd() returns a non-empty string matching
// the actual working directory.
func TestCwd(t *testing.T) {
	got := cwd()
	if got == "" {
		t.Fatal("cwd() returned empty string, want non-empty")
	}
	want, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	if got != want {
		t.Errorf("cwd() = %q, want %q", got, want)
	}
}

// TestOmegaHomeEnvVar verifies the OMEGA_HOME env var branch.
func TestOmegaHomeEnvVar(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OMEGA_HOME", dir)
	if got := omegaHome(); got != dir {
		t.Errorf("omegaHome() = %q, want %q", got, dir)
	}
}

// TestOmegaHomeBinaryDir verifies the os.Executable() branch when
// OMEGA_HOME is unset.
func TestOmegaHomeBinaryDir(t *testing.T) {
	t.Setenv("OMEGA_HOME", "")
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	want := filepath.Dir(exe)
	if got := omegaHome(); got != want {
		t.Errorf("omegaHome() = %q, want %q", got, want)
	}
}

// TestOmegaHomeFallback verifies that omegaHome() returns a non-empty
// string when OMEGA_HOME is unset (the os.Executable() branch should
// succeed in the test binary, so this confirms a valid path is returned).
func TestOmegaHomeFallback(t *testing.T) {
	t.Setenv("OMEGA_HOME", "")
	got := omegaHome()
	if got == "" {
		t.Fatal("omegaHome() returned empty string, want non-empty")
	}
}

func TestHomeForExe(t *testing.T) {
	cases := []struct {
		name    string
		envHome string
		exePath string
		want    string
	}{
		// OMEGA_HOME always wins
		{"env override", "/custom/home", "/home/u/go/bin/omega", "/custom/home"},
		// release-folder install: home = exe dir
		{"release folder", "", "/home/u/omega/omega", "/home/u/omega"},
		// dotfolder install: not a bin dir, stays put
		{"dotfolder", "", "/home/u/.omega/omega", "/home/u/.omega"},
		// go install: ~/go/bin is a bin dir -> ~/.omega
		{"go install", "", "/home/u/go/bin/omega", "/home/u/.omega"},
		// local bin dir -> ~/.omega
		{"local bin", "", "/home/u/.local/bin/omega", "/home/u/.omega"},
		// system PATH install -> ~/.omega
		{"usr local bin", "", "/usr/local/bin/omega", "/home/u/.omega"},
		// repo build output: a bin/ subfolder of a checkout keeps home
		// beside the binary (the v0.6.3 base=="bin" heuristic broke this)
		{"repo build", "", "/home/u/dev/omega/bin/omega", "/home/u/dev/omega/bin"},
		// unresolvable exe path -> ~/.omega
		{"no exe", "", "", "/home/u/.omega"},
	}
	for _, c := range cases {
		if got := homeForExe(c.envHome, c.exePath, "/home/u"); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestBootstrapHome(t *testing.T) {
	home := t.TempDir()

	bootstrapHome(home)
	for _, name := range []string{
		"config.yaml.example", "mcp.yaml.example", "crons.yaml.example",
		"memory.md.example", "user.md.example",
		// live memory store files are also seeded on first run
		"memory.md", "user.md",
	} {
		if _, err := os.Stat(filepath.Join(home, name)); err != nil {
			t.Fatalf("bootstrap did not write %s: %v", name, err)
		}
	}
	// config.yaml stays example-only: omega runs on defaults, no live
	// config is ever written.
	if _, err := os.Stat(filepath.Join(home, "config.yaml")); !os.IsNotExist(err) {
		t.Fatal("bootstrap wrote a live config.yaml, want none")
	}

	// idempotent: existing files are never overwritten
	existing := filepath.Join(home, "config.yaml.example")
	if err := os.WriteFile(existing, []byte("do not touch"), 0o644); err != nil {
		t.Fatal(err)
	}
	bootstrapHome(home)
	data, err := os.ReadFile(existing)
	if err != nil || string(data) != "do not touch" {
		t.Fatalf("bootstrap overwrote existing file: %q, %v", data, err)
	}
}

// TestBootstrapHomeNeverOverwritesMemory is the upgrade-path law: an
// existing home whose memory store the user (or the agent) has modified
// comes out of bootstrap byte-identical.
func TestBootstrapHomeNeverOverwritesMemory(t *testing.T) {
	home := t.TempDir()
	mem := filepath.Join(home, "memory.md")
	usr := filepath.Join(home, "user.md")
	before := []byte("endocustom accumulated memory entry - do not lose\nsecond entry\n")
	if err := os.WriteFile(mem, before, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(usr, []byte("Name: someone\nLives somewhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bootstrapHome(home)
	for path, want := range map[string][]byte{mem: before, usr: []byte("Name: someone\nLives somewhere\n")} {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if string(got) != string(want) {
			t.Fatalf("bootstrap overwrote %s: got %q, want %q", path, got, want)
		}
	}
}

// TestBootstrapHomeSeedsMemoryParses proves the seeded files are valid
// input for the memory extension's reader: the snapshot path reads them
// without error and yields non-empty prompt blocks.
func TestBootstrapHomeSeedsMemoryParses(t *testing.T) {
	home := t.TempDir()
	bootstrapHome(home)
	fm := memory.NewFileMemory(memory.Config{
		Enabled:              true,
		UserProfileEnabled:   true,
		CharLimit:            memory.Default().CharLimit,
		UserProfileCharLimit: memory.Default().UserProfileCharLimit,
		File:                 filepath.Join(home, "memory.md"),
		UserProfileFile:      filepath.Join(home, "user.md"),
	})
	snap := fm.Snapshot()
	if !strings.Contains(snap, "MEMORY") || !strings.Contains(snap, "USER PROFILE") {
		t.Fatalf("snapshot missing sections:\n%s", snap)
	}
	memEntries, err := fm.List("memory")
	if err != nil || len(memEntries) != 1 {
		t.Fatalf("List(memory) = %v, %v, want 1 entry", memEntries, err)
	}
	usrEntries, err := fm.List("user")
	if err != nil || len(usrEntries) != 1 {
		t.Fatalf("List(user) = %v, %v, want 1 entry", usrEntries, err)
	}
}

// TestSeedContentFitsLimits keeps the seeded starters inside the
// default store limits, so the first memory add never trips the
// would-exceed error out of the box.
func TestSeedContentFitsLimits(t *testing.T) {
	cases := []struct {
		example string
		limit   int
	}{
		{"memory.md.example", memory.Default().CharLimit},
		{"user.md.example", memory.Default().UserProfileCharLimit},
	}
	for _, c := range cases {
		data, err := exampleFiles.ReadFile("examples/" + c.example)
		if err != nil {
			t.Fatalf("read embedded %s: %v", c.example, err)
		}
		if len(data) > c.limit {
			t.Errorf("%s is %d chars, exceeds default limit %d", c.example, len(data), c.limit)
		}
	}
}

func TestBootstrapHomeCreatesDir(t *testing.T) {
	home := filepath.Join(t.TempDir(), "nested", ".omega")
	bootstrapHome(home)
	if _, err := os.Stat(filepath.Join(home, "config.yaml.example")); err != nil {
		t.Fatalf("bootstrap did not create nested home: %v", err)
	}
}