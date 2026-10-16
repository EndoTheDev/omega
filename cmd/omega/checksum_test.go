package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newArchiveFile creates a temp file with the given content and returns its path.
func newArchiveFile(t *testing.T, content []byte) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "archive.tar.gz")
	if err := os.WriteFile(p, content, 0o644); err != nil {
		t.Fatalf("write archive: %v", err)
	}
	return p
}

// checksumServer serves the given checksums.txt body.
func checksumServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// The real contract: the caller passes the bare platform tag from
// assetNameForOS ("linux_arm64"), and checksums.txt lines carry the full
// release filename ("omega-v0.6.1-linux_arm64.tar.gz") - which CONTAINS
// the tag but never ends with it.

func TestVerifyChecksumMatch(t *testing.T) {
	content := []byte("fake archive content")
	archivePath := newArchiveFile(t, content)

	h := sha256.Sum256(content)
	realHash := hex.EncodeToString(h[:])
	assetName := "linux_arm64"

	body := fmt.Sprintf("abc123  omega-v0.6.1-darwin_arm64.tar.gz\n%s  omega-v0.6.1-%s.tar.gz\n", realHash, assetName)
	srv := checksumServer(t, body)

	if err := verifyChecksum(archivePath, srv.URL, assetName); err != nil {
		t.Fatalf("verifyChecksum returned error on match: %v", err)
	}
}

func TestVerifyChecksumMismatch(t *testing.T) {
	content := []byte("fake archive content")
	archivePath := newArchiveFile(t, content)
	assetName := "linux_arm64"

	// Deliberately wrong hash, real-world line format.
	body := "0000000000000000000000000000000000000000000000000000000000000000  omega-v0.6.1-linux_arm64.tar.gz\n"
	srv := checksumServer(t, body)

	err := verifyChecksum(archivePath, srv.URL, assetName)
	if err == nil {
		t.Fatal("verifyChecksum returned nil on mismatch, want error")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("verifyChecksum error = %q, want substring 'checksum mismatch'", err.Error())
	}
}

func TestVerifyChecksumNoEntry(t *testing.T) {
	content := []byte("fake archive content")
	archivePath := newArchiveFile(t, content)
	assetName := "linux_arm64"

	// checksums.txt has entries, but none for our asset.
	body := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789  omega-v0.6.1-windows_amd64.zip\n"
	srv := checksumServer(t, body)

	err := verifyChecksum(archivePath, srv.URL, assetName)
	if err == nil {
		t.Fatal("verifyChecksum returned nil on missing entry, want error")
	}
	if !strings.Contains(err.Error(), "no entry") {
		t.Errorf("verifyChecksum error = %q, want substring 'no entry'", err.Error())
	}
}

func TestVerifyChecksumHTTPError(t *testing.T) {
	content := []byte("fake archive content")
	archivePath := newArchiveFile(t, content)
	assetName := "linux_arm64"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	err := verifyChecksum(archivePath, srv.URL, assetName)
	if err == nil {
		t.Fatal("verifyChecksum returned nil on HTTP 500, want error")
	}
	if !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("verifyChecksum error = %q, want substring 'HTTP 500'", err.Error())
	}
}