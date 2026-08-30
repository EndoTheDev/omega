package main

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCmdHealthOK verifies cmdHealth returns nil when a server responds
// with 200 OK on /health at the configured port.
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

// TestCmdHealthNotRunning verifies cmdHealth returns an error containing
// "not reachable" when nothing is listening on the configured port.
func TestCmdHealthNotRunning(t *testing.T) {
	// Port 1 is reserved and extremely unlikely to have a listener.
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

// TestCmdHealthBadStatus verifies cmdHealth returns an error containing
// "returned" when the server responds with a non-200 status.
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