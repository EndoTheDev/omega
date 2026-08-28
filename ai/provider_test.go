package ai

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSSEData(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		want    string
		wantOK  bool
		wantErr bool
	}{
		{
			name:   "simple_data_line",
			input:  "data: hello\n",
			want:   "hello",
			wantOK: true,
		},
		{
			name:   "data_line_no_space",
			input:  "data:payload\n",
			want:   "payload",
			wantOK: true,
		},
		{
			name:   "data_line_trailing_space",
			input:  "data:   spaced  \n",
			want:   "spaced",
			wantOK: true,
		},
		{
			name:   "skips_comment_then_data",
			input:  ": comment line\ndata: real\n",
			want:   "real",
			wantOK: true,
		},
		{
			name:   "skips_event_then_data",
			input:  "event: ping\ndata: payload\n",
			want:   "payload",
			wantOK: true,
		},
		{
			name:   "skips_blank_then_data",
			input:  "\n\ndata: after-blank\n",
			want:   "after-blank",
			wantOK: true,
		},
		{
			name:   "skips_done_sentinel_then_data",
			input:  "data: [DONE]\ndata: after-done\n",
			want:   "after-done",
			wantOK: true,
		},
		{
			name:   "skips_empty_data_then_data",
			input:  "data:\ndata: filled\n",
			want:   "filled",
			wantOK: true,
		},
		{
			name:   "data_with_crlf",
			input:  "data: crlf\r\n",
			want:   "crlf",
			wantOK: true,
		},
		{
			name:    "done_only_returns_eof",
			input:   "data: [DONE]\n",
			want:    "",
			wantOK:  false,
			wantErr: true, // ReadString hits EOF without a data payload
		},
		{
			name:    "empty_input_eof",
			input:   "",
			want:    "",
			wantOK:  false,
			wantErr: true,
		},
		{
			name:    "comments_only_eof",
			input:   ": a\n: b\n",
			want:    "",
			wantOK:  false,
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := bufio.NewReader(strings.NewReader(tc.input))
			got, ok, err := SSEData(r)
			if tc.wantErr && err == nil {
				t.Errorf("SSEData() err = nil, want non-nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("SSEData() err = %v, want nil", err)
			}
			if ok != tc.wantOK {
				t.Errorf("SSEData() ok = %v, want %v", ok, tc.wantOK)
			}
			if got != tc.want {
				t.Errorf("SSEData() payload = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSSEDataSequential(t *testing.T) {
	// A single bufio.Reader can be read multiple times to walk the stream.
	input := "event: ping\ndata: first\n: comment\ndata: second\ndata: [DONE]\ndata: third\n"
	r := bufio.NewReader(strings.NewReader(input))

	want := []string{"first", "second", "third"}
	for i, w := range want {
		got, ok, err := SSEData(r)
		if err != nil {
			t.Fatalf("read %d: unexpected error: %v", i, err)
		}
		if !ok {
			t.Fatalf("read %d: ok=false, want true", i)
		}
		if got != w {
			t.Errorf("read %d: payload = %q, want %q", i, got, w)
		}
	}
}

func TestSetHTTPTimeout(t *testing.T) {
	// Save and restore original timeout.
	orig := httpClient.Timeout
	t.Cleanup(func() { httpClient.Timeout = orig })

	t.Run("positive_sets_timeout", func(t *testing.T) {
		SetHTTPTimeout(42)
		if got := httpClient.Timeout; got != 42*time.Second {
			t.Errorf("httpClient.Timeout = %v, want 42s", got)
		}
	})

	t.Run("zero_ignored", func(t *testing.T) {
		SetHTTPTimeout(99)
		SetHTTPTimeout(0)
		if got := httpClient.Timeout; got != 99*time.Second {
			t.Errorf("httpClient.Timeout = %v, want 99s (unchanged by 0)", got)
		}
	})

	t.Run("negative_ignored", func(t *testing.T) {
		SetHTTPTimeout(50)
		SetHTTPTimeout(-5)
		if got := httpClient.Timeout; got != 50*time.Second {
			t.Errorf("httpClient.Timeout = %v, want 50s (unchanged by -5)", got)
		}
	})
}

func TestHTTPClient(t *testing.T) {
	c := HTTPClient()
	if c == nil {
		t.Fatal("HTTPClient() = nil, want non-nil")
	}
	if c != httpClient {
		t.Error("HTTPClient() returned a different pointer than the shared httpClient")
	}
}

func TestThinkingEnabled(t *testing.T) {
	cases := []struct {
		level string
		want  bool
	}{
		{"", false},
		{"none", false},
		{"off", false},
		{"on", true},
		{"minimal", true},
		{"low", true},
		{"medium", true},
		{"high", true},
		{"extra high", true},
		{"max", true},
		{"ultra", true},
		{"anything-else", true},
	}

	for _, tc := range cases {
		t.Run(fmt.Sprintf("level_%q", tc.level), func(t *testing.T) {
			if got := ThinkingEnabled(tc.level); got != tc.want {
				t.Errorf("ThinkingEnabled(%q) = %v, want %v", tc.level, got, tc.want)
			}
		})
	}
}

// --- RetryHTTP / retryHTTP via httptest ---

// newRetryReq builds a retryable GET request (bytes.Reader body => GetBody set).
func newRetryReq(t *testing.T, target string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, target, bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	return req
}

func TestRetryHTTP_SuccessFirstTry(t *testing.T) {
	t.Setenv("OMEGA_MAX_RETRIES", "0")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	req := newRetryReq(t, srv.URL)
	resp, err := RetryHTTP(req)
	if err != nil {
		t.Fatalf("RetryHTTP error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestRetryHTTP_NoRetryOn400(t *testing.T) {
	t.Setenv("OMEGA_MAX_RETRIES", "0")
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	req := newRetryReq(t, srv.URL)
	resp, err := RetryHTTP(req)
	if err != nil {
		t.Fatalf("RetryHTTP error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
	if calls != 1 {
		t.Errorf("server calls = %d, want 1 (no retry on 4xx)", calls)
	}
}

func TestRetryHTTP_RetryOn429ThenSucceed(t *testing.T) {
	t.Setenv("OMEGA_MAX_RETRIES", "1")
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	req := newRetryReq(t, srv.URL)
	resp, err := RetryHTTP(req)
	if err != nil {
		t.Fatalf("RetryHTTP error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 after retry", resp.StatusCode)
	}
	if calls != 2 {
		t.Errorf("server calls = %d, want 2", calls)
	}
}

func TestRetryHTTP_RetryOn503ThenSucceed(t *testing.T) {
	t.Setenv("OMEGA_MAX_RETRIES", "1")
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	req := newRetryReq(t, srv.URL)
	resp, err := RetryHTTP(req)
	if err != nil {
		t.Fatalf("RetryHTTP error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 after retry", resp.StatusCode)
	}
	if calls != 2 {
		t.Errorf("server calls = %d, want 2", calls)
	}
}

func TestRetryHTTP_ExhaustsRetries(t *testing.T) {
	t.Setenv("OMEGA_MAX_RETRIES", "1")
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	req := newRetryReq(t, srv.URL)
	resp, err := RetryHTTP(req)
	if err != nil {
		t.Fatalf("RetryHTTP error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 (last attempt)", resp.StatusCode)
	}
	if calls != 2 {
		t.Errorf("server calls = %d, want 2 (1 initial + 1 retry)", calls)
	}
}

func TestRetryHTTP_ContextCancellation(t *testing.T) {
	t.Setenv("OMEGA_MAX_RETRIES", "1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL, bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	_, err = retryHTTP(ctx, req)
	if err == nil {
		t.Fatal("retryHTTP error = nil, want context deadline error")
	}
}