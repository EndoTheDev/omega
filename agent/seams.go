package agent

import (
	"context"

	"github.com/EndoTheDev/omega/ai"
)

// Compactor handles context compaction when the token budget is
// exceeded. The harness implements this with the default provider-based
// summarization; extensions can override with custom summaries.
type Compactor interface {
	// Compact compacts the message history. Returns the compacted
	// history, or the original if no compaction is needed. A nil
	// Compactor means compaction is disabled.
	Compact(ctx context.Context, messages []ai.Message) ([]ai.Message, error)
}

// ToolProvider supplies tools to the agent. Called once per Run to
// build the tool set. Extension-provided tools are merged on top.
type ToolProvider interface {
	Tools() map[string]Tool
}

// StoreProvider is the session persistence seam. The default
// implementation is SQLite (gateway.Store), provided via the
// core-store extension. All session and message operations go
// through this interface.
type StoreProvider interface {
	Open(dsn string) error
	Close() error
	CreateSession(ctx context.Context, id, parentID, label string) error
	GetSession(ctx context.Context, id string) (Session, error)
	ListSessions(ctx context.Context) ([]Session, error)
	DeleteSession(ctx context.Context, id string) error
	UpdateSession(ctx context.Context, id, label string) error
	AppendMessage(ctx context.Context, sessionID string, msg ai.Message) error
	GetMessages(ctx context.Context, sessionID string) ([]ai.Message, error)
	GetSessionTree(ctx context.Context) ([]*SessionNode, error)
	GetAncestorMessages(ctx context.Context, sessionID string) ([]ai.Message, error)
	SearchMessages(ctx context.Context, query string) ([]SearchResult, error)
	ComputeInsights(ctx context.Context, days int) (*Insights, error)
	CountMessages(ctx context.Context, sessionID string) (int, error)
}
