package store

// Config configures the session store extension.
type Config struct {
	DBPath string `yaml:"db_path"`
}

// Default returns the default store configuration.
func Default() Config {
	return Config{DBPath: "omega.db"}
}

// HygieneDefaults are the default retention and summarizer windows for
// the hygiene pass (card t_329b9312).
const (
	// DefaultRetentionDays is the retention window: sessions older
	// than this are pruned by the hygiene job. Hermes' number (the
	// reference agent), 90d.
	DefaultRetentionDays = 90

	// DefaultSummarizeAfterHours is how old a session must be before
	// the summarizer writes one. The pass only ever reads SETTLED
	// conversations - a summary of a session that is still being
	// written would be stale on arrival.
	DefaultSummarizeAfterHours = 24
)

// HygieneConfig configures the sessions hygiene pass (card t_329b9312):
// the LLM session summarizer and the retention window. The `sessions:`
// config.yaml section; routed into the store capsule at boot
// (ctx.Configs), so changes apply on restart.
type HygieneConfig struct {
	// RetentionDays is the session retention window. Sessions older
	// than this (by updated_at) are deleted by the hygiene pass.
	// 0 = never prune.
	RetentionDays int `yaml:"retention_days"`

	// SummarizeAfterHours is the minimum session age before the
	// summarizer writes one. The pass only ever reads SETTLED
	// conversations - a summary of a session that is still being
	// written would be stale on arrival.
	SummarizeAfterHours int `yaml:"summarize_after_hours"`

	// SummarizerModel is the model the summarizer pass consults. Empty
	// = the session provider's own model (Endo's ruling: default A,
	// configurable in settings - the same law as the curator model).
	SummarizerModel string `yaml:"summarizer_model"`
}

// DefaultHygiene returns the default hygiene configuration.
func DefaultHygiene() HygieneConfig {
	return HygieneConfig{
		RetentionDays:       DefaultRetentionDays,
		SummarizeAfterHours: DefaultSummarizeAfterHours,
	}
}
