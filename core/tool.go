package core

import "context"

// ToolSchema describes a tool the model may call. It is passed to
// the provider and serialized into the API request.
type ToolSchema struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// Provider is the interface for LLM provider implementations.
// Stream returns a channel of stream events. Errors are encoded as
// StreamEnd(FinishReason="error", Error=...), not returned as Go
// errors. The channel is closed when the stream ends.
type Provider interface {
	Stream(ctx context.Context, messages []Message, tools []ToolSchema) <-chan ProviderStreamEvent
	ModelName() string
	SetThinkingLevel(level string)
	// WithModel returns a copy of the provider serving the given
	// model. The receiver is left unchanged, so callers derive
	// per-run providers without mutating shared state.
	WithModel(model string) Provider
	// WithThinkingLevel returns a copy of the provider streaming at
	// the given thinking level (the thinking-levels ladder values).
	// Same copy discipline as WithModel: the receiver is left
	// unchanged, so concurrent per-run derivations don't interfere.
	WithThinkingLevel(level string) Provider
	ListModels() ([]string, error)
	ModelInfo() (ModelInfo, error)
}

// ModelInfo holds metadata about the current model, queried from the
// provider when available. Zero values mean the provider does not
// expose that field; callers fall back to config defaults.
type ModelInfo struct {
	ContextWindow int // max context in tokens, 0 if unknown
}

// ModelEntry is one listed model with the provider's capability flags
// (card t_70c28128: the typed model pickers' data). Capabilities are
// nil when the provider's list endpoint does not carry them (the
// openai/anthropic id lists) - callers that need a slot filter fall
// back to a curated map for those.
type ModelEntry struct {
	Name         string   `json:"name"`
	Capabilities []string `json:"capabilities,omitempty"`
}

// ModelCapabilitiesProvider is an optional interface upgrade for
// Providers whose model-list endpoint carries capability flags
// (the ollama family's /api/tags "capabilities" array today).
// The core Provider interface stays name-only - a provider without
// capabilities is a full member, the upgrade is probed at runtime the
// same way hotApply probes the channel seams.
type ModelCapabilitiesProvider interface {
	ListModelEntries() ([]ModelEntry, error)
}
