package agent

import (
	"os"
	"path/filepath"
)

// LoadProjectContext reads AGENTS.md from dir and returns its contents.
// It returns "" when the file does not exist, so an agent can run
// without project context. ponytail: only AGENTS.md is read — no
// .pi/ or .agents/ directory scanning.
func LoadProjectContext(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		return ""
	}
	return string(data)
}
