// The workspace picker/finder face's self-check (moved from
// agent/api/server_workspaces_test.go + server_workspace_intent_test.go's
// listing test by the shell-ring shrink wave 3, card t_b75df168): pin
// the wire contract directly against the capsule's Routes - the base
// package no longer serves these (they are module routes).
//
// /workspaces/browse lists child dirs of a validated absolute path
// (hidden entries filtered, root default on an empty path, the parent
// reported); /workspaces/files lists first-level entries (hidden
// filtered) and refuses relative, nonexistent and file paths.

package files

import (
	"encoding/json"
	"net/http"
	httptest2 "net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

func workspaceTestRoutes(t *testing.T) *Routes {
	t.Helper()
	return newRoutes(&core.Context{})
}

func jsonMarshalWorkspace(v string) []byte {
	b, _ := json.Marshal(v)
	return b
}

func mustQueryWorkspace(v string) string {
	p := filepath.ToSlash(filepath.Clean(v))
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(p); i++ {
		c := p[i]
		switch {
		case c == '/':
			b.WriteByte('/')
		case ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z') || ('0' <= c && c <= '9'):
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&15])
		}
	}
	return b.String()
}

// TestWorkspaceBrowse: the picker's read - the root listing (empty
// path), the parent chain, hidden filtered, relative refused.
func TestWorkspaceBrowse(t *testing.T) {
	rt := workspaceTestRoutes(t)

	// The root listing lists the root dirs and reports no parent. On
	// windows the "/" default must be mounted on the process volume -
	// a volume-less root's rows ("\Users") never re-validate on click
	// (t_7dcdc98e): every emitted row path must be absolute.
	req := httptest2.NewRequest(http.MethodGet, "/workspaces/browse", nil)
	rec := httptest2.NewRecorder()
	rt.handleWorkspacesBrowse(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("browse root: status = %d", rec.Code)
	}
	var listing struct {
		Path   string
		Parent string
		Dirs   []struct {
			Name, Path string
		}
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listing); err != nil {
		t.Fatalf("decode browse: %v", err)
	}
	if runtime.GOOS == "windows" {
		if vol := filepath.VolumeName(listing.Path); vol == "" {
			t.Errorf("browse root path = %q, want a volume-qualified root", listing.Path)
		}
	} else if listing.Path != "/" && listing.Path != "" {
		t.Errorf("browse root path = %q, want \"/\"", listing.Path)
	}
	if listing.Parent != "" {
		t.Errorf("browse root parent = %q, want empty", listing.Parent)
	}
	if len(listing.Dirs) == 0 {
		t.Fatal("root listing is empty")
	}
	for _, d := range listing.Dirs {
		if !filepath.IsAbs(d.Path) {
			t.Errorf("root row %q is not absolute - the browse click would be refused", d.Path)
		}
	}
	// The click-through wire shape: every root row path re-validates
	// in the same endpoint that emitted it. A row may still 400 on a
	// permission-bound dir (open ... Access is denied) - a real disk
	// error, browsable by a user with the rights - but the
	// "not an absolute path" refusal can no longer happen (t_7dcdc98e).
	for _, d := range listing.Dirs {
		req := httptest2.NewRequest(http.MethodGet, "/workspaces/browse?path="+mustQueryWorkspace(d.Path), nil)
		rec := httptest2.NewRecorder()
		rt.handleWorkspacesBrowse(rec, req)
		if rec.Code == http.StatusOK {
			continue
		}
		if strings.Contains(rec.Body.String(), "absolute path") {
			t.Errorf("root row %q was refused as non-absolute: %s - the dead-click regression, t_7dcdc98e", d.Path, rec.Body.String())
		}
	}

	// A relative path is rejected; an absolute dir lists its subdirs.
	req = httptest2.NewRequest(http.MethodGet, "/workspaces/browse?path=rel/ative", nil)
	rec = httptest2.NewRecorder()
	rt.handleWorkspacesBrowse(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("relative browse: status = %d, want 400", rec.Code)
	}

	// An absolute dir lists its visible subdirs and hides dot dirs.
	dir := t.TempDir()
	sub := filepath.Join(dir, "proj")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	dot := filepath.Join(dir, ".idea")
	if err := os.Mkdir(dot, 0o755); err != nil {
		t.Fatal(err)
	}
	req = httptest2.NewRequest(http.MethodGet, "/workspaces/browse?path="+mustQueryWorkspace(dir), nil)
	rec = httptest2.NewRecorder()
	rt.handleWorkspacesBrowse(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("browse dir: status = %d (%s)", rec.Code, rec.Body.String())
	}
	listing = struct {
		Path   string
		Parent string
		Dirs   []struct {
			Name, Path string
		}
	}{}
	if err := json.Unmarshal(rec.Body.Bytes(), &listing); err != nil {
		t.Fatalf("decode dir listing: %v", err)
	}
	if len(listing.Dirs) != 1 || listing.Dirs[0].Name != "proj" {
		t.Fatalf("dirs = %+v, want only proj", listing.Dirs)
	}
	if listing.Parent == "" {
		t.Errorf("non-root listing must report a parent")
	}

	// A nonexistent dir is a 400.
	req = httptest2.NewRequest(http.MethodGet, "/workspaces/browse?path="+string(jsonMarshalWorkspace(filepath.Join(dir, "nope"))), nil)
	rec = httptest2.NewRecorder()
	rt.handleWorkspacesBrowse(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing dir: status = %d, want 400", rec.Code)
	}
}

