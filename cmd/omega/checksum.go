package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// findChecksumURL returns the download URL for checksums.txt, or "" if
// the release doesn't include one (older releases predating checksums).
func findChecksumURL(assets []githubAsset) string {
	for _, a := range assets {
		if a.Name == "checksums.txt" {
			return a.BrowserDownloadURL
		}
	}
	return ""
}

// verifyChecksum downloads checksums.txt from the release, finds the
// SHA256 hash for the given asset name, and verifies that archivePath
// matches. Returns nil if verification succeeds, or an error if the
// hash doesn't match.
func verifyChecksum(archivePath, checksumsURL, assetName string) error {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(checksumsURL)
	if err != nil {
		return fmt.Errorf("fetch checksums: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch checksums: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read checksums: %w", err)
	}
	// Parse "sha256  filename" lines (sha256sum output format).
	wantHash := ""
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.HasSuffix(fields[1], assetName) {
			wantHash = fields[0]
			break
		}
	}
	if wantHash == "" {
		return fmt.Errorf("checksums.txt has no entry for %s", assetName)
	}
	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open archive for hashing: %w", err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("hash archive: %w", err)
	}
	gotHash := hex.EncodeToString(h.Sum(nil))
	if gotHash != wantHash {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", wantHash, gotHash)
	}
	return nil
}
