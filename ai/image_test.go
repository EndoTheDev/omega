package ai

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestDetectImage(t *testing.T) {
	cases := []struct {
		name      string
		mediaType string
		content   []byte
	}{
		{
			name:      "png",
			mediaType: "image/png",
			content:   []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x01},
		},
		{
			name:      "jpeg",
			mediaType: "image/jpeg",
			content:   []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10},
		},
		{
			name:      "gif",
			mediaType: "image/gif",
			content:   []byte{'G', 'I', 'F', '8', '9', 'a', 0x01, 0x00, 0x01, 0x00, 0x00, 0x00},
		},
		{
			name:      "webp",
			mediaType: "image/webp",
			content:   []byte{'R', 'I', 'F', 'F', 0x00, 0x00, 0x00, 0x00, 'W', 'E', 'B', 'P', 0x56, 0x50, 0x38, 0x4c},
		},
		{
			name:      "bmp",
			mediaType: "image/bmp",
			content:   []byte{'B', 'M', 0x00, 0x01, 0x00, 0x00, 0x00, 0x00},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), tc.name+".img")
			if err := os.WriteFile(path, tc.content, 0o644); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
			img, err := DetectImage(path)
			if err != nil {
				t.Fatalf("DetectImage: unexpected error: %v", err)
			}
			if img == nil {
				t.Fatal("DetectImage returned nil, want ImageContent")
			}
			if img.MediaType != tc.mediaType {
				t.Errorf("MediaType = %q, want %q", img.MediaType, tc.mediaType)
			}
			if img.Base64 == "" {
				t.Error("Base64 is empty, want non-empty")
			}
			decoded, err := base64.StdEncoding.DecodeString(img.Base64)
			if err != nil {
				t.Fatalf("base64 decode: %v", err)
			}
			if string(decoded) != string(tc.content) {
				t.Errorf("decoded base64 does not match original file bytes")
			}
		})
	}
}

func TestDetectImageNonImage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-an-image.txt")
	if err := os.WriteFile(path, []byte("hello, this is plain text"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	img, err := DetectImage(path)
	if err != nil {
		t.Fatalf("DetectImage: unexpected error: %v", err)
	}
	if img != nil {
		t.Errorf("DetectImage returned %v, want nil for non-image", img)
	}
}

func TestDetectImageMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.img")
	img, err := DetectImage(path)
	if err == nil {
		t.Fatal("DetectImage: expected error for missing file, got nil")
	}
	if img != nil {
		t.Errorf("DetectImage returned %v for missing file, want nil", img)
	}
}

func TestDetectImageTooLarge(t *testing.T) {
	// Create a file just over MaxImageBytes with a PNG header so the size
	// check is the failing condition (DetectImage reads the whole file).
	path := filepath.Join(t.TempDir(), "big.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := f.Write([]byte{0x89, 0x50, 0x4e, 0x47}); err != nil {
		t.Fatalf("Write header: %v", err)
	}
	if err := f.Truncate(MaxImageBytes + 1); err != nil {
		t.Fatalf("Truncate: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	img, err := DetectImage(path)
	if err == nil {
		t.Fatal("DetectImage: expected error for oversized file, got nil")
	}
	if img != nil {
		t.Errorf("DetectImage returned %v for oversized file, want nil", img)
	}
}

func TestDetectImageWebPRejectsRIFFOnly(t *testing.T) {
	// RIFF header but no WEBP marker at offset 8 should not match webp.
	path := filepath.Join(t.TempDir(), "riff-only.bin")
	content := []byte{'R', 'I', 'F', 'F', 0x00, 0x00, 0x00, 0x00, 'W', 'A', 'V', 'E'}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	img, err := DetectImage(path)
	if err != nil {
		t.Fatalf("DetectImage: unexpected error: %v", err)
	}
	if img != nil {
		t.Errorf("DetectImage returned %v for RIFF/WAVE, want nil", img)
	}
}