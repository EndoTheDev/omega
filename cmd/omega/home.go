package main

import (
	"embed"
	"os"
	"path/filepath"
)

//go:embed examples/config.yaml.example examples/mcp.yaml.example examples/crons.yaml.example examples/memory.md.example examples/user.md.example
var exampleFiles embed.FS

// homeForExe resolves the omega home: OMEGA_HOME wins, else the exe's
// directory - except for PATH-style installs, where the binary lives in
// a shared bin directory (go install's ~/go/bin, ~/.local/bin, ~/bin,
// /usr/local/bin) and home moves to ~/.omega so config, db and logs
// don't scatter among other programs' binaries.
// ponytail: explicit known-bin-dir list; an exotic PATH dir
// (/opt/homebrew/bin) falls back to exe-dir home - OMEGA_HOME overrides.
func homeForExe(envHome, exePath, userHome string) string {
	if envHome != "" {
		return envHome
	}
	if exePath == "" {
		return filepath.Join(userHome, ".omega")
	}
	dir := filepath.Dir(exePath)
	if isPathBinInstall(dir, userHome) {
		return filepath.Join(userHome, ".omega")
	}
	return dir
}

// isPathBinInstall reports whether dir is a known PATH-style bin
// directory where a self-contained home is impossible. A repo's bin/
// build output (e.g. ~/dev/omega/bin) is deliberately NOT in this list -
// repo builds keep config, db and examples beside the binary.
func isPathBinInstall(dir, userHome string) bool {
	return dir == filepath.Join(userHome, "go", "bin") ||
		dir == filepath.Join(userHome, ".local", "bin") ||
		dir == filepath.Join(userHome, "bin") ||
		dir == "/usr/local/bin"
}

// bootstrapHome makes a fresh install self-documenting: writes the
// example configs into the home dir (idempotent - never overwrites),
// creating the home if it doesn't exist yet (~/.omega case). omega runs
// on defaults without config.yaml, so only the examples are written.
// memory.md and user.md are also seeded live: they are the memory
// extension's store, and a fresh install should start with its memory
// and user profile already populated. Same law as the examples -
// absent-only, never overwriting. A user's accumulated memory is
// sacred on upgrade.
func bootstrapHome(home string) {
	os.MkdirAll(home, 0o755)
	// Only ever written: the .example files. A live crons.yaml is
	// always human-authored - omega proposes jobs, it never writes
	// its own schedule (trust gate).
	for _, name := range []string{"config.yaml.example", "mcp.yaml.example", "crons.yaml.example", "memory.md.example", "user.md.example"} {
		seedFile(home, name, name)
	}
	// Live memory store, seeded from the examples.
	seedFile(home, "memory.md", "memory.md.example")
	seedFile(home, "user.md", "user.md.example")
}

// seedFile writes the embedded example src to home/dst when dst does
// not exist yet. Failures are non-fatal: omega runs fine without any
// of these files (defaults, or the memory reader tolerating an empty
// store).
func seedFile(home, dst, src string) {
	target := filepath.Join(home, dst)
	if _, err := os.Stat(target); err == nil {
		return
	}
	data, err := exampleFiles.ReadFile("examples/" + src)
	if err != nil {
		return // embedded file missing: cosmetic, not fatal
	}
	_ = os.WriteFile(target, data, 0o644)
}
