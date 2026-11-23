package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// sessionContextOf drives GET /sessions/{id}/context and decodes the answer.
func sessionContextOf(t *testing.T, s *Server, path string) (int, sessionContextInfo) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	var out sessionContextInfo
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: status = %d (body: %s)", path, rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("%s: decode: %v", path, err)
	}
	return rec.Code, out
}

// TestSessionContextProbe: the model-switch dialog's two numbers - the live context
// estimate of the session's active path and the TARGET slot's window, probed through the
// provider instance the slot names. A provider that cannot report a window floors at
// DefaultContextWindow, exactly like /models.
func TestSessionContextProbe(t *testing.T) {
	s, st, backup, _ := newInstanceTestServer(t)
	backup.contextWindow = 4096
	ctx := context.Background()
	if err := st.CreateSession(ctx, "ctx1", "", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}
	// 20000 chars at the estimate's 4 chars/token = 5000 tokens of live context.
	if err := st.AppendMessage(ctx, "ctx1", core.NewUser(strings.Repeat("x", 20000))); err != nil {
		t.Fatalf("append message: %v", err)
	}

	_, base := sessionContextOf(t, s, "/sessions/ctx1/context")
	if base.Tokens != 5000 {
		t.Fatalf("tokens = %d, want 5000 (the active-path estimate)", base.Tokens)
	}
	if base.ContextWindow != agent.DefaultContextWindow {
		t.Fatalf("window = %d, want the DefaultContextWindow floor for a silent provider",
			base.ContextWindow)
	}

	_, picked := sessionContextOf(t, s, "/sessions/ctx1/context?model=backup:backup-model")
	if picked.ContextWindow != 4096 {
		t.Fatalf("window = %d, want 4096 (the picked instance's own model)", picked.ContextWindow)
	}
	if picked.Model != "backup:backup-model" {
		t.Fatalf("model = %q, want the picked slot echoed", picked.Model)
	}
	if picked.Tokens != base.Tokens {
		t.Fatalf("tokens changed with the target model: %d -> %d", base.Tokens, picked.Tokens)
	}

	// No query: the session's own pin answers the window.
	if err := st.SetSessionModel(ctx, "ctx1", "backup:backup-model"); err != nil {
		t.Fatalf("pin model: %v", err)
	}
	_, pinned := sessionContextOf(t, s, "/sessions/ctx1/context")
	if pinned.ContextWindow != 4096 || pinned.Model != "backup:backup-model" {
		t.Fatalf("pinned probe = %+v, want the session's own slot", pinned)
	}

	// An unknown session is a 404, never a zeroed answer.
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sessions/nope/context", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown session status = %d, want 404", rec.Code)
	}

	// An unknown ephemeral id is a 404 too (expired or never minted).
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sessions/eph-missing/context", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown ephemeral status = %d, want 404", rec.Code)
	}
}

// TestSessionContextProbeMethodGuard: the probe is a read.
func TestSessionContextProbeMethodGuard(t *testing.T) {
	s, _, _, _ := newInstanceTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sessions/ctx1/context", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}
