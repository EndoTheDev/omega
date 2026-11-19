package agent

import (
	"github.com/EndoTheDev/omega/core"
)

// charsPerToken is the char-to-token ratio used by estimateTokens.
const charsPerToken = 4

// DefaultContextWindow is the nominal model context in tokens used when neither.
const DefaultContextWindow = 32768

const defaultReserveTokens = 16384

// CompactionConfig controls when the agent summarizes old messages to stay within.
type CompactionConfig struct {
	Enabled       bool    `yaml:"enabled"`
	Threshold     float64 `yaml:"threshold"`
	ContextWindow int     `yaml:"context_window"`
	KeepFirst     int     `yaml:"keep_first"`
	KeepLast      int     `yaml:"keep_last"`
	ReserveTokens int     `yaml:"reserve_tokens"`
	MaxToolOutput int     `yaml:"max_tool_output"`
	// Model is the per-subsystem model slot. The host routing (cmd/omega
	// compactionSection) copies it in from the top-level compaction_model knob - never a
	// separate yaml key.
	Model string `yaml:"-"`
}

// Budget returns the token count at which compaction triggers, computed against.
func (c CompactionConfig) Budget() int {
	return c.BudgetWith(c.ContextWindow)
}

// BudgetWith is Budget against a caller-resolved context window.
func (c CompactionConfig) BudgetWith(window int) int {
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
func EstimateTokens(history []core.Message) int {
	total := 0
	for _, m := range history {
		total += len(MessageText(m))
	}
	return total / charsPerToken
}

// MessageText returns the user-visible text of a message, used for token estimation.
func MessageText(m core.Message) string {
	switch v := m.(type) {
	case core.System:
		return v.Content
	case core.User:
		return v.Content
	case core.Assistant:
		return v.Content
	case core.ToolResult:
		return v.Content
	}
	return ""
}
