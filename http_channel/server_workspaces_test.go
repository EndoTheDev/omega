package http_channel

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// TestSessionContextPatch pins the session-context PATCH surface (card
// t_9de5b009): model + workspace land on the row, the list payload
// serves the pinned model, invalid workspaces are rejected before any
// write, and GET /sessions/{id} restores both fields.
func TestSessionContextPatch(t *testing.T) {
	s, _ := newTestServerWithStore(t)

	// Create a session row.
	req := httptest.NewRequest(http.MethodPost, "/sessions", strings.NewReader(`{"id":"cx1"}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create session: status = %d, want 201", rec.Code)
	}

	// PATCH the pinned model and a real directory as workspace.
	ws := t.TempDir()
	body := `{"model":"glm-5.3","workspace":` + string(jsonMarshal(ws)) + `}`
	req = httptest.NewRequest(http.MethodPatch, "/sessions/cx1", strings.NewReader(body))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: status = %d (%s), want 200", rec.Code, rec.Body.String())
	}

	// GET /sessions carries the pinned model in the list payload.
	req = httptest.NewRequest(http.MethodGet, "/sessions", nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	var rows []sessionPayload
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode sessions: %v", err)
	}
	found := false
	for _, r := range rows {
		if r.ID == "cx1" {
			found = true
			if r.Model != "glm-5.3" {
				t.Errorf("list model = %q, want \"glm-5.3\"", r.Model)
			}
			if filepath.Clean(r.Workspace) != filepath.Clean(ws) {
				t.Errorf("list workspace = %q, want %q", r.Workspace, ws)
			}
		}
	}
	if !found {
		t.Fatalf("session cx1 missing from the list")
	}

	// GET /sessions/{id} restores both fields (reopen semantics).
	req = httptest.NewRequest(http.MethodGet, "/sessions/cx1", nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	var detail struct {
		Session sessionPayload
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if detail.Session.Model != "glm-5.3" || filepath.Clean(detail.Session.Workspace) != filepath.Clean(ws) {
		t.Fatalf("detail restore: model %q workspace %q", detail.Session.Model, detail.Session.Workspace)
	}

	// A nonexistent workspace dir is rejected BEFORE any write.
	req = httptest.NewRequest(http.MethodPatch, "/sessions/cx1", strings.NewReader(`{"workspace":"/definitely/not/here"}`))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad workspace: status = %d, want 400", rec.Code)
	}

	// Clearing: empty strings overwrite both fields.
	req = httptest.NewRequest(http.MethodPatch, "/sessions/cx1", strings.NewReader(`{"model":"","workspace":""}`))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear patch: status = %d, want 200", rec.Code)
	}

	// The workspaces browse endpoint lists dirs and reports a parent.
	req = httptest.NewRequest(http.MethodGet, "/workspaces/browse", nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("browse root: status = %d", rec.Code)
	}
	var listing struct {
		Path   string
		Parent string
		Dirs   []struct{ Name, Path string }
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listing); err != nil {
		t.Fatalf("decode browse: %v", err)
	}
	if listing.Path != "/" && listing.Path != "" {
		t.Errorf("browse root path = %q, want \"/\"", listing.Path)
	}
	if listing.Parent != "" {
		t.Errorf("browse root parent = %q, want empty", listing.Parent)
	}

	// A relative path is rejected; an absolute dir lists its subdirs.
	req = httptest.NewRequest(http.MethodGet, "/workspaces/browse?path=rel/ative", nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("relative browse: status = %d, want 400", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/workspaces/browse?path="+mustQuery(ws), nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("browse dir: status = %d", rec.Code)
	}
}

func jsonMarshal(v string) []byte {
	b, _ := json.Marshal(v)
	return b
}

func mustQuery(v string) string {
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