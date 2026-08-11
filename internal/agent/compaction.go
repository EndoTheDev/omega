package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/EndoTheDev/omega-dev/internal/ai"
)

// charsPerToken is the char-to-token ratio used by estimateTokens.
// ponytail: 4 chars/token is a rough average across models; good enough
// to trigger compaction. Upgrade path: use the provider's real tokenizer
// when one is exposed.
const charsPerToken = 4

// defaultContextWindow is the nominal model context in tokens used to
// turn the compaction threshold fraction into an absolute budget.
// ponytail: fixed constant, not per-model. Upgrade path: query the
// provider for its real context window.
const defaultContextWindow = 8192

// CompactionConfig controls when the agent summarizes old messages to
// stay within the model's context window. It lives in the agent package
// (not gateway) so the agent can consume it without importing a layer
// above itself.
type CompactionConfig struct {
	Enabled       bool    `yaml:"enabled"`
	Threshold     float64 `yaml:"threshold"`
	ContextWindow int     `yaml:"context_window"`
	KeepFirst     int     `yaml:"keep_first"`
	KeepLast      int     `yaml:"keep_last"`
}

// budget returns the token count at which compaction triggers.
func (c CompactionConfig) budget() int {
	window := c.ContextWindow
	if window <= 0 {
		window = defaultContextWindow
	}
	return int(float64(window) * c.Threshold)
}

// EstimateTokens returns a rough token count for the message history.
func EstimateTokens(history []ai.Message) int {
	total := 0
	for _, m := range history {
		total += len(messageText(m))
	}
	return total / charsPerToken
}

// messageText returns the user-visible text of a message, used for token
// estimation and summary rendering.
func messageText(m ai.Message) string {
	switch v := m.(type) {
	case ai.System:
		return v.Content
	case ai.User:
		return v.Content
	case ai.Assistant:
		return v.Content
	case ai.ToolResult:
		return v.Content
	}
	return ""
}

// compact summarizes the middle of history and replaces it with a single
// system message. The first keepFirst and last keepLast messages are
// preserved verbatim. If there is nothing to compact, history is
// returned unchanged.
func compact(ctx context.Context, provider ai.Provider, history []ai.Message, keepFirst, keepLast int) ([]ai.Message, error) {
	if keepFirst+keepLast >= len(history) {
		return history, nil
	}
	middle := history[keepFirst : len(history)-keepLast]
	summary, err := summarize(ctx, provider, middle)
	if err != nil {
		return nil, err
	}
	result := make([]ai.Message, 0, keepFirst+keepLast+1)
	result = append(result, history[:keepFirst]...)
	result = append(result, ai.NewSystem("[compacted: "+summary+"]"))
	result = append(result, history[len(history)-keepLast:]...)
	return result, nil
}

// summarize asks the provider to condense the given messages into a
// short summary preserving key facts and decisions.
func summarize(ctx context.Context, provider ai.Provider, messages []ai.Message) (string, error) {
	return summarizeWithFocus(ctx, provider, messages, "")
}

// summarizeWithFocus is like summarize but prepends a focus instruction
// to the prompt when focus is non-empty.
func summarizeWithFocus(ctx context.Context, provider ai.Provider, messages []ai.Message, focus string) (string, error) {
	var b strings.Builder
	b.WriteString("Summarize the following conversation concisely, preserving key facts, decisions, and context.")
	if focus != "" {
		b.WriteString(" ")
		b.WriteString(focus)
		b.WriteString(".")
	}
	b.WriteString(" Output only the summary.\n\n")
	for _, m := range messages {
		b.WriteString("- ")
		b.WriteString(messageText(m))
		b.WriteString("\n")
	}

	prompt := []ai.Message{ai.NewUser(b.String())}
	var summary strings.Builder
	for event := range provider.Stream(ctx, prompt, nil) {
		switch e := event.(type) {
		case ai.ResponseChunk:
			summary.WriteString(e.Content)
		case ai.StreamEnd:
			if e.FinishReason == "error" {
				return "", fmt.Errorf("summarize: %s", e.Error)
			}
		}
	}
	if summary.Len() == 0 {
		return "", fmt.Errorf("summarize: empty summary")
	}
	return strings.TrimSpace(summary.String()), nil
}

// CompactWithFocus summarizes the middle of history with an optional
// focus instruction, replacing it with a single system message. The
// first keepFirst and last keepLast messages are preserved verbatim.
// If there is nothing to compact, history is returned unchanged.
func CompactWithFocus(ctx context.Context, provider ai.Provider, history []ai.Message, keepFirst, keepLast int, focus string) ([]ai.Message, error) {
	if keepFirst+keepLast >= len(history) {
		return history, nil
	}
	middle := history[keepFirst : len(history)-keepLast]
	summary, err := summarizeWithFocus(ctx, provider, middle, focus)
	if err != nil {
		return nil, err
	}
	result := make([]ai.Message, 0, keepFirst+keepLast+1)
	result = append(result, history[:keepFirst]...)
	result = append(result, ai.NewSystem("[compacted: "+summary+"]"))
	result = append(result, history[len(history)-keepLast:]...)
	return result, nil
}
