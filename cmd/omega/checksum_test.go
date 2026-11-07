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

// The both-shapes contract (card t_3d123470). Every release checksums.txt
// carries TWO lines per asset with the same hash: the full release
// filename (current substring matcher) and a bare platform-tag alias
// ("windows_amd64"). The alias exists because binaries built before
// 2026-10-16 (v0.6.0 and older) match checksum entries with
// strings.HasSuffix(filename, assetName) - a real filename ends with
// .zip/.tar.gz, never the bare tag, so those installs abort self-update
// with "no entry" and strand. They cannot be patched; the release side
// must stay legible to their matcher. scripts/release-checksums.sh
// generates both shapes and scripts/release-checksums_test.sh pins the
// emission; these tests pin the CONSUMER side from this binary's seat.

// TestVerifyChecksumBothShapes verifies the current updater against a
// checksums.txt emitted by scripts/release-checksums.sh: full lines and
// alias lines interleaved, both carrying the real hash. The alias lines
// must not confuse the current matcher into the wrong entry.
func TestVerifyChecksumBothShapes(t *testing.T) {
	content := []byte("fake archive content")
	archivePath := newArchiveFile(t, content)
	h := sha256.Sum256(content)
	realHash := hex.EncodeToString(h[:])
	assetName := "linux_arm64"

	// Byte-for-byte the shape release-checksums.sh emits (asset pair:
	// full line then its alias, per asset).
	body := fmt.Sprintf(""+
		"e412488a0f72e241b2cd06a66f44ba21b67a05ce541db5092eac4afa69e9a6a9  omega-v0.15.2-darwin_arm64.tar.gz\n"+
		"e412488a0f72e241b2cd06a66f44ba21b67a05ce541db5092eac4afa69e9a6a9  darwin_arm64\n"+
		"%s  omega-v0.15.2-%s.tar.gz\n"+
		"%s  %s\n",
		realHash, assetName, realHash, assetName)
	srv := checksumServer(t, body)

	if err := verifyChecksum(archivePath, srv.URL, assetName); err != nil {
		t.Fatalf("verifyChecksum failed on both-shapes checksums.txt: %v", err)
	}
}

// TestVerifyChecksumAliasOnlyShape: a future format change may drop the
// full-filename lines; the current substring matcher must still verify
// against alias-only files instead of stranding (the compat decision is
// documented in release-checksums.sh - whoever flips the format reads it).
func TestVerifyChecksumAliasOnlyShape(t *testing.T) {
	content := []byte("fake archive content")
	archivePath := newArchiveFile(t, content)
	h := sha256.Sum256(content)
	realHash := hex.EncodeToString(h[:])
	assetName := "windows_amd64"

	body := fmt.Sprintf("%s  %s\n", realHash, assetName)
	srv := checksumServer(t, body)

	if err := verifyChecksum(archivePath, srv.URL, assetName); err != nil {
		t.Fatalf("verifyChecksum failed on alias-only checksums.txt: %v", err)
	}
}

// legacyChecksumLookup is the pre-Oct-16 entry matcher, VERBATIM from
// v0.5.1 cmd/omega/checksum.go (the code baked into every stranded
// binary): first "hash  filename" line whose filename ENDS with the
// platform tag. Returns "" when no line matches.
func legacyChecksumLookup(checksums, assetName string) string {
	wantHash := ""
	for _, line := range strings.Split(checksums, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.HasSuffix(fields[1], assetName) {
			wantHash = fields[0]
			break
		}
	}
	return wantHash
}

// TestLegacySuffixMatcherFindsCompatEntry pins why the alias lines are
// load-bearing: the legacy matcher finds an entry ONLY in the both-shapes
// file and never in a full-filename-only file (the strand, reproduced by
// the real v0.5.1 binary against the live v0.15.2 release on 2026-11-08:
// "checksum verification failed: checksums.txt has no entry for
// windows_amd64"). If this reds after a release-format change, a
// pre-v0.7.0 install just lost its only self-update path.
func TestLegacySuffixMatcherFindsCompatEntry(t *testing.T) {
	const (
		hash      = "3e9a490032f66067845b05f2afc67494578cc59389466f8a5316caeee1556a3d"
		assetName = "windows_amd64"
	)
	fullOnly := hash + "  omega-v0.15.2-windows_amd64.zip\n"
	bothShapes := fullOnly + hash + "  " + assetName + "\n"

	if got := legacyChecksumLookup(fullOnly, assetName); got != "" {
		t.Errorf("legacy matcher found entry %q in full-only file, want none", got)
	}
	if got := legacyChecksumLookup(bothShapes, assetName); got != hash {
		t.Errorf("legacy matcher entry = %q, want %q (the compat alias line)", got, hash)
	}
}
