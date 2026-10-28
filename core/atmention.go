package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// AtMentionEnv carries the seams the @-mention expansion reaches for.
// Core is stdlib-only by law, so the pieces it cannot own ride in as
// functions the caller supplies - one expansion law, two callers (the
// TUI submit path and the webui submit path). A nil seam degrades the
// exact way it always has: @file unresolvable, @session left as-is.
type AtMentionEnv struct {
	// Store backs @session:<id> history injection. Nil leaves those
	// tokens untouched.
	Store StoreProvider
	// Skills backs @skill:<name> content injection. Nil/empty leaves
	// those tokens untouched.
	Skills []Skill
	// DetectImage decides file-vs-image (the ai package owns the magic
	// byte sniffing). Nil leaves file tokens untouched.
	DetectImage func(path string) (*ImageContent, error)
	// MessageText renders a stored message's user-visible text for
	// @session injection (the agent package owns it).
	MessageText func(Message) string
	// Dir is the base for relative file paths and globs. Empty means
	// the process working directory - the TUI's historical behavior.
	Dir string
}

// AtFilePattern matches @<non-space> tokens in a text string.
var AtFilePattern = regexp.MustCompile(`@\S+`)

// atMentionResolve roots a relative path (or glob pattern) at the env's
// Dir. Absolute paths pass through; an empty Dir passes through too -
// the expansion then behaves exactly like the process-cwd TUI did.
func atMentionResolve(dir, path string) string {
	if dir == "" || filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(dir, path)
}

// ExpandAtMentions scans a text string for @path tokens, loads any
// image files as base64 ImageContent, and inlines text files. Also
// supports:
//
//	@*.go          - glob patterns (expand all matches, inline text files)
//	@session:<id>  - inject session message history as context
//	@skill:<name>  - inject skill content
//
// Relative paths resolve against env.Dir. Tokens that don't resolve
// are left as-is in the text. Both submit paths (TUI and webui) run
// this on their own thread; there is exactly one copy of the law.
func ExpandAtMentions(input string, env AtMentionEnv) (prompt string, images []ImageContent, err error) {
	prompt = input
	var loadedImages []ImageContent

	prompt = AtFilePattern.ReplaceAllStringFunc(input, func(token string) string {
		path := token[1:] // strip @

		// @skill:<name> - inject skill content.
		if strings.HasPrefix(path, "skill:") {
			skillName := path[6:]
			for _, s := range env.Skills {
				if s.Name == skillName {
					return s.Content
				}
			}
			return token // skill not found, leave as-is
		}

		// @session:<id> - inject session message history (the ACTIVE
		// path, t_17365247: a branched session injects its selected
		// branch, never the parked variants).
		if strings.HasPrefix(path, "session:") && env.Store != nil {
			sid := path[8:]
			msgs, sErr := env.Store.GetActiveMessages(context.Background(), sid)
			if sErr != nil || len(msgs) == 0 {
				return token // session not found, leave as-is
			}
			var sb strings.Builder
			for _, msg := range msgs {
				role := MessageRole(msg)
				sb.WriteString(fmt.Sprintf("[%s] %s\n", role, env.MessageText(msg)))
			}
			return sb.String()
		}

		resolved := atMentionResolve(env.Dir, path)

		// Glob patterns (e.g. @*.go) - expand and inline text files.
		if strings.Contains(path, "*") || strings.Contains(path, "?") {
			matches, globErr := filepath.Glob(resolved)
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
		info, statErr := os.Stat(resolved)
		if statErr != nil || info.IsDir() {
			return token
		}

		// Try image detection.
		if env.DetectImage != nil {
			img, detectErr := env.DetectImage(resolved)
			if detectErr != nil {
				err = detectErr
				return token
			}
			if img != nil {
				loadedImages = append(loadedImages, *img)
				return "[" + img.MediaType + ": " + path + "]"
			}
		}

		// Not an image: inline the file contents.
		data, readErr := os.ReadFile(resolved)
		if readErr != nil {
			err = readErr
			return token
		}
		return string(data)
	})

	return prompt, loadedImages, err
}