package main

import (
	"embed"
	"os"
	"path/filepath"

	"github.com/EndoTheDev/omega/harness/trust"
)

//go:embed examples/config.yaml.example examples/mcp.yaml.example examples/crons.yaml.example
var exampleFiles embed.FS

// homeForExe resolves the omega home: OMEGA_HOME wins, else the exe's directory -
// except for PATH-style installs, where the binary lives in a shared bin directory (go
// install's ~/go/bin, ~/.local/bin, ~/bin, /usr/local/bin) and home moves to ~/.omega
// so config, db and logs don't scatter among other programs' binaries.
func homeForExe(envHome, exePath, userHome string) string {
	if envHome != "" {
		return canonPath(envHome)
	}
	if exePath == "" {
		return canonPath(filepath.Join(userHome, ".omega"))
	}
	dir := filepath.Dir(exePath)
	if isPathBinInstall(dir, userHome) {
		return canonPath(filepath.Join(userHome, ".omega"))
	}
	return canonPath(dir)
}

// isPathBinInstall reports whether dir is a known PATH-style bin directory where a.
// Both sides compare on canon spelling: the callers' fakes and the real exe path carry
// different separators.
func isPathBinInstall(dir, userHome string) bool {
	dir = canonPath(dir)
	return dir == canonPath(filepath.Join(userHome, "go", "bin")) ||
		dir == canonPath(filepath.Join(userHome, ".local", "bin")) ||
		dir == canonPath(filepath.Join(userHome, "bin")) ||
		dir == canonPath("/usr/local/bin")
}

// bootstrapHome makes a fresh install self-documenting: writes the example configs into
// the home's config/ subdirectory (idempotent - never overwrites), creating the home
// and config/ if they don't exist yet (~/.omega case). Existing live memory.md/user.md
// in an instance home are simply never touched here.
func bootstrapHome(home string) {
	os.MkdirAll(home, 0o755)
	cfgDir := filepath.Join(home, "config")
	os.MkdirAll(cfgDir, 0o755)
	// Only ever written: the.example files, into config/. A live crons.yaml is always
	// human-authored - omega proposes jobs, it never writes its own schedule (trust gate).
	for _, name := range []string{"config.yaml.example", "mcp.yaml.example", "crons.yaml.example"} {
		seedFile(cfgDir, name, name)
	}
	// Upgrade path: a legacy home-root trust.yaml copies into config/ when the target
	// doesn't exist.
	trust.Bootstrap(home)
}

// seedFile writes the embedded example src to home/dst when dst does not exist yet.
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