// TestWorkspaceFilesListing: /workspaces/files lists first-level
// entries of a validated absolute dir (hidden filtered), refusing
// relative and nonexistent paths - the @ picker's draft-intent read.
func TestWorkspaceFilesListing(t *testing.T) {
	rt := workspaceTestRoutes(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".hidden"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed hidden: %v", err)
	}

	req := httptest2.NewRequest(http.MethodGet, "/workspaces/files?path="+mustQueryWorkspace(dir), nil)
	rec := httptest2.NewRecorder()
	rt.handleWorkspaceFiles(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: status = %d (%s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Path string
		Entries []struct {
			Name  string `json:"name"`
			IsDir bool   `json:"is_dir"`
		}
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if filepath.Clean(body.Path) != filepath.Clean(dir) {
		t.Errorf("path = %q, want %q", body.Path, dir)
	}
	names := map[string]bool{}
	for _, e := range body.Entries {
		names[e.Name] = e.IsDir
	}
	if names["note.txt"] {
		t.Errorf("note.txt flagged as dir")
	}
	if _, ok := names["note.txt"]; !ok {
		t.Errorf("note.txt missing from the listing")
	}
	if _, ok := names[".hidden"]; ok {
		t.Errorf(".hidden must stay filtered")
	}

	// Refusals: relative, nonexistent, a file not a dir.
	for _, bad := range []string{"relative/path", "/definitely/not/here", filepath.Join(dir, "note.txt")} {
		req = httptest2.NewRequest(http.MethodGet, "/workspaces/files?path="+mustQueryWorkspace(bad), nil)
		rec = httptest2.NewRecorder()
		rt.handleWorkspaceFiles(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("bad path %q: status = %d, want 400", bad, rec.Code)
		}
	}
}

// TestWorkspaceMethodGuard: the picker face is read-only GET; a POST is
// a 405, never a browse.
func TestWorkspaceMethodGuard(t *testing.T) {
	rt := workspaceTestRoutes(t)
	for _, probe := range []struct {
		path   string
		handle func(http.ResponseWriter, *http.Request)
	}{
		{"/workspaces/browse", rt.handleWorkspacesBrowse},
		{"/workspaces/files", rt.handleWorkspaceFiles},
	} {
		req := httptest2.NewRequest(http.MethodPost, probe.path, strings.NewReader("{}"))
		rec := httptest2.NewRecorder()
		probe.handle(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("POST %s: status = %d, want 405", probe.path, rec.Code)
		}
	}
}
