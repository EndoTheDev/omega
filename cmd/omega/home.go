package main

import (
	"embed"
	"os"
	"path/filepath"
)

//go:embed examples/config.yaml.example examples/mcp.yaml.example
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
func bootstrapHome(home string) {
	os.MkdirAll(home, 0o755)
	for _, name := range []string{"config.yaml.example", "mcp.yaml.example"} {
		dst := filepath.Join(home, name)
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		data, err := exampleFiles.ReadFile("examples/" + name)
		if err != nil {
			continue // embedded file missing: cosmetic, not fatal
		}
		_ = os.WriteFile(dst, data, 0o644)
	}
}