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
	Logger             LoggerProvider         // optional logger; nil = no logging
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

// LoggerProvider is the logging seam. Extensions call it to write
// log entries. The default implementation writes to a file in
// OMEGA_HOME; a no-op implementation is used when logging is disabled.
type LoggerProvider interface {
	// Printf writes an info-level log entry.
	Printf(format string, args ...any)
	// Errorf writes an error-level log entry.
	Errorf(format string, args ...any)
	// Close flushes and closes the log output.
	Close() error
}

// MemoryProvider manages persistent memory across sessions.
// The prompt builder calls Snapshot to inject current memory into
// the system prompt. The memory tool calls Add/Replace/Remove to
// mutate entries during a session.
type MemoryProvider interface {
	// Snapshot returns the formatted prompt block for system prompt
	// injection. Reads files fresh on each call.
	Snapshot() string

	// Add appends a new entry to the target store ("memory" or "user").
	// Returns the new usage string and an error if the entry would
	// exceed the char limit or is an exact duplicate.
	Add(target, content string) (usage string, err error)

	// Replace finds the entry matching oldText (unique substring) and
	// replaces it with content. Returns usage string and error.
	Replace(target, oldText, content string) (usage string, err error)

	// Remove finds the entry matching oldText (unique substring) and
	// deletes it. Returns usage string and error.
	Remove(target, oldText string) (usage string, err error)

	// List returns all entries for the target.
	List(target string) ([]string, error)
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

// Channel is a delivery transport that connects external clients to
// the agent. Channels are additive: multiple can run simultaneously
// (HTTP + Discord + Telegram). The host starts all mounted channels
// after MountAll and stops them on shutdown.
//
// Start blocks until the channel stops or the context is cancelled.
// Each channel creates agents from deps.Ctx as needed — HTTP reuses
// one agent; a chat-platform channel may create one per conversation.
type Channel interface {
	// Start runs the channel until the context is cancelled or Stop
	// is called. Blocks.
	Start(ctx context.Context, deps ChannelDeps) error
	// Stop shuts the channel down. Called by the host after the
	// context is cancelled.
	Stop() error
}

// ChannelDeps carries everything a channel needs to run. Ctx is the
// fully-wired plugin Context — channels create agents from it. Store
// persists messages. Config is the host config (gateway.Config) for
// channels to type-assert their settings from.
type ChannelDeps struct {
	Ctx    *Context
	Store  StoreProvider
	Config any
}

// Frontend is the user-facing interface seam. The default implementation
// is the terminal TUI (extensions/tui/). A custom implementation could
// serve a web UI or any other interactive frontend. The host calls Run
// after mounting all extensions; Run blocks until the frontend exits.
type Frontend interface {
	Run(ctx context.Context, pctx *Context, opts FrontendOptions) error
}

// FrontendOptions carries host-level settings the frontend needs that
// are not on the Context itself. All fields are optional zero-values.
type FrontendOptions struct {
	ModelName     string
	ProviderType  string
	Compaction    *CompactionConfig
	PromptCustom  string
	PromptAppend  []string
	PromptContext string
	Skills        []Skill
	ThemeName     string
	TrustState    string
	Notifications string
	CWD           string
	Version       string
}
