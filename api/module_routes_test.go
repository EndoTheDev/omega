package api

import (
	"net/http"
	httptest2 "net/http/httptest"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// The module-rest dispatcher (v0.12 phase F): the catch-all serves a
// mounted module's routes through the capsule mux - a pattern miss on
// a module-rooted path answers 404 (the capsule mux has no fallback),
// a non-module path serves the SPA shell, a method mismatch is the
// mux's 405. The shell never imports the module: the route rides the
// live Context.
func TestModuleRouterServesAndFallsThrough(t *testing.T) {
	s := NewServer(nil, nil, nil, nil, nil, nil)
	hits := 0
	ctx := &core.Context{ModuleRoutes: []core.ModuleRoute{
		{Module: "probe", Pattern: "GET /tasks/only", Handler: func(w http.ResponseWriter, r *http.Request) {
			hits++
			w.WriteHeader(http.StatusOK)
		}},
	}}
	if err := s.rewireSeams(ctx); err != nil {
		t.Fatalf("rewire: %v", err)
	}
	h := s.Handler()

	rec := httptest2.NewRecorder()
	h.ServeHTTP(rec, httptest2.NewRequest(http.MethodGet, "/tasks/only", nil))
	if rec.Code != http.StatusOK || hits != 1 {
		t.Fatalf("registered route = %d hits=%d", rec.Code, hits)
	}

	// module-rooted miss: 404, never the SPA shell - the capsule mux
	// holds only its own patterns, no fallback
	rec = httptest2.NewRecorder()
	h.ServeHTTP(rec, httptest2.NewRequest(http.MethodGet, "/tasks/missing", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("module-rooted miss = %d", rec.Code)
	}

	// method mismatch on a method-scoped registration: the mux's 405
	rec = httptest2.NewRecorder()
	h.ServeHTTP(rec, httptest2.NewRequest(http.MethodPost, "/tasks/only", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("method mismatch = %d", rec.Code)
	}

	// a non-module path: the SPA shell (untouched)
	rec = httptest2.NewRecorder()
	h.ServeHTTP(rec, httptest2.NewRequest(http.MethodGet, "/unknown-browser-path", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("spa path = %d", rec.Code)
	}

	// all routes gone: the catch-all never consults a module mux
	if err := s.rewireSeams(&core.Context{}); err != nil {
		t.Fatalf("rewire empty: %v", err)
	}
	rec = httptest2.NewRecorder()
	h.ServeHTTP(rec, httptest2.NewRequest(http.MethodGet, "/tasks/t_1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("dismounted route = %d", rec.Code)
	}
}

// The 50-sessions-reload nightly red (2026-11-05, master 903a6cee): the
// base mux's /sessions/{id} wildcard out-specifies the store capsule's
// literal GET /sessions/search living behind the "/" catch-all - ServeMux
// dispatch never sees the module mux - so the search seat died into the
// single-session detail handler (id="search") and the sidebar's search
// never narrowed. The rescue layer exact-matches the module literals
// first; this pin holds the tripwire: a capsule literal wins over a base
// wildcard that happens to cover it, and the wildcard still owns paths
// the capsule never claimed.
func TestModuleLiteralRescuedFromBaseWildcard(t *testing.T) {
	s := NewServer(nil, nil, nil, nil, nil, nil)
	ctx := &core.Context{ModuleRoutes: []core.ModuleRoute{
		{Module: "store", Pattern: "GET /sessions/search", Handler: func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[]`))
		}},
	}}
	if err := s.rewireSeams(ctx); err != nil {
		t.Fatalf("rewire: %v", err)
	}
	h := s.Handler()

	// the search seat: the capsule's literal, not the base detail
	// handler (which without the fix answers 501 "session store not
	// configured" at this nil-store constructor).
	rec := httptest2.NewRecorder()
	h.ServeHTTP(rec, httptest2.NewRequest(http.MethodGet, "/sessions/search?q=fibo", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "[]" {
		t.Fatalf("search seat = %d body=%q, want the capsule 200 []", rec.Code, rec.Body.String())
	}

	// the detail wildcard keeps unclaimed paths - a real session id
	// still reaches /sessions/{id} (501 here: no store on the base).
	rec = httptest2.NewRecorder()
	h.ServeHTTP(rec, httptest2.NewRequest(http.MethodGet, "/sessions/jrn-1", nil))
	if rec.Code != http.StatusNotImplemented && rec.Code != http.StatusNotFound {
		t.Fatalf("detail wildcard = %d, want the base 501/404 lane", rec.Code)
	}

	// a method the capsule never registered: the rescue is
	// method-scoped, the request rides the base mux as always (501 at
	// this nil-store constructor via /sessions/{id}) - never the
	// capsule's GET handler.
	rec = httptest2.NewRecorder()
	h.ServeHTTP(rec, httptest2.NewRequest(http.MethodPost, "/sessions/search", nil))
	if rec.Code == http.StatusOK {
		t.Fatalf("method miss served the capsule literal: %d", rec.Code)
	}

	// live toggle off: the rescue lane dies with the routes - the
	// wildcard owns the path again.
	if err := s.rewireSeams(&core.Context{}); err != nil {
		t.Fatalf("rewire empty: %v", err)
	}
	rec = httptest2.NewRecorder()
	h.ServeHTTP(rec, httptest2.NewRequest(http.MethodGet, "/sessions/search?q=fibo", nil))
	if rec.Code == http.StatusOK && rec.Body.String() == "[]" {
		t.Fatalf("dismounted literal still served: 200 []")
	}
}
