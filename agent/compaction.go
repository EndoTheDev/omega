package agent

import (
	"github.com/EndoTheDev/omega/core"
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
// config value (CompactionConfig.ContextWindow) when available. The
// 32768 floor rides the meter into agent/ (part 3, the seam
// constitution - the substrate keeps no compaction vocabulary).
const DefaultContextWindow = 32768

const defaultReserveTokens = 16384

// CompactionConfig controls when the agent summarizes old messages to
// stay within the model's context window. It lives in agent (the
// consumer's own package - the loop is the need; the compactor capsule
// imports the config type from here; harness -> agent is the legal
// import direction). The config is passed to the compactor plugin via
// Context; the plugin owns the compaction logic.
type CompactionConfig struct {
	Enabled       bool    `yaml:"enabled"`
	Threshold     float64 `yaml:"threshold"`
	ContextWindow int     `yaml:"context_window"`
	KeepFirst     int     `yaml:"keep_first"`
	KeepLast      int     `yaml:"keep_last"`
	ReserveTokens int     `yaml:"reserve_tokens"`
	MaxToolOutput int     `yaml:"max_tool_output"`
	// Model is the per-subsystem model slot (card t_fc0b2f2d):
	// compaction's consult rides "instance:model" or a bare model,
	// resolved through agent.ResolveModelSlot on every Compact call
	// (the model knob is hot, mirroring every other compaction
	// knob). The host routing (cmd/omega compactionSection) copies
	// it in from the top-level compaction_model knob - never a
	// separate yaml key.
	Model string `yaml:"-"`
}

// Budget returns the token count at which compaction triggers,
// computed against the manual context window override. 0 or fewer
// falls back to DefaultContextWindow.
func (c CompactionConfig) Budget() int {
	return c.BudgetWith(c.ContextWindow)
}

// BudgetWith is Budget against a caller-resolved context window: the
// compactor passes the window probed from the provider at startup
// when the config's context_window is 0 (= auto). A window of 0 or
// less falls back to DefaultContextWindow, unchanged.
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

// MessageText returns the user-visible text of a message, used for token
// estimation, summary rendering, and export.
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
