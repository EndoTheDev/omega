package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/engine/agent"
	"github.com/EndoTheDev/omega/engine/ai"
)

// atFilePattern matches @<non-space> tokens in a text string.
var atFilePattern = regexp.MustCompile(`@\S+`)

// Resolution outcome of one @token.
const (
	atResolved = iota // token consumed (image attached, file inlined, session/skill injected)
	atLiteral         // token did not resolve; it stays in the text as-is
	atFailed          // resolution was attempted but hit an I/O error (also stays as-is)
)

// extractImages scans a text string for @path tokens, loads any image
// files as base64 ImageContent, and inlines text files. Also supports:
//
//	@*.go          — glob patterns (expand all matches, inline text files)
//	@session:<id>  — inject session message history as context
//	@skill:<name>  — inject skill content
//
// Tokens that don't resolve are left as-is in the text (cwd-relative;
// unresolvable passthrough is the protocol, never a send-blocker) and
// reported in `unresolved` so the caller can surface them. Only a
// standalone @ (at input start or after a space) counts as unresolved;
// a mid-word @ is an email address or handle, not a file reference.
// Used by the TUI submit path to support @file references in chat input.
func extractImages(input string, store core.StoreProvider, skills []core.Skill) (prompt string, images []core.ImageContent, unresolved []string, err error) {
	var b strings.Builder
	var loadedImages []core.ImageContent
	last := 0
	for _, loc := range atFilePattern.FindAllStringSubmatchIndex(input, -1) {
		start, end := loc[0], loc[1]
		b.WriteString(input[last:start])
		token := input[start:end]
		standalone := start == 0 || input[start-1] == ' '
		replacement, img, outcome, resErr := resolveAtToken(token, store, skills)
		if resErr != nil && err == nil {
			err = resErr
		}
		if img != nil {
			loadedImages = append(loadedImages, *img)
		}
		b.WriteString(replacement)
		if standalone && outcome == atLiteral {
			unresolved = append(unresolved, token)
		}
		last = end
	}
	b.WriteString(input[last:])
	prompt = b.String()
	images = loadedImages
	return prompt, images, unresolved, err
}

// resolveAtToken resolves one @token against the cwd. Returns the
// replacement text, the loaded image (nil for text injections) and the
// outcome: atResolved (token replaced), atLiteral (unresolvable, sent
// as-is) or atFailed (attempted, I/O error; also sent as-is but the
// error is reported to the caller).
func resolveAtToken(token string, store core.StoreProvider, skills []core.Skill) (string, *core.ImageContent, int, error) {
	path := token[1:] // strip @

	// @skill:<name> — inject skill content.
	if strings.HasPrefix(path, "skill:") {
		skillName := path[6:]
		for _, s := range skills {
			if s.Name == skillName {
				return s.Content, nil, atResolved, nil
			}
		}
		return token, nil, atLiteral, nil // skill not found, sent as-is
	}

	// @session:<id> — inject session message history (the ACTIVE
	// path, t_17365247: a branched session injects its selected
	// branch, never the parked variants).
	if strings.HasPrefix(path, "session:") && store != nil {
		sid := path[8:]
		msgs, sErr := store.GetActiveMessages(context.Background(), sid)
		if sErr != nil || len(msgs) == 0 {
			return token, nil, atLiteral, nil // session not found, sent as-is
		}
		var sb strings.Builder
		for _, msg := range msgs {
			role := core.MessageRole(msg)
			sb.WriteString(fmt.Sprintf("[%s] %s\n", role, agent.MessageText(msg)))
		}
		return sb.String(), nil, atResolved, nil
	}

	// Glob patterns (e.g. @*.go) — expand and inline text files.
	if strings.Contains(path, "*") || strings.Contains(path, "?") {
		matches, globErr := filepath.Glob(path)
		if globErr != nil || len(matches) == 0 {
			return token, nil, atLiteral, nil
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
			return token, nil, atLiteral, nil
		}
		return sb.String(), nil, atResolved, nil
	}

	// If the file doesn't exist, leave the token as-is.
	info, statErr := os.Stat(path)
	if statErr != nil || info.IsDir() {
		return token, nil, atLiteral, nil
	}

	// Try image detection.
	img, detectErr := ai.DetectImage(path)
	if detectErr != nil {
		return token, nil, atFailed, detectErr
	}
	if img != nil {
		return "[" + img.MediaType + ": " + path + "]", img, atResolved, nil
	}

	// Not an image: inline the file contents.
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		return token, nil, atFailed, readErr
	}
	return string(data), nil, atResolved, nil
}