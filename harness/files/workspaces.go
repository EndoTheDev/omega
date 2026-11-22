// The session workspace surface. The workspace PICKER is the session's row truth (the
// store's Workspace column); these routes only WALK directories, unbuzzed from the
// explorer's rooted cwd. The picker reads the local filesystem to pick a folder, strictly
// read-only: no write, delete or exec path exists here. The browse walks REAL directories
// (an absolute path selects any folder on the box, unlike /files, which stays rooted in
// the process working directory).
//
// Safety rules, every request:
//   - absolute paths only, validated by core.ResolveWorkspaceDir (the one validator the
//     session create/PATCH paths share)
//   - "."-prefixed segments and hidden entries are rejected or filtered; the path is
//     symlink-resolved before the disk is touched; only directory listings are served
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

// handleWorkspacesBrowse serves GET /workspaces/browse?path=<abs>.
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
	} else if filepath.VolumeName(abs) == "" {
		// The empty-path root default. Rows carry volume-qualified paths that re-validate on
		// click; a non-windows volume name is "", so linux behavior is byte-identical.
		if wd, err := os.Getwd(); err == nil {
			if vol := filepath.VolumeName(wd); vol != "" {
				abs = vol + string(filepath.Separator)
			}
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
	// "/" is the documented root default on every platform; on windows the process
	// volume's root ("C:\") is also a root, and the climb must stay hidden there too.
	if volRoot := filepath.VolumeName(abs) + string(filepath.Separator); abs != "/" && abs != volRoot {
		parent = filepath.Dir(abs)
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": abs, "parent": parent, "dirs": out})
}

// workspaceFile is one /workspaces/files row.
type workspaceFile struct {
	Name  string `json:"name"`
	IsDir bool   `json:"is_dir"`
}

// handleWorkspaceFiles serves GET /workspaces/files?path=<abs>, listing one validated
// folder. It backs the @ mention picker: the token the picker inserts expands SERVER-side
// against the session workspace at run time, so the listing must read THAT folder.
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
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		out = append(out, workspaceFile{Name: e.Name(), IsDir: e.IsDir()})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	// a runaway folder never floods the DOM.
	if len(out) > 500 {
		out = out[:500]
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": abs, "entries": out})
}
