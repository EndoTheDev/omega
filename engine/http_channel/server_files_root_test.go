package http_channel

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The root resolution law (card t_a5f9d185): the file surface roots on
// (1) the focused session's pinned workspace, (2) the configured
// default_workspace, (3) the process cwd. Every test here drives the
// HTTP handlers, never the resolver alone - the law lives on the wire.

// filesRootServer wires a store-backed server whose cwd is `cwdDir` via
// chdirToWorkdir; the returned setWorkspace pins the default_workspace
// knob the hot seam sets.
func filesRootServer(t *testing.T) (*Server, string, func(string)) {
	t.Helper()
	cwd := chdirToWorkdir(t)
	s, _ := newTestServerWithStore(t)
	return s, cwd, func(ws string) { s.SetDefaultWorkspace(ws) }
}

// createWorkspaceSession mints one real store session whose Workspace
// column pins `dir` - the law's step 1.
func createWorkspaceSession(t *testing.T, s *Server, dir string) string {
	t.Helper()
	st, ok := s.store.(interface {
		CreateSession(context.Context, string, string, string) error
		SetSessionWorkspace(context.Context, string, string) error
	})
	if !ok {
		t.Fatalf("store does not expose the workspace seam")
	}
	id := "sess-root-law"
	ctx := context.Background()
	if err := st.CreateSession(ctx, id, "", "root law"); err != nil {
		if !strings.Contains(err.Error(), "exists") && !strings.Contains(err.Error(), "unique") {
			t.Fatalf("create session: %v", err)
		}
	}
	if err := st.SetSessionWorkspace(ctx, id, dir); err != nil {
		t.Fatalf("set session workspace: %v", err)
	}
	return id
}

// browseRoot GETs the root listing and returns the resolved root +
// source the response reports.
func browseRoot(t *testing.T, s *Server, url string) (int, string, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	res := rec.Result()
	var body map[string]any
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	root, _ := body["root"].(string)
	source, _ := body["source"].(string)
	return res.StatusCode, root, source
}

func TestFilesRootResolutionChain(t *testing.T) {
	s, cwd, setDefault := filesRootServer(t)

	// No session, no default: the cwd fallback stands, source=cwd.
	status, root, source := browseRoot(t, s, "/files/browse")
	if status != http.StatusOK || root != cwd || source != filesSourceCwd {
		t.Fatalf("bare browse: status=%d root=%q source=%q; want 200 %q %q",
			status, root, source, cwd, filesSourceCwd)
	}

	// default_workspace set, still no session: the knob wins.
	ws := t.TempDir()
	os.MkdirAll(filepath.Join(ws, "marker"), 0o755) //nolint:errcheck // setup
	setDefault(ws)
	_, root, source = browseRoot(t, s, "/files/browse")
	if source != filesSourceDefault || root != ws {
		t.Fatalf("default-only browse: root=%q source=%q; want %q %q", root, source, ws, filesSourceDefault)
	}

	// A session with a pinned workspace: step 1 outranks the knob -
	// even when the pin points somewhere else entirely.
	sessDir := t.TempDir()
	id := createWorkspaceSession(t, s, sessDir)
	_, root, source = browseRoot(t, s, fmt.Sprintf("/files/browse?session=%s", id))
	if source != filesSourceSession || root != sessDir {
		t.Fatalf("session browse: root=%q source=%q; want %q %q", root, source, sessDir, filesSourceSession)
	}
}

func TestFilesRootSessionOutranksDefault(t *testing.T) {
	// Direct check of the same ordering the chain test walks, at the
	// sub-listing level: the session workspace's marker dir lists, the
	// default workspace's never does.
	s, _, setDefault := filesRootServer(t)
	ws := t.TempDir()
	sessDir := t.TempDir()
	os.MkdirAll(filepath.Join(sessDir, "sess-marker"), 0o755) //nolint:errcheck // setup
	os.MkdirAll(filepath.Join(ws, "default-marker"), 0o755)   //nolint:errcheck // setup
	setDefault(ws)
	id := createWorkspaceSession(t, s, sessDir)

	req := httptest.NewRequest(http.MethodGet, "/files/browse?session="+id, nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	var body struct {
		Entries []fileEntry `json:"entries"`
		Root    string      `json:"root"`
		Source  string      `json:"source"`
	}
	if err := json.NewDecoder(rec.Result().Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Source != filesSourceSession || body.Root != sessDir {
		t.Fatalf("root=%q source=%q; want %q %q", body.Root, body.Source, sessDir, filesSourceSession)
	}
	found := false
	for _, e := range body.Entries {
		if e.Name == "sess-marker" {
			found = true
		}
		if e.Name == "default-marker" {
			t.Fatalf("default workspace's entries leaked into a session-rooted listing")
		}
	}
	if !found {
		t.Fatalf("session workspace's listing missing its marker: %+v", body.Entries)
	}
}

func TestFilesRootUnknownSessionFallsToDefault(t *testing.T) {
	// The law: an unknown session id falls through (no 404 on browse) -
	// a draft tab or a lost id must not break the files surface.
	s, _, setDefault := filesRootServer(t)
	setDefault(t.TempDir())
	status, _, source := browseRoot(t, s, "/files/browse?session=eph-nope")
	if status != http.StatusOK || source != filesSourceDefault {
		t.Fatalf("unknown session: status=%d source=%q; want 200 %q", status, source, filesSourceDefault)
	}
	// ...and with no default either, all the way down to cwd.
	s2, cwd2, _ := filesRootServer(t)
	status, root, source := browseRoot(t, s2, "/files/browse?session=eph-nope")
	if status != http.StatusOK || root != cwd2 || source != filesSourceCwd {
		t.Fatalf("unknown session, no default: status=%d root=%q source=%q; want 200 %q %q",
			status, root, source, cwd2, filesSourceCwd)
	}
}

func TestFilesConfinementUnderSessionRoot(t *testing.T) {
	// TRUST BOUNDARY: with the tree rooted on a session workspace AWAY
	// from the process cwd, a relative path must still be jailed to the
	// RESOLVED root - browse inside works, traversal and writes through
	// symlinks out die on the same rules as ever.
	s, cwd, _ := filesRootServer(t)
	sessDir := t.TempDir()
	id := createWorkspaceSession(t, s, sessDir)
	os.WriteFile(filepath.Join(sessDir, "notes.txt"), []byte("hello"), 0o644) //nolint:errcheck // setup

	// read inside the resolved root works
	req := httptest.NewRequest(http.MethodGet, "/files/read?path=notes.txt&session="+id, nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Result().StatusCode != http.StatusOK {
		t.Fatalf("read inside session root: status=%d", rec.Result().StatusCode)
	}

	// the SAME relative path does NOT resolve against cwd - the file
	// only lives in the session workspace
	cwdFile := filepath.Join(cwd, "only-in-cwd.txt")
	os.WriteFile(cwdFile, []byte("x"), 0o644) //nolint:errcheck // setup
	req = httptest.NewRequest(http.MethodGet, "/files/read?path=only-in-cwd.txt&session="+id, nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Result().StatusCode != http.StatusNotFound {
		t.Fatalf("read of a cwd file through a session-rooted request: status=%d; want 404",
			rec.Result().StatusCode)
	}

	// write into the session root lands THERE, never in cwd
	sessionParam := id
	req = httptest.NewRequest(http.MethodPost, "/files/write",
		strings.NewReader(fmt.Sprintf(`{"path":"made.txt","content":"y","session":%q}`, sessionParam)))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Result().StatusCode != http.StatusOK {
		t.Fatalf("write under session root: status=%d", rec.Result().StatusCode)
	}
	if _, err := os.Stat(filepath.Join(sessDir, "made.txt")); err != nil {
		t.Fatalf("wrote file missing from the session workspace: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cwd, "made.txt")); err == nil {
		t.Fatalf("the write landed in the process cwd - root confinement broken")
	}

	// traversal through the session root stays refused
	req = httptest.NewRequest(http.MethodGet, "/files/browse?path=..&session="+id, nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("traversal under session root: status=%d; want 400", rec.Result().StatusCode)
	}
}
