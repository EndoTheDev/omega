package http_channel

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
// mux's 405. The engine never imports the module: the route rides the
// live Context.
func TestModuleRouterServesAndFallsThrough(t *testing.T) {
	s := NewServer(nil, nil, nil, nil, nil)
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
