package agent

import (
	"context"
	"encoding/json"

	"github.com/EndoTheDev/omega/ai"
)

// CompactionProviderDispatcher routes compactor-seam JSON-RPC calls to
// the extension that declared the "compactor" seam.
type CompactionProviderDispatcher interface {
	CompactorRequest(ctx context.Context, method string, params map[string]any) (json.RawMessage, error)
}

// ProxyCompactionProvider forwards CompactionProvider methods to a
// compactor-seam extension via JSON-RPC. Messages are encoded as
// (role, payload) pairs - the same wire format used by the store. The
// compaction config is passed in each request so the extension owns
// the logic.
type ProxyCompactionProvider struct {
	Dispatcher CompactionProviderDispatcher
	Config     CompactionConfig
}

// Compact sends the message history and compaction config to the
// compactor extension. The extension owns the full compaction logic:
// threshold check, summarization, message assembly. Returns the
// compacted message list, or the original if no compaction was needed.
func (p *ProxyCompactionProvider) Compact(ctx context.Context, messages []ai.Message) ([]ai.Message, error) {
	msgJSON := messagesToJSON(messages)
	raw, err := p.Dispatcher.CompactorRequest(ctx, "compaction/compact", map[string]any{
		"messages": msgJSON,
		"config":   p.Config,
	})
	if err != nil {
		return nil, err
	}
	return decodeMessages(raw)
}