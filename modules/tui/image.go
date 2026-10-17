package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/ai"
)

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
				role := ai.MessageRole(msg)
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
		img, detectErr := ai.DetectImage(path)
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
