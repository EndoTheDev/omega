package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/EndoTheDev/omega/ai"
)

// imageMagic holds magic-byte signatures for supported image formats.
var imageMagic = []struct {
MediaType string
	prefix   []byte
}{
	{"image/png", []byte{0x89, 0x50, 0x4e, 0x47}},       // \x89PNG
	{"image/jpeg", []byte{0xff, 0xd8, 0xff}},              // \xff\xd8\xff
	{"image/gif", []byte{'G', 'I', 'F', '8'}},            // GIF8
	{"image/webp", []byte{'R', 'I', 'F', 'F'}},           // RIFF....WEBP (check WEBP at offset 8)
	{"image/bmp", []byte{'B', 'M'}},                      // BM
}

// detectImage reads a file and returns ImageContent if it's a supported
// image format, or nil if it's not an image. Returns an error on read
// failure or if the file exceeds maxImageBytes.
const maxImageBytes = 20 * 1024 * 1024 // 20 MB

func detectImage(path string) (*ai.ImageContent, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if len(data) > maxImageBytes {
		return nil, fmt.Errorf("image too large: %s (%d bytes, max %d)", path, len(data), maxImageBytes)
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

	return &ai.ImageContent{
		MediaType: mediaType,
		Base64:    base64.StdEncoding.EncodeToString(data),
	}, nil
}

// parseFileArgs splits args into image content and text prompt. Args
// starting with "@" are treated as file references: image files are
// loaded as base64 ImageContent, text files are inlined into the prompt.
// Non-file args become the text prompt.
func parseFileArgs(args []string) (string, []ai.ImageContent, error) {
	var promptParts []string
	var images []ai.ImageContent
	for _, arg := range args {
		if !strings.HasPrefix(arg, "@") {
			promptParts = append(promptParts, arg)
			continue
		}
		path := arg[1:]
		img, err := detectImage(path)
		if err != nil {
			return "", nil, err
		}
		if img != nil {
			images = append(images, *img)
			promptParts = append(promptParts, "["+img.MediaType+": "+path+"]")
			continue
		}
		// Not an image: inline the file contents into the prompt.
		data, err := os.ReadFile(path)
		if err != nil {
			return "", nil, fmt.Errorf("read %s: %w", path, err)
		}
		promptParts = append(promptParts, string(data))
	}
	return strings.Join(promptParts, " "), images, nil
}

// atFilePattern matches @<non-space> tokens in a text string.
var atFilePattern = regexp.MustCompile(`@\S+`)

// extractImages scans a text string for @path tokens, loads any image
// files as base64 ImageContent, and inlines text files. Tokens that
// don't resolve to an existing file are left as-is in the text. Used
// by the TUI submit path to support @file references in chat input.
func extractImages(input string) (prompt string, images []ai.ImageContent, err error) {
	prompt = input
	var loadedImages []ai.ImageContent

	prompt = atFilePattern.ReplaceAllStringFunc(input, func(token string) string {
		path := token[1:] // strip @

		// If the file doesn't exist, leave the token as-is.
		info, statErr := os.Stat(path)
		if statErr != nil || info.IsDir() {
			return token
		}

		// Try image detection.
		img, detectErr := detectImage(path)
		if detectErr != nil {
			err = detectErr
			return token
		}
		if img != nil {
			loadedImages = append(loadedImages, *img)
			return "[" + img.MediaType + ": " + path + "]"
		}

		// Not an image: inline the file contents.
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			err = readErr
			return token
		}
		return string(data)
	})

	return prompt, loadedImages, err
}