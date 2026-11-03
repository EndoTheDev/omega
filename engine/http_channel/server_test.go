package http_channel

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)


func TestHealth(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Status        string `json:"status"`
		Version       string `json:"version"`
		UptimeSeconds int    `json:"uptime_seconds"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Status != "ok" {
		t.Fatalf("status field = %q, want ok", body.Status)
	}
	// uptime is anchored at server construction - non-negative, present
	if body.UptimeSeconds < 0 {
		t.Fatalf("uptime_seconds = %d, want >= 0", body.UptimeSeconds)
	}
	if body.Version != "" {
		t.Fatalf("version = %q, want absent without a host build tag", body.Version)
	}
}

// TestHealthVersion: the host build tag rides the health payload. The
// version field must match exactly what the host supplied.
func TestHealthVersion(t *testing.T) {
	s := newTestServer()
	s.version = "v0.5.0-test"
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["version"] != "v0.5.0-test" {
		t.Fatalf("version = %v, want v0.5.0-test", body["version"])
	}
	if body["uptime_seconds"] == nil {
		t.Fatalf("uptime_seconds missing")
	}
}

func TestStaticIndex(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	// works for the committed placeholder AND a synced real build
	if !strings.Contains(rec.Body.String(), "<!DOCTYPE html") {
		t.Fatalf("index body missing doctype")
	}
}

// TestSPAFallback pins the history-mode fallback: deep route paths serve
// the SPA shell, asset-like misses under /_nuxt/ still 404.
func TestSPAFallback(t *testing.T) {
	s := newTestServer()
	h := s.Handler()

	// deep route path -> 200 + SPA shell
	req := httptest.NewRequest(http.MethodGet, "/chat/anything", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/chat/anything status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "<!DOCTYPE html") {
		t.Fatalf("/chat/anything body missing SPA shell doctype")
	}

	// asset miss under /_nuxt/ -> 404, never the shell
	req = httptest.NewRequest(http.MethodGet, "/_nuxt/missing.js", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("/_nuxt/missing.js status = %d, want 404", rec.Code)
	}

	// real assets keep winning: FileServer canonically redirects
	// /index.html to / (301), not the shell path
	req = httptest.NewRequest(http.MethodGet, "/index.html", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("/index.html status = %d, want 301", rec.Code)
	}
}


func TestHealthMethodNotAllowed(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodPost, "/health", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}
