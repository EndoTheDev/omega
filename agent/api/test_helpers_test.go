package api

// Inspector-surface helpers for the base-package tests (the shell's
// inspector test file owns the originals; after the carve these two
// fixtures serve both sides - card t_905fa502).

import (
	"encoding/json"
	"net/http"
	httptest "net/http/httptest"
	"testing"
)

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

// mustJSON is the shared JSON serializer for wire assertions (the
// shell inspector test owns the original after the carve).
func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
