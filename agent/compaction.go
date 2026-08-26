package agent

import (
	"github.com/EndoTheDev/omega/ai"
)

// charsPerToken is the char-to-token ratio used by estimateTokens.
// ponytail: 4 chars/token is a rough average across models; good enough
// to trigger compaction. Upgrade path: use the provider's real tokenizer
// when one is exposed.
const charsPerToken = 4

// DefaultContextWindow is the nominal model context in tokens used when
// neither the provider nor config supplies a value. It is a rough
// fallback; the status bar and compaction budget prefer the auto-
// discovered value from the provider (ModelInfo.ContextWindow) or the
// config value (CompactionConfig.ContextWindow) when available.
const DefaultContextWindow = 8192
const defaultReserveTokens = 16384

// CompactionConfig controls when the agent summarizes old messages to
// stay within the model's context window. It lives in the agent package
// (not gateway) so the agent can consume it without importing a layer
// above itself. The config is passed to the compactor plugin via Context;
// the plugin owns the compaction logic.
type CompactionConfig struct {
	Enabled       bool    `yaml:"enabled"`
	Threshold     float64 `yaml:"threshold"`
	ContextWindow int     `yaml:"context_window"`
	KeepFirst     int     `yaml:"keep_first"`
	KeepLast      int     `yaml:"keep_last"`
	ReserveTokens int     `yaml:"reserve_tokens"`
	MaxToolOutput int     `yaml:"max_tool_output"`
}

// Budget returns the token count at which compaction triggers.
// ReserveTokens are subtracted from the context window so the model
// has room for its response after the prompt.
func (c CompactionConfig) Budget() int {
	window := c.ContextWindow
	if window <= 0 {
		window = DefaultContextWindow
	}
	reserve := c.ReserveTokens
	if reserve <= 0 {
		reserve = defaultReserveTokens
	}
	effective := window - reserve
	if effective < window/2 {
		effective = window / 2 // never let reserve eat more than half
	}
	return int(float64(effective) * c.Threshold)
}

// EstimateTokens returns a rough token count for the message history.
func EstimateTokens(history []ai.Message) int {
	total := 0
	for _, m := range history {
		total += len(MessageText(m))
	}
	return total / charsPerToken
}

// MessageText returns the user-visible text of a message, used for token
// estimation, summary rendering, and export.
func MessageText(m ai.Message) string {
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

// BuildCompactedMessages assembles the compacted message list from a
// pre-computed summary. Shared by the branch summary feature and the
// compactor plugin.
func BuildCompactedMessages(history []ai.Message, summary string, keepFirst, keepLast int) []ai.Message {
	result := make([]ai.Message, 0, keepFirst+keepLast+1)
	result = append(result, history[:keepFirst]...)
	result = append(result, ai.NewSystem("[compacted: "+summary+"]"))
	result = append(result, history[len(history)-keepLast:]...)
	return result
}
