package api

// POST-time rejection of client-chosen `eph-` session ids.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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

// The roster capsule's guard.
