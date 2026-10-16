package main

import (
	"embed"
	"os"
	"path/filepath"
)

//go:embed examples/config.yaml.example examples/mcp.yaml.example
var exampleFiles embed.FS

// homeForExe resolves the omega home: OMEGA_HOME wins, else the exe's
// directory - except when the exe lives in a directory literally named
// "bin" (go install -> ~/go/bin, ~/.local/bin, /usr/local/bin), where
// home moves to ~/.omega so config, db and logs don't scatter into the
// bin dir.
// ponytail: ANY folder named "bin" gets the dotfolder home - a portable
// install extracted into a "bin" dir would too. OMEGA_HOME overrides.
func homeForExe(envHome, exePath, userHome string) string {
	if envHome != "" {
		return envHome
	}
	if exePath == "" {
		return filepath.Join(userHome, ".omega")
	}
	dir := filepath.Dir(exePath)
	if filepath.Base(dir) == "bin" {
		return filepath.Join(userHome, ".omega")
	}
	return dir
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