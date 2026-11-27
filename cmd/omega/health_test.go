package main

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestDialableBind: a wildcard bind folds to loopback for the probe dial; an exact bind
// stays exact because a loopback dial finds no listener there.
func TestDialableBind(t *testing.T) {
	tests := []struct{ addr, want string }{
		{":8099", "127.0.0.1:8099"},
		{"0.0.0.0:8099", "127.0.0.1:8099"},
		{":::8099", "127.0.0.1:8099"},
		{"[::]:8099", "127.0.0.1:8099"},
		{"100.64.1.2:8099", "100.64.1.2:8099"},
		{"127.0.0.1:8099", "127.0.0.1:8099"},
	}
	for _, tt := range tests {
		if got := dialableBind(tt.addr); got != tt.want {
			t.Errorf("dialableBind(%q) = %q, want %q", tt.addr, got, tt.want)
		}
	}
}

// TestSelfHost: the probe's Host is the machine's own name on the serve's port - the one
// identity the guard derives for every bind shape.
func TestSelfHost(t *testing.T) {
	name, err := os.Hostname()
	if err != nil {
		t.Fatalf("os.Hostname: %v", err)
	}
	want := strings.ToLower(strings.TrimSpace(name)) + ":8099"
	if got := selfHost("8099"); got != want {
		t.Fatalf("selfHost(8099) = %q, want %q", got, want)
	}
	if got := selfHost("8099"); strings.HasPrefix(got, ":") {
		t.Fatalf("selfHost(8099) = %q, want a non-empty machine name", got)
	}
}

// TestHealthBodySendsSelfHost: a wildcard-bound serve refuses a loopback Host, so the
// probe must carry the machine's own name or the boot gate tears down a healthy serve.
func TestHealthBodySendsSelfHost(t *testing.T) {
	var want string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != want {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		fmt.Fprint(w, `{"status":"ok"}`)
	}))
	defer ts.Close()
	want = selfHost(strconv.Itoa(ts.Listener.Addr().(*net.TCPAddr).Port))

	body, err := healthBody(&http.Client{Timeout: 2 * time.Second}, ts.URL)
	if err != nil {
		t.Fatalf("healthBody: %v", err)
	}
	if !strings.Contains(body, `"status":"ok"`) {
		t.Fatalf("healthBody body = %q, want the healthy answer", body)
	}
}

// TestHealthBodySurfacesRefusal: a refused Host is an error, never a silent pass.
func TestHealthBodySurfacesRefusal(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer ts.Close()

	if _, err := healthBody(&http.Client{Timeout: 2 * time.Second}, ts.URL); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("healthBody error = %v, want the 403 surfaced", err)
	}
}

// TestCmdHealthOK verifies cmdHealth returns nil when a server responds with 200 OK.
func TestCmdHealthOK(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	port := ts.Listener.Addr().(*net.TCPAddr).Port
	cfgPath := writeTempConfig(t, fmt.Sprintf(`
provider:
  model_name: llama3
server:
  port: %d
`, port))

	t.Setenv("OMEGA_HEALTH_HOST", "127.0.0.1")

	if err := cmdHealth(cfgPath); err != nil {
		t.Fatalf("cmdHealth returned error: %v", err)
	}
}

// TestCmdHealthNotRunning verifies cmdHealth returns an error containing "not.
func TestCmdHealthNotRunning(t *testing.T) {
	cfgPath := writeTempConfig(t, `
provider:
  model_name: llama3
server:
  port: 1
`)

	t.Setenv("OMEGA_HEALTH_HOST", "127.0.0.1")

	err := cmdHealth(cfgPath)
	if err == nil {
		t.Fatal("cmdHealth returned nil, want error")
	}
	if !strings.Contains(err.Error(), "not reachable") {
		t.Errorf("error = %q, want substring %q", err.Error(), "not reachable")
	}
}

// TestCmdHealthBadStatus verifies cmdHealth returns an error containing "returned".
func TestCmdHealthBadStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer ts.Close()

	port := ts.Listener.Addr().(*net.TCPAddr).Port
	cfgPath := writeTempConfig(t, fmt.Sprintf(`
provider:
  model_name: llama3
server:
  port: %d
`, port))

	t.Setenv("OMEGA_HEALTH_HOST", "127.0.0.1")

	err := cmdHealth(cfgPath)
	if err == nil {
		t.Fatal("cmdHealth returned nil, want error")
	}
	if !strings.Contains(err.Error(), "returned") {
		t.Errorf("error = %q, want substring %q", err.Error(), "returned")
	}
}

// TestFindChecksumURL exercises findChecksumURL across a table of inputs:
// assets containing checksums.txt, assets lacking it, and nil assets.
func TestFindChecksumURL(t *testing.T) {
	const wantURL = "https://example.com/checksums.txt"

	tests := []struct {
		name   string
		assets []githubAsset
		want   string
	}{
		{
			name: "has checksums.txt",
			assets: []githubAsset{
				{Name: "omega_windows_amd64.zip", BrowserDownloadURL: "https://example.com/win.zip"},
				{Name: "checksums.txt", BrowserDownloadURL: wantURL},
			},
			want: wantURL,
		},
		{
			name: "missing checksums.txt",
			assets: []githubAsset{
				{Name: "omega_windows_amd64.zip", BrowserDownloadURL: "https://example.com/win.zip"},
				{Name: "omega_linux_amd64.tar.gz", BrowserDownloadURL: "https://example.com/linux.tar.gz"},
			},
			want: "",
		},
		{
			name:   "nil assets",
			assets: nil,
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := findChecksumURL(tt.assets); got != tt.want {
				t.Errorf("findChecksumURL() = %q, want %q", got, tt.want)
			}
		})
	}
}
