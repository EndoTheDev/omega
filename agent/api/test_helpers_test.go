package api

// Inspector-surface helpers for the base-package tests.

import (
	"encoding/json"
	"net/http"
	httptest "net/http/httptest"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// offerServices offers the fake supplies into a fresh Context and rewires the server
// against it (part 3.
func offerServices(t *testing.T, s *Server, offers map[string]any) {
	t.Helper()
	ctx := &core.Context{}
	for k, v := range offers {
		ctx.ApplyOffer(k, "fake", v)
	}
	if err := s.RewireSeams(ctx, nil, nil); err != nil {
		t.Fatalf("rewire: %v", err)
	}
}

func inspectorServer(t *testing.T) *Server {
	t.Helper()
	return NewServer(nil, nil, nil, nil, nil, nil)
}

func serveInspector(t *testing.T, s *Server) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts
}

var _ = http.StatusOK

// mustJSON is the shared JSON serializer for wire assertions (the shell inspector.
func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
