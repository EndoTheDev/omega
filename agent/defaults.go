package agent

import (
	"context"

	"github.com/EndoTheDev/omega/ai"
)

// DefaultCompactor wraps the existing CompactWithFocus logic with
// config and extension hooks. It is the default Compactor — the
// host wires it with the provider, compaction config, and
// extension manager. Extensions that want custom compaction implement
// their own Compactor.
type DefaultCompactor struct {
	Provider   ai.Provider
	Config     *CompactionConfig
	Extensions ExtensionManager
}

// Compact implements the Compactor interface. If the token estimate is
// within budget, history is returned unchanged. If an extension
// provides a custom summary via CustomizeCompaction, it is used
// instead of the provider-based summarization.
func (d DefaultCompactor) Compact(ctx context.Context, messages []ai.Message) ([]ai.Message, error) {
	if d.Config == nil || !d.Config.Enabled {
		return messages, nil
	}
	if EstimateTokens(messages) <= d.Config.budget() {
		return messages, nil
	}
	if d.Config.KeepFirst+d.Config.KeepLast >= len(messages) {
		return messages, nil
	}
	// Check extensions for full compaction first.
	if d.Extensions != nil {
		if compacted, ok := d.Extensions.CompactMessages(ctx, messages); ok {
			return compacted, nil
		}
		// Check extensions for a custom summary.
		if summary, ok := d.Extensions.CustomizeCompaction(ctx, messages); ok {
			return BuildCompactedMessages(messages, summary, d.Config.KeepFirst, d.Config.KeepLast), nil
		}
	}
	return CompactWithFocus(ctx, d.Provider, messages, d.Config.KeepFirst, d.Config.KeepLast, "")
}

// DefaultToolProvider wraps a static tool map. Extension tools are
// merged by the agent on top of these.
type DefaultToolProvider struct {
	ToolsMap map[string]Tool
}

// Tools returns the tool map.
func (d DefaultToolProvider) Tools() map[string]Tool { return d.ToolsMap }
