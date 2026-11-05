// ResolveWorkspaceDir: the workspace-path validator (card t_9de5b009,
// born in agent/api's workspaces surface, homed here by the shell-ring
// shrink wave 3 - card t_b75df168 - so the chat/session creation path
// in agent/api and the files capsule's workspace REST face can share it
// without an agent->harness import).
package core

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// ResolveWorkspaceDir validates and normalizes a user-supplied workspace
// path: it must be absolute, exist, be a directory, and survive symlink
// resolution. Returns the cleaned absolute path. Hidden segments are NOT
// rejected here - a project may legitimately live under a dot directory
// (~/projects/.local); typing such a path is allowed even though the
// browse listing filters hidden entries out of the picker's view.
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
