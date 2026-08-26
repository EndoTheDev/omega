package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// ProjectRoot returns the nearest directory (walking up from dir) that
// contains an AGENTS.md, or "" if none exists. This is the trust unit:
// a project is trusted by its root directory, not by individual files.
func ProjectRoot(dir string) string {
	visited := map[string]bool{}
	for {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return ""
		}
		if visited[abs] {
			return ""
		}
		visited[abs] = true

		if _, err := os.Stat(filepath.Join(abs, "AGENTS.md")); err == nil {
			return abs
		}

		parent := filepath.Dir(abs)
		if parent == abs {
			return ""
		}
		dir = parent
	}
}

// LoadProjectContext walks from dir up to the filesystem root,
// collecting AGENTS.md files at each level. Results are concatenated
// in root-to-leaf order (outermost project first, nearest last) so
// the nearest context has the most influence. Non-existent files are
// silently skipped. Read errors (permission denied, etc.) produce a
// warning line instead of being silently dropped.
func LoadProjectContext(dir string) string {
	var parts []string
	visited := map[string]bool{}

	for {
		abs, err := filepath.Abs(dir)
		if err != nil {
			break
		}
		if visited[abs] {
			break
		}
		visited[abs] = true

		path := filepath.Join(abs, "AGENTS.md")
		data, err := os.ReadFile(path)
		if err == nil {
			parts = append(parts, string(data))
		} else if !os.IsNotExist(err) {
			parts = append(parts, fmt.Sprintf("[warning: could not read %s: %v]", path, err))
		}

		parent := filepath.Dir(abs)
		if parent == abs {
			break
		}
		dir = parent
	}

	// Reverse so root is first, CWD is last.
	slices.Reverse(parts)
	return strings.Join(parts, "\n\n")
}