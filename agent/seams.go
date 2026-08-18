package agent

import (
	"context"

	"github.com/EndoTheDev/omega/ai"
)

// PromptBuilder builds the system prompt for each agent run. The
// harness implements this to inject project context, skills,
// guidelines, and custom prompts. Extensions can provide alternative
// implementations to customize the prompt.
type PromptBuilder interface {
	// Build returns the system prompt string. Called once per Run
	// before the conversation loop starts.
	Build() string
}

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

// SessionStore abstracts session persistence. The harness implements
// this with the default SQLite store; alternative backends (JSONL,
// remote) can replace it.
type SessionStore interface {
	// AppendMessage appends a message to the session's history.
	AppendMessage(ctx context.Context, sessionID string, msg ai.Message) (int64, error)
	// GetMessages loads all messages for a session.
	GetMessages(ctx context.Context, sessionID string) ([]ai.Message, error)
	// Close releases resources.
	Close() error
}
