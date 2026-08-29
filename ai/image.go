package ai

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"
)

// imageMagic holds magic-byte signatures for supported image formats.
var imageMagic = []struct {
	MediaType string
	prefix    []byte
}{
	{"image/png", []byte{0x89, 0x50, 0x4e, 0x47}},       // \x89PNG
	{"image/jpeg", []byte{0xff, 0xd8, 0xff}},              // \xff\xd8\xff
	{"image/gif", []byte{'G', 'I', 'F', '8'}},            // GIF8
	{"image/webp", []byte{'R', 'I', 'F', 'F'}},           // RIFF....WEBP (check WEBP at offset 8)
	{"image/bmp", []byte{'B', 'M'}},                      // BM
}

// MaxImageBytes is the maximum image file size accepted by DetectImage.
const MaxImageBytes = 20 * 1024 * 1024 // 20 MB

// DetectImage reads a file and returns ImageContent if it's a supported
// image format, or nil if it's not an image. Returns an error on read
// failure or if the file exceeds MaxImageBytes.
func DetectImage(path string) (*ImageContent, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if len(data) > MaxImageBytes {
		return nil, fmt.Errorf("image too large: %s (%d bytes, max %d)", path, len(data), MaxImageBytes)
	}

	mediaType := ""
	for _, sig := range imageMagic {
		if len(data) < len(sig.prefix) {
			continue
		}
		if !strings.HasPrefix(string(data[:len(sig.prefix)]), string(sig.prefix)) {
			continue
		}
		// WebP needs a secondary check: bytes 8-12 are "WEBP".
		if sig.MediaType == "image/webp" {
			if len(data) < 12 || string(data[8:12]) != "WEBP" {
				continue
			}
		}
		mediaType = sig.MediaType
		break
	}
	if mediaType == "" {
		return nil, nil // not an image
	}

	return &ImageContent{
		MediaType: mediaType,
		Base64:    base64.StdEncoding.EncodeToString(data),
	}, nil
}
