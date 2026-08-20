package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/EndoTheDev/omega/agent"
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
// files as base64 ImageContent, and inlines text files. Also supports:
//   @*.go          — glob patterns (expand all matches, inline text files)
//   @session:<id>  — inject session message history as context
//   @skill:<name>  — inject skill content
// Tokens that don't resolve are left as-is in the text. Used
// by the TUI submit path to support @file references in chat input.
func extractImages(input string, store agent.StoreProvider, skills []agent.Skill) (prompt string, images []ai.ImageContent, err error) {
	prompt = input
	var loadedImages []ai.ImageContent

	prompt = atFilePattern.ReplaceAllStringFunc(input, func(token string) string {
		path := token[1:] // strip @

		// @skill:<name> — inject skill content.
		if strings.HasPrefix(path, "skill:") {
			skillName := path[6:]
			for _, s := range skills {
				if s.Name == skillName {
					return s.Content
				}
			}
			return token // skill not found, leave as-is
		}

		// @session:<id> — inject session message history.
		if strings.HasPrefix(path, "session:") && store != nil {
			sid := path[8:]
			msgs, sErr := store.GetMessages(context.Background(), sid)
			if sErr != nil || len(msgs) == 0 {
				return token // session not found, leave as-is
			}
			var sb strings.Builder
			for _, msg := range msgs {
				role := "unknown"
				switch msg.(type) {
				case ai.System:
					role = "system"
				case ai.User:
					role = "user"
				case ai.Assistant:
					role = "assistant"
				case ai.ToolResult:
					role = "tool"
				}
				sb.WriteString(fmt.Sprintf("[%s] %s\n", role, agent.MessageText(msg)))
			}
			return sb.String()
		}

		// Glob patterns (e.g. @*.go) — expand and inline text files.
		if strings.Contains(path, "*") || strings.Contains(path, "?") {
			matches, globErr := filepath.Glob(path)
			if globErr != nil || len(matches) == 0 {
				return token
			}
			var sb strings.Builder
			for _, m := range matches {
				data, readErr := os.ReadFile(m)
				if readErr != nil {
					continue
				}
				sb.WriteString(fmt.Sprintf("--- %s ---\n%s\n", m, string(data)))
			}
			if sb.Len() == 0 {
				return token
			}
			return sb.String()
		}

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
