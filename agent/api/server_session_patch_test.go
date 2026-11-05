package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// TestSessionContextPatch pins the session-context PATCH surface (card
// t_9de5b009): model + workspace land on the row, the list payload
// serves the pinned model, invalid workspaces are rejected before any
// write, and GET /sessions/{id} restores both fields. The file used to
// be the workspaces surface's whole test (both GET routes served from
// agent/api); the picker routes rode the files capsule out with wave 3
// (card t_b75df168, harness/files/workspaces_test.go) and only the
// session-row PATCH face is left here - the file renamed with it.
// The workspace-path validator itself is core.ResolveWorkspaceDir now
// (the shared guard both consumers ride).
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

	// GET /sessions/{id} carries the pinned fields (the list payload rode
	// the store capsule out with wave 2; the row truth is the store).
	req = httptest.NewRequest(http.MethodGet, "/sessions/cx1", nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	var detail struct {
		Session core.Session
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if detail.Session.Model != "glm-5.3" {
		t.Errorf("row model = %q, want \"glm-5.3\"", detail.Session.Model)
	}
	if filepath.Clean(detail.Session.Workspace) != filepath.Clean(ws) {
		t.Errorf("row workspace = %q, want %q", detail.Session.Workspace, ws)
	}

	// GET /sessions/{id} restores both fields (reopen semantics).
	req = httptest.NewRequest(http.MethodGet, "/sessions/cx1", nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	var restored struct {
		Session core.Session
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &restored); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if restored.Session.Model != "glm-5.3" || filepath.Clean(restored.Session.Workspace) != filepath.Clean(ws) {
		t.Fatalf("detail restore: model %q workspace %q", restored.Session.Model, restored.Session.Workspace)
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
