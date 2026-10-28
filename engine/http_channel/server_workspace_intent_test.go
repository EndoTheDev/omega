package http_channel

// The draft workspace intent (card t_ea7e6a58): a session is born
// pinned when the create carries a folder - POST /sessions {workspace}
// and the BARE POST /chat auto-create. Refusal paths answer loudly and
// create nothing (never a half-pinned row); a create without an intent
// keeps the pre-card behavior.

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	httptest2 "net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/modules/store"
)

func sessionRowWorkspace(t *testing.T, s *Server, id string) (found bool, workspace string) {
	t.Helper()
	req := httptest2.NewRequest(http.MethodGet, "/sessions/"+id, nil)
	rec := httptest2.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code == http.StatusNotFound {
		return false, ""
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("get %s: status = %d (%s)", id, rec.Code, rec.Body.String())
	}
	var detail struct {
		Session sessionPayload
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	return true, detail.Session.Workspace
}

// TestSessionCreateWorkspace: POST /sessions {workspace} creates the
// row already pinned; a bad folder is refused BEFORE any row write;
// a create without a workspace stays unpinned.
func TestSessionCreateWorkspace(t *testing.T) {
	s, _ := newTestServerWithStore(t)
	ws := t.TempDir()

	// Born pinned.
	req := httptest2.NewRequest(http.MethodPost, "/sessions",
		strings.NewReader(`{"id":"b1","workspace":`+string(jsonMarshal(ws))+`}`))
	rec := httptest2.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create with workspace: status = %d (%s), want 201", rec.Code, rec.Body.String())
	}
	found, got := sessionRowWorkspace(t, s, "b1")
	if !found || filepath.Clean(got) != filepath.Clean(ws) {
		t.Fatalf("born pin: found=%v workspace=%q, want %q", found, got, ws)
	}

	// A nonexistent dir is refused loudly, nothing created.
	req = httptest2.NewRequest(http.MethodPost, "/sessions",
		strings.NewReader(`{"id":"b2","workspace":"/definitely/not/here"}`))
	rec = httptest2.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad folder: status = %d (%s), want 400", rec.Code, rec.Body.String())
	}
	if found, _ := sessionRowWorkspace(t, s, "b2"); found {
		t.Fatal("b2 exists - the refusal must not create a row")
	}

	// A relative path is refused the same way.
	req = httptest2.NewRequest(http.MethodPost, "/sessions",
		strings.NewReader(`{"id":"b3","workspace":"relative/dir"}`))
	rec = httptest2.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("relative folder: status = %d, want 400", rec.Code)
	}
	if found, _ := sessionRowWorkspace(t, s, "b3"); found {
		t.Fatal("b3 exists - the refusal must not create a row")
	}

	// No workspace in the payload: today's behavior, unpinned row.
	req = httptest2.NewRequest(http.MethodPost, "/sessions", strings.NewReader(`{"id":"b4"}`))
	rec = httptest2.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("plain create: status = %d, want 201", rec.Code)
	}
	found, got = sessionRowWorkspace(t, s, "b4")
	if !found || got != "" {
		t.Fatalf("plain create: found=%v workspace=%q, want empty", found, got)
	}
}

// TestChatCreateWorkspace: the BARE /chat create pins the draft intent
// folder; every other shape (session id given, ephemeral, retry) plus
// a workspace is refused before anything runs.
func TestChatCreateWorkspace(t *testing.T) {
	s, st := newTestServerWithStore(t)
	s.chat.batch = 0
	ws := t.TempDir()

	ts := httptest2.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	// Bare create with the intent: the run streams, the row is born
	// pinned. prepareRun reads the row's root for THIS run.
	res, err := http.Post(ts.URL+"/chat", "application/json",
		strings.NewReader(`{"messages":[{"role":"user","content":"say hi"}],"workspace":`+
			string(jsonMarshal(ws))+`}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	rd := bufio.NewReader(res.Body)
	for {
		frame, ok := nextFrameW(t, rd)
		if !ok {
			t.Fatal("stream ended without agent_end")
		}
		if frame.event == "agent_end" {
			break
		}
	}
	res.Body.Close()

	found, got := sessionRowWorkspace(t, s, firstSessionID(t, st))
	if !found || filepath.Clean(got) != filepath.Clean(ws) {
		t.Fatalf("bare create pin: found=%v workspace=%q, want %q", found, got, ws)
	}

	// A session-bound send never pins (the server refuses loudly).
	body := `{"session_id":"` + firstSessionID(t, st) + `","messages":[{"role":"user","content":"x"}],"workspace":"/tmp/ws"}`
	req := httptest2.NewRequest(http.MethodPost, "/chat", strings.NewReader(body))
	rec := httptest2.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("session + workspace: status = %d (%s), want 400", rec.Code, rec.Body.String())
	}

	// An ephemeral mint plus a workspace is refused (no row to pin).
	req = httptest2.NewRequest(http.MethodPost, "/chat",
		strings.NewReader(`{"ephemeral":true,"messages":[{"role":"user","content":"m"}],"workspace":"/tmp/ws"}`))
	rec = httptest2.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("ephemeral + workspace: status = %d, want 400", rec.Code)
	}

	// A bad folder on the bare-create path: refused BEFORE the create -
	// no row, no run, loud 400.
	rows, err := st.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	before := len(rows)
	res2, err := http.Post(ts.URL+"/chat", "application/json",
		strings.NewReader(`{"messages":[{"role":"user","content":"m"}],"workspace":"/definitely/not/here"}`))
	if err != nil {
		t.Fatalf("post bad: %v", err)
	}
	res2.Body.Close()
	if res2.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad folder create: status = %d, want 400", res2.StatusCode)
	}
	rows2, err := st.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("list sessions after: %v", err)
	}
	if after := len(rows2); after != before {
		t.Fatalf("bad folder created %d rows, want %d", after, before)
	}
}

// TestWorkspaceFilesListing: /workspaces/files lists first-level
// entries of a validated absolute dir (hidden filtered), refusing
// relative and nonexistent paths - the @ picker's draft-intent read.
func TestWorkspaceFilesListing(t *testing.T) {
	s, _ := newTestServerWithStore(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".hidden"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed hidden: %v", err)
	}

	req := httptest2.NewRequest(http.MethodGet, "/workspaces/files?path="+mustQuery(dir), nil)
	rec := httptest2.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: status = %d (%s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Path    string
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
		req = httptest2.NewRequest(http.MethodGet, "/workspaces/files?path="+mustQuery(bad), nil)
		rec = httptest2.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("bad path %q: status = %d, want 400", bad, rec.Code)
		}
	}
}

// firstSessionID returns the store's only session id (the /chat run
// creates exactly one).
func firstSessionID(t *testing.T, st *store.Store) string {
	t.Helper()
	rows, err := st.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("sessions = %d, want 1", len(rows))
	}
	return rows[0].ID
}