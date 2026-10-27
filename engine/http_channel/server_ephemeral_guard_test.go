package http_channel

// POST-time rejection of client-chosen `eph-` session ids (card
// t_860033e3, finding 4): the `eph-` namespace is minted by the /chat
// ephemeral lane and lives in memory only. A durable row under one of
// those ids would be unreachable - misrouted - on every store-read
// endpoint. Both creation endpoints refuse the prefix with 400.

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/engine/agent"
)

func TestCreateSessionRejectsEphemeralPrefix(t *testing.T) {
	s, st := newTestServerWithStore(t)
	h := s.Handler()

	req := httptest.NewRequest(http.MethodPost, "/sessions", strings.NewReader(`{"id":"eph-sneaky"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %q", rec.Code, rec.Body.String())
	}
	if _, err := st.GetSession(t.Context(), "eph-sneaky"); err == nil {
		t.Fatal("eph-sneaky row exists in the store - the durable lane minted an eph- id")
	}
}

func TestAgentSessionsRejectEphemeralPrefix(t *testing.T) {
	s, st := newTestServerWithStore(t)
	t.Setenv("OMEGA_HOME", t.TempDir())
	if err := agent.ScaffoldAgent(os.Getenv("OMEGA_HOME"), "scout", "probe duty"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/agents/{name}/sessions", s.handleAgentSessions)

	req := httptest.NewRequest(http.MethodPost, "/agents/scout/sessions", strings.NewReader(`{"id":"eph-sneaky"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %q", rec.Code, rec.Body.String())
	}
	if _, err := st.GetSession(t.Context(), "eph-sneaky"); err == nil {
		t.Fatal("eph-sneaky row exists in the store - the durable lane minted an eph- id")
	}
}
