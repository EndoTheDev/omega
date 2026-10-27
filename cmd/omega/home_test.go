package main

import (
	"os"
	"path/filepath"
	"testing"
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
	// The three infrastructure example templates now land in <home>/config/
	// (Endo 2026-10-18); omega.db, memory.md, user.md, skills/ stay at root.
	for _, name := range []string{
		"config.yaml.example", "mcp.yaml.example", "crons.yaml.example",
	} {
		if _, err := os.Stat(filepath.Join(home, "config", name)); err != nil {
			t.Fatalf("bootstrap did not write config/%s: %v", name, err)
		}
	}
	// Nothing is seeded for the memory store: a fresh home boots WITHOUT
	// memory.md/user.md - they are created by the agent's first
	// 'memory add'. Nothing is shipped that becomes agent context.
	for _, name := range []string{"memory.md", "user.md"} {
		if _, err := os.Stat(filepath.Join(home, name)); !os.IsNotExist(err) {
			t.Fatalf("bootstrap seeded %s, want none (agent creates it on first use)", name)
		}
	}
	// config.yaml stays example-only: omega runs on defaults, no live
	// config is ever written (neither at root nor in config/).
	for _, p := range []string{
		filepath.Join(home, "config.yaml"),
		filepath.Join(home, "config", "config.yaml"),
	} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("bootstrap wrote a live %s, want none", p)
		}
	}

	// idempotent: existing files are never overwritten
	existing := filepath.Join(home, "config", "config.yaml.example")
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
// comes out of bootstrap byte-identical - bootstrap never touches live
// memory.md/user.md (it doesn't seed them either; see TestBootstrapHome).
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

func TestBootstrapHomeCreatesDir(t *testing.T) {
	home := filepath.Join(t.TempDir(), "nested", ".omega")
	bootstrapHome(home)
	if _, err := os.Stat(filepath.Join(home, "config", "config.yaml.example")); err != nil {
		t.Fatalf("bootstrap did not create nested home config/: %v", err)
	}
}

// TestBootstrapHomeConfigSubdir is the Endo 2026-10-18 layout law: a
// fresh home gets exactly <home>/config/{config,mcp,crons}.yaml.example
// and nothing else writable - omega.db/memory.md/user.md/skills/ stay
// absent until the agent or operator creates them.
func TestBootstrapHomeConfigSubdir(t *testing.T) {
	home := t.TempDir()
	bootstrapHome(home)
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "config" {
		t.Fatalf("fresh home should contain only config/, got %v", entries)
	}
}

// TestBootstrapHomeMigratesTrust fires the trust.yaml migration through
// the real boot path: a home with a legacy root trust.yaml comes out of
// bootstrapHome with a config/ copy, root untouched. Nothing is
// invented on a fresh home (no source, no copy).
func TestBootstrapHomeMigratesTrust(t *testing.T) {
	home := t.TempDir()
	content := []byte("trusted: [{path: /legacy, level: exact}]\n")
	if err := os.WriteFile(filepath.Join(home, "trust.yaml"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	bootstrapHome(home)
	cfgCopy, err := os.ReadFile(filepath.Join(home, "config", "trust.yaml"))
	if err != nil || string(cfgCopy) != string(content) {
		t.Fatalf("bootstrapHome did not migrate trust.yaml to config/: %q, %v", cfgCopy, err)
	}
	before, err := os.ReadFile(filepath.Join(home, "trust.yaml"))
	if err != nil || string(before) != string(content) {
		t.Fatalf("bootstrapHome touched root trust.yaml: %q, %v", before, err)
	}

	// Fresh home: no source, no copy invented.
	fresh := t.TempDir()
	bootstrapHome(fresh)
	if _, err := os.Stat(filepath.Join(fresh, "config", "trust.yaml")); !os.IsNotExist(err) {
		t.Fatalf("bootstrap invented config/trust.yaml without a source")
	}
}

// TestLoggingFileNeverCwd pins the log-path law (card t_0ccda501):
// the construction default carries no filename, resolveHomePaths routes
// the empty default and the legacy "omega.log" alias to the home root,
// and the resolved path can never land in the process CWD.
func TestLoggingFileNeverCwd(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	t.Chdir(cwd)

	cfg := DefaultConfig()
	if cfg.Logging.File != "" {
		t.Errorf("DefaultConfig Logging.File = %q, want empty (no cwd-relative default)", cfg.Logging.File)
	}
	if err := resolveHomePaths(&cfg); err != nil {
		t.Fatalf("resolveHomePaths: %v", err)
	}
	want := filepath.Join(home, "omega.log")
	if cfg.Logging.File != want {
		t.Errorf("resolved Logging.File = %q, want %q", cfg.Logging.File, want)
	}

	// The legacy bare alias resolves to the home root too.
	legacy := DefaultConfig()
	legacy.Logging.File = "omega.log"
	if err := resolveHomePaths(&legacy); err != nil {
		t.Fatalf("resolveHomePaths: %v", err)
	}
	if legacy.Logging.File != want {
		t.Errorf("legacy alias Logging.File = %q, want %q", legacy.Logging.File, want)
	}

	// An explicit absolute path is left alone.
	custom := DefaultConfig()
	custom.Logging.File = filepath.Join(home, "custom", "log.txt")
	if err := resolveHomePaths(&custom); err != nil {
		t.Fatalf("resolveHomePaths: %v", err)
	}
	if custom.Logging.File != filepath.Join(home, "custom", "log.txt") {
		t.Errorf("custom Logging.File = %q, want unchanged", custom.Logging.File)
	}

	// Nothing appeared in the CWD while all of this resolved.
	if entries, err := os.ReadDir(cwd); err != nil || len(entries) != 0 {
		t.Errorf("log file leaked into CWD: entries=%v err=%v", entries, err)
	}
}
