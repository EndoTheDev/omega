package agent

import (
	"context"

	"github.com/EndoTheDev/omega/ai"
)

// LoopProvider drives the multi-turn conversation. The default
// implementation (agent_loop.Loop in extensions/agent_loop/) runs the
// standard turn loop: stream provider responses, execute tool calls,
// feed results back. A custom implementation can replace the entire
// loop logic.
type LoopProvider interface {
	Run(ctx context.Context, opts LoopOptions) error
}

// LoopOptions carries everything the loop needs to run one agent
// session. The Agent struct builds this from its configured fields.
type LoopOptions struct {
	Provider           ai.Provider
	Messages           []ai.Message
	Tools              map[string]Tool
	ToolProvider       ToolProvider
	ToolProviders      []ToolProvider // additive tool sources (extensions)
	CompactionProvider CompactionProvider
	PromptBuilder      PromptBuilder           // builds system prompt + guidelines
	ExtensionInfos     []ExtensionInfo         // for prompt building context
	MaxTurns           int
	MaxToolOutput      int
	CWD                string
	PromptCustom       string   // user-supplied prompt from config, for extension-built prompts
	PromptAppend       []string // extra prompts from --append-system-prompt
	PromptContext      string   // trust-gated AGENTS.md project context
	Events             chan<- Event
	InjectedMessages   <-chan InjectedMessage // subagent results (nil if no delegate extension)
	UserInput          <-chan string           // user messages while running (nil for one-shot mode)
	PendingDelegations func() int             // returns count of running subagent tasks
}

// CompactionProvider handles context compaction when the token budget
// is exceeded. The compactor-seam extension implements this; when no
// compactor extension is loaded, compaction is disabled and the agent
// surfaces a friendly error on context overflow.
type CompactionProvider interface {
	// Compact compacts the message history. Returns the compacted
	// history, or the original if no compaction is needed. A nil
	// CompactionProvider means compaction is disabled.
	Compact(ctx context.Context, messages []ai.Message) ([]ai.Message, error)
}

// ToolProvider supplies tools to the agent. Called once per Run to
// build the tool set. Extension-provided tools are merged on top.
type ToolProvider interface {
	Tools() map[string]Tool
}

// DefaultToolProvider wraps a static tool map. Extension tools are
// merged by the agent on top of these.
type DefaultToolProvider struct {
	ToolsMap map[string]Tool
}

// Tools returns the tool map.
func (d DefaultToolProvider) Tools() map[string]Tool { return d.ToolsMap }

// StoreProvider is the session persistence seam. The default
// implementation is SQLite (gateway.Store), provided via the store
// plugin. All session and message operations go through this interface.
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

// SkillsProvider is the skill loading seam. The default implementation
// scans a directory for <name>/<name>.md files, provided via the skills
// plugin. The host uses this to populate the skill list for autocomplete,
// inline invocation, and @skill: mentions.
type SkillsProvider interface {
	LoadSkills(dir string) ([]Skill, error)
}

// PromptBuilder builds the system prompt and supplies guideline
// lines appended to it. The default implementation is provided via
// the prompt extension. When no prompt extension is loaded, both
// methods return zero values.
type PromptBuilder interface {
	// BuildPrompt assembles the full system prompt. Returns ok=false
	// if the builder does not want to provide a prompt; the agent gets
	// no system prompt in that case.
	BuildPrompt(ctx context.Context, opts PromptBuildOptions) (string, bool)
	// Guidelines returns extra lines appended under
	// "## Extension Guidelines" in the system prompt.
	Guidelines() []string
}