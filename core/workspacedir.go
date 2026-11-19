// ResolveWorkspaceDir: the workspace-path validator.
package core

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// ResolveWorkspaceDir validates and normalizes a user-supplied workspace path: it must
// be absolute, exist, be a directory, and survive symlink resolution.
func ResolveWorkspaceDir(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", errors.New("path must not be empty")
	}
	if !filepath.IsAbs(raw) {
		return "", errors.New("workspace must be an absolute path")
	}
	abs, err := filepath.EvalSymlinks(filepath.Clean(raw))
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("not a directory")
	}
	return abs, nil
}
