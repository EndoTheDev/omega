package main

import (
	"embed"
	"os"
	"path/filepath"

	"github.com/EndoTheDev/omega/modules/trust"
)

//go:embed examples/config.yaml.example examples/mcp.yaml.example examples/crons.yaml.example
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
// example configs into the home's config/ subdirectory (idempotent -
// never overwrites), creating the home and config/ if they don't exist
// yet (~/.omega case). omega runs on defaults without config.yaml, so
// only the examples are written. memory.md and user.md are NOT seeded:
// nothing is shipped for them - the memory tool's own description
// teaches the file conventions, and the agent's first 'memory add'
// creates the files. Existing live memory.md/user.md in an instance
// home are simply never touched here.
//
// Only the three infrastructure yaml templates move into config/ (Endo
// 2026-10-18): config.yaml, mcp.yaml, crons.yaml. omega.db, memory.md,
// user.md, skills/, and log files stay at the home root. Resolution of
// the live yamls checks the home root first (legacy installs keep
// working unchanged), then config/ - see resolveConfigPath and the
// mcp/cron loaders.
func bootstrapHome(home string) {
	os.MkdirAll(home, 0o755)
	cfgDir := filepath.Join(home, "config")
	os.MkdirAll(cfgDir, 0o755)
	// Only ever written: the .example files, into config/. A live
	// crons.yaml is always human-authored - omega proposes jobs, it
	// never writes its own schedule (trust gate).
	for _, name := range []string{"config.yaml.example", "mcp.yaml.example", "crons.yaml.example"} {
		seedFile(cfgDir, name, name)
	}
	// Upgrade path: a legacy home-root trust.yaml copies into config/
	// (Endo 2026-10-18 layout) when the target doesn't exist; root
	// stays untouched and authoritative either way. The trust module
	// owns the paths - bootstrap just fires it on every boot.
	trust.Bootstrap(home)
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
