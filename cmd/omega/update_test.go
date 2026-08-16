package main

import (
	"testing"
)

func TestAssetNameForOS(t *testing.T) {
	tests := []struct {
		goos, goarch, want string
	}{
		{"windows", "amd64", "windows_amd64"},
		{"linux", "amd64", "linux_amd64"},
		{"darwin", "arm64", "darwin_arm64"},
		{"darwin", "amd64", "darwin_amd64"},
	}
	for _, tt := range tests {
		if got := assetNameForOS(tt.goos, tt.goarch); got != tt.want {
			t.Errorf("assetNameForOS(%s, %s) = %q, want %q", tt.goos, tt.goarch, got, tt.want)
		}
	}
}

func TestFindAsset(t *testing.T) {
	assets := []githubAsset{
		{Name: "omega_windows_amd64.exe", BrowserDownloadURL: "https://example.com/win.exe"},
		{Name: "omega_linux_amd64", BrowserDownloadURL: "https://example.com/linux"},
		{Name: "omega_darwin_arm64", BrowserDownloadURL: "https://example.com/darwin"},
	}
	tests := []struct {
		goos, goarch, want string
	}{
		{"windows", "amd64", "https://example.com/win.exe"},
		{"linux", "amd64", "https://example.com/linux"},
		{"darwin", "arm64", "https://example.com/darwin"},
		{"darwin", "amd64", ""}, // no matching asset
		{"freebsd", "amd64", ""}, // no matching asset
	}
	for _, tt := range tests {
		got := findAsset(assets, tt.goos, tt.goarch)
		if got != tt.want {
			t.Errorf("findAsset(%s, %s) = %q, want %q", tt.goos, tt.goarch, got, tt.want)
		}
	}
}

func TestFindAssetEmpty(t *testing.T) {
	if got := findAsset(nil, "windows", "amd64"); got != "" {
		t.Fatalf("findAsset(nil) = %q, want \"\"", got)
	}
}
