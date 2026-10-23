package core

// DefaultContextWindow is the nominal model context in tokens used when
// neither the provider nor config supplies a value. It is a rough
// fallback; the status bar and compaction budget prefer the auto-
// discovered value from the provider (ModelInfo.ContextWindow) or the
// config value (CompactionConfig.ContextWindow) when available.
const DefaultContextWindow = 8192

const defaultReserveTokens = 16384

// CompactionConfig controls when the agent summarizes old messages to
// stay within the model's context window. It lives in core (not the
// host) so the loop engines can consume it without importing a layer
// above themselves. The config is passed to the compactor plugin via
// Context; the plugin owns the compaction logic.
type CompactionConfig struct {
	Enabled       bool    `yaml:"enabled"`
	Threshold     float64 `yaml:"threshold"`
	ContextWindow int     `yaml:"context_window"`
	KeepFirst     int     `yaml:"keep_first"`
	KeepLast      int     `yaml:"keep_last"`
	ReserveTokens int     `yaml:"reserve_tokens"`
	MaxToolOutput int     `yaml:"max_tool_output"`
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