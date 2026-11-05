// The session workspace surface, riding the file domain capsule
// (shell-ring shrink wave 3, card t_b75df168: a filesystem listing face
// is the files domain's work - the workspace PICKER is the session's
// row truth (the store's Workspace column); these routes only WALK
// directories, unbuzzed from the explorer's rooted cwd). The picker
// reads the local filesystem to pick a folder; strictly read-only GET -
// no write, delete or exec path exists here. The browse walks REAL
// directories (an absolute path selects any folder on the box - a
// session workspace is not contained by the process working directory,
// unlike the /files inspector, which stays rooted there).
//
// Safety rules, every request:
//   - absolute paths only (no browser-relative confusion), validated
//     by core.ResolveWorkspaceDir (the shared guard the session
//     create/PATCH paths use - one validator, two consumers)
//   - "."-prefixed segments and hidden entries are rejected/filtered
//   - the path is symlink-resolved before the disk is touched
//   - only directory listings are served, never file contents
package files

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/EndoTheDev/omega/core"
)

// workspaceDir is one /workspaces/browse row.
type workspaceDir struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// handleWorkspacesBrowse serves GET /workspaces/browse?path=<abs>,
// listing one directory's child folders. Empty or missing path lists
// "/". Hidden entries (and every dot-prefixed name) are filtered, and
// dot segments are rejected in the query path - no browsing into hidden
// trees. The response carries the entries plus the parent path (""
// when at the root - the UI hides the climb there).
func (rt *Routes) handleWorkspacesBrowse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	raw := r.URL.Query().Get("path")
	abs := "/"
	if raw != "" {
		var err error
		abs, err = core.ResolveWorkspaceDir(raw)
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
	// "/" is the documented root default on every platform; on windows
	// it never equals filepath.Separator, so both spellings are roots.
	if root := string(filepath.Separator); abs != root && abs != "/" {
		parent = filepath.Dir(abs)
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": abs, "parent": parent, "dirs": out})
}

// workspaceFile is one /workspaces/files row: the name and whether it
// is a directory (the @ picker filters to files).
type workspaceFile struct {
	Name  string `json:"name"`
	IsDir bool   `json:"is_dir"`
}

// handleWorkspaceFiles serves GET /workspaces/files?path=<abs>, listing
// one validated directory's first-level entries. It backs the @ mention
// picker when a workspace intent (or a pinned session workspace) is
// set: the token the picker inserts expands SERVER-side against the
// session workspace at run time, so the listing must read THAT folder,
// not the process working directory /files/browse is jailed to (card
// t_ea7e6a58). Read-only, one level deep, hidden entries filtered, the
// same absolute-path + symlink rules as the browse endpoint.
func (rt *Routes) handleWorkspaceFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	raw := r.URL.Query().Get("path")
	abs, err := core.ResolveWorkspaceDir(raw)
	if err != nil {
		fileError(w, http.StatusBadRequest, err.Error())
		return
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		fileError(w, http.StatusBadRequest, err.Error())
		return
	}
	out := make([]workspaceFile, 0, len(entries))
	for _, e := range entries {
		// hidden entries stay invisible (same rule as the browse walk)
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		out = append(out, workspaceFile{Name: e.Name(), IsDir: e.IsDir()})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	// a runaway folder never floods the DOM - the picker slices its
	// rows further; the cap only bounds this endpoint's payload
	if len(out) > 500 {
		out = out[:500]
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": abs, "entries": out})
}
