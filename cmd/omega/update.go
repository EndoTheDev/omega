package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
)

// githubRelease represents the subset of the GitHub releases API
// response that omega update needs.
type githubRelease struct {
	TagName string          `json:"tag_name"`
	Assets  []githubAsset   `json:"assets"`
	Message string          `json:"message"` // API error message (e.g. "Not Found")
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// assetNameForOS returns the expected release asset name for the
// current GOOS/GOARCH (e.g. "windows_amd64", "darwin_arm64").
func assetNameForOS(goos, goarch string) string {
	return goos + "_" + goarch
}

// findAsset returns the download URL for the asset matching the current
// OS/arch, or "" if none matches.
func findAsset(assets []githubAsset, goos, goarch string) string {
	target := assetNameForOS(goos, goarch)
	for _, a := range assets {
		// Match assets that contain the target string (e.g.
		// "omega_windows_amd64.exe" matches "windows_amd64").
		if strings.Contains(a.Name, target) {
			return a.BrowserDownloadURL
		}
	}
	return ""
}

// cmdUpdate downloads the latest GitHub release binary and replaces the
// running executable. Handles platform-specific replacement: on Windows
// the running exe must be renamed before the new one takes its place.
func cmdUpdate() error {
	fmt.Fprintln(os.Stderr, "omega: checking for latest release...")

	resp, err := http.Get("https://api.github.com/repos/EndoTheDev/omega/releases/latest")
	if err != nil {
		return fmt.Errorf("fetch releases: %w", err)
	}
	defer resp.Body.Close()

	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return fmt.Errorf("decode release: %w", err)
	}

	// GitHub returns 404 with a message field when there are no releases.
	if rel.Message != "" {
		return fmt.Errorf("no releases found: %s; visit https://github.com/EndoTheDev/omega/releases", rel.Message)
	}
	if rel.TagName == "" {
		return fmt.Errorf("no releases found; visit https://github.com/EndoTheDev/omega/releases")
	}

	url := findAsset(rel.Assets, runtime.GOOS, runtime.GOARCH)
	if url == "" {
		return fmt.Errorf("no release asset for %s/%s in %s; visit https://github.com/EndoTheDev/omega/releases", runtime.GOOS, runtime.GOARCH, rel.TagName)
	}

	fmt.Fprintf(os.Stderr, "omega: downloading %s...\n", rel.TagName)

	dlResp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer dlResp.Body.Close()

	if dlResp.StatusCode != http.StatusOK {
		return fmt.Errorf("download: HTTP %d", dlResp.StatusCode)
	}

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}

	// Write to a temp file in the same directory as the executable.
	tmpPath := exePath + ".new"
	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	if _, err := io.Copy(tmpFile, dlResp.Body); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("download: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp file: %w", err)
	}

	// Replace the running binary. On Windows, the running exe cannot be
	// overwritten directly; rename it out of the way first.
	if runtime.GOOS == "windows" {
		oldPath := exePath + ".old"
		os.Remove(oldPath) // best-effort cleanup of a previous update
		if err := os.Rename(exePath, oldPath); err != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("rename current exe: %w", err)
		}
		if err := os.Rename(tmpPath, exePath); err != nil {
			// Try to restore the old binary.
			os.Rename(oldPath, exePath)
			os.Remove(tmpPath)
			return fmt.Errorf("install new exe: %w", err)
		}
		os.Remove(oldPath) // best-effort; may fail if still locked
	} else {
		if err := os.Rename(tmpPath, exePath); err != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("install: %w", err)
		}
	}

	fmt.Printf("omega: updated to %s\n", rel.TagName)
	return nil
}
