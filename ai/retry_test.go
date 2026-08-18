package ai

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestRetryHTTPTransient verifies that a 500 followed by a success is
// retried and returns the successful response.
func TestRetryHTTPTransient(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	req, err := http.NewRequestWithContext(context.Background(), "POST", srv.URL, bytes.NewReader([]byte("{}")))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := retryHTTP(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if calls != 3 {
		t.Fatalf("expected 3 calls (2 retries), got %d", calls)
	}
}

// TestRetryHTTPClientError verifies a 400 is not retried.
func TestRetryHTTPClientError(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	req, _ := http.NewRequestWithContext(context.Background(), "POST", srv.URL, bytes.NewReader([]byte("{}")))
	resp, err := retryHTTP(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call (no retry on 4xx), got %d", calls)
	}
}

// TestRetryHTTPContextCancel verifies a cancelled context stops retries.
func TestRetryHTTPContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "POST", srv.URL, bytes.NewReader([]byte("{}")))
	cancel()
	_, err := retryHTTP(ctx, req)
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
}

// TestRetryHTTPZeroRetries verifies OMEGA_MAX_RETRIES=0 disables retries.
func TestRetryHTTPZeroRetries(t *testing.T) {
	t.Setenv("OMEGA_MAX_RETRIES", "0")
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	req, _ := http.NewRequestWithContext(context.Background(), "POST", srv.URL, bytes.NewReader([]byte("{}")))
	resp, err := retryHTTP(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call with retries disabled, got %d", calls)
	}
}

// TestRetryHTTPBackoff verifies the backoff schedule is 1s, 2s, 4s.
func TestRetryHTTPBackoff(t *testing.T) {
	// Jitter is at most base/4, so the lower bound is base.
	if got := backoff(0); got < time.Second || got > 1250*time.Millisecond {
		t.Fatalf("backoff(0) = %v, want ~1s", got)
	}
	if got := backoff(1); got < 2*time.Second || got > 2500*time.Millisecond {
		t.Fatalf("backoff(1) = %v, want ~2s", got)
	}
	if got := backoff(2); got < 4*time.Second || got > 5*time.Second {
		t.Fatalf("backoff(2) = %v, want ~4s", got)
	}
}
