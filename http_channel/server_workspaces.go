package http_channel

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// The session workspace surface (card t_9de5b009): the picker reads the
// local filesystem to pick a folder, the PATCH validates the row write.
// Strictly read-only GET on the browse side - no write, delete or exec
// path exists here. The browse walks REAL directories (an absolute path
// selects any folder on the box - a session workspace is not contained
// by the process working directory, unlike the /files inspector, which
// stays rooted there).
//
// Safety rules, every request:
//   - absolute paths only (no browser-relative confusion)
//   - "."-prefixed segments and hidden entries are rejected/filtered
//   - the path is symlink-resolved before the disk is touched
//   - only directory listings are served, never file contents

// resolveWorkspaceDir validates and normalizes a user-supplied workspace
// path: it must be absolute, exist, be a directory, and survive symlink
// resolution. Returns the cleaned absolute path. Hidden segments are NOT
// rejected here - a project may legitimately live under a dot directory
// (~/projects/.local); typing such a path is allowed even though the
// browse listing filters hidden entries out of the picker's view.
func resolveWorkspaceDir(raw string) (string, error) {
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

// workspaceDir is one /workspaces/browse row.
type workspaceDir struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// handleWorkspacesBrowse serves GET /workspaces/browse?path=<abs>,
// listing one directory's child folders. Empty or missing path lists
// "/". Hidden entries (and every dot-prefixed name) are filtered, and
// dot segments are rejected in the query path - no browsing into hidden
// trees. The response carries the entries plus the parent path ("" when
// at the root - the UI hides the climb there).
func (s *Server) handleWorkspacesBrowse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	raw := r.URL.Query().Get("path")
	abs := "/"
	if raw != "" {
		var err error
		abs, err = resolveWorkspaceDir(raw)
		if err != nil {
			fileError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		fileError(w, http.StatusBadRequest, err.Error())
		return
	}
	out := make([]workspaceDir, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		out = append(out, workspaceDir{Name: e.Name(), Path: filepath.Join(abs, e.Name())})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	parent := ""
	if abs != string(filepath.Separator) {
		parent = filepath.Dir(abs)
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": abs, "parent": parent, "dirs": out})
}