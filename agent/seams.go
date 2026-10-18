package agent

import (
	"context"

	"github.com/EndoTheDev/omega/ai"
)

// LoopProvider drives the multi-turn conversation. The default
// implementation (agent_loop.Loop in modules/agent_loop/) runs the
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

// DefaultToolProvider wraps a static tool map. Used by tests to
// satisfy the ToolProvider interface with a simple map.
type DefaultToolProvider struct {
	ToolsMap map[string]Tool
}

// Tools returns the tool map.
func (d DefaultToolProvider) Tools() map[string]Tool { return d.ToolsMap }

// StoreProvider is the session persistence seam. The default
// implementation is SQLite (store.Store in modules/store/), provided via the store
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

	// GetMessagesWithTimes returns a session's messages in append
	// order, each paired with its store-level created_at.
	GetMessagesWithTimes(ctx context.Context, sessionID string) ([]StoredMessage, error)

	GetSessionTree(ctx context.Context) ([]*SessionNode, error)
	GetAncestorMessages(ctx context.Context, sessionID string) ([]ai.Message, error)
	SearchMessages(ctx context.Context, query string) ([]SearchResult, error)
	ComputeInsights(ctx context.Context, days int) (*Insights, error)
	CountMessages(ctx context.Context, sessionID string) (int, error)

	// SessionCounts computes per-session message and tool-result counts
	// at read time (SQL COUNT, no maintained counters). Sessions with
	// no messages are absent from the map.
	SessionCounts(ctx context.Context) (map[string]SessionCounts, error)

	// AddSessionTokens adds a turn's real provider-reported usage to the
	// session's accumulated token totals. Zero usage is a no-op.
	AddSessionTokens(ctx context.Context, sessionID string, prompt, completion int) error

	// SetSessionLastRun writes the last-run stats block (or clears it
	// with nil) on the session's row, so the webui status bar can show
	// the last-known rates after a reload - no stream required.
	SetSessionLastRun(ctx context.Context, sessionID string, stats *LastRunStats) error

	// SetSessionPinned sets the pinned flag on a session. It is a
	// user-list concern and does not touch updated_at.
	SetSessionPinned(ctx context.Context, sessionID string, pinned bool) error

	// SetSessionRead stamps a session's last-read time so unread
	// clears. It is a read concern and does not touch updated_at.
	SetSessionRead(ctx context.Context, sessionID string) error

	// SetSessionTitle writes a generated label together with its title
	// state transition in one update. provisional (final=false) is
	// allowed only from title state 0 and moves to 1; final (final=true)
	// is allowed from 0 or 1 and moves to 2. A session already at 2 -
	// including every pre-title-state row that carried a label - is
	// never touched. The state machine is enforced here.
	SetSessionTitle(ctx context.Context, sessionID string, title string, final bool) error

	// SessionModels returns the last recorded model per session,
	// derived from the session's model_change entries at read time.
	// Sessions with no model entry are absent from the map.
	SessionModels(ctx context.Context) (map[string]string, error)
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

// PromptSectioner is the additive prompt-section seam. Modules that
// own a piece of the system prompt (memory snapshot, skills list,
// tools list) contribute it as a PromptSection; the prompt builder is
// a generic assembler that renders whatever is contributed. Section
// layout order: memory 10, skills 20, tools 30 - new sections pick a
// value between them.
type PromptSectioner interface {
	// PromptSections returns the sections this module contributes for
	// this build. Returns nil when it has nothing to render.
	PromptSections(opts PromptBuildOptions) []PromptSection
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

// SettingsKnob is one exposed config field. Type drives the webui
// control (toggle, number, text, secret, select, textarea, bindlist).
// Live marks knobs the hot-apply path applies without a restart.
// Surface declares who can USE the knob: "" = every surface, "tui" =
// terminal app only - each serving surface filters to its own.
type SettingsKnob struct {
	Key     string   `json:"key"`
	Label   string   `json:"label"`
	Type    string   `json:"type"`
	Value   any      `json:"value"`
	Options []string `json:"options,omitempty"`
	Min     *float64 `json:"min,omitempty"`
	Max     *float64 `json:"max,omitempty"`
	Step    *float64 `json:"step,omitempty"`
	Help    string   `json:"help,omitempty"`
	Live    bool     `json:"live,omitempty"`
	Surface string   `json:"surface,omitempty"`
}

// SettingsGroup is one config section: a labeled set of knobs.
type SettingsGroup struct {
	Key   string         `json:"key"`
	Label string         `json:"label"`
	Knobs []SettingsKnob `json:"knobs"`
}

// SettingsSchema is the JSON-marshalable settings payload: knob groups
// in config-file order with current values. Mirrors the wire shape the
// webui settings pane already consumes.
type SettingsSchema struct {
	Groups []SettingsGroup `json:"groups"`
}

// Settings is the runtime config knobs seam. Channels that host a UI
// (the webui settings pane lives behind the HTTP channel's /settings)
// use it to read the host's config knobs and save changes - the host
// owns the schema, validation, persistence, and hot-apply; the channel
// never touches config.yaml itself. Nil when the host does not expose
// settings (endpoints then answer 501).
type Settings interface {
	// Schema returns the settings payload: knob groups, each knob's
	// label/type/controls, and current values. Secret values MUST be
	// masked - the plain text never rides the wire.
	Schema() (*SettingsSchema, error)
	// Save applies values (map of dotted yaml path -> JSON value),
	// validates, persists the config file, hot-applies what the
	// running process can apply, and returns the refreshed payload.
	Save(values map[string]any) (*SettingsSchema, error)
}

// ChannelDeps carries everything a channel needs to run. Ctx is the
// fully-wired plugin Context — channels create agents from it. Store
// persists messages. Settings (optional) exposes the config knobs.
// Version is the host's build tag, surfaced through /health.
type ChannelDeps struct {
	Ctx      *Context
	Store    StoreProvider
	Settings Settings
	Version  string
}

// CronProvider schedules omega prompts on cron expressions. The
// default implementation is the cron extension (modules/cron/),
// backed by crons.yaml in OMEGA_HOME. Each fire is one full agent
// turn in a session labeled cron:<name>.
//
// Like Channel, Start blocks until the context is cancelled. The
// serve host starts the mounted Cron after MountAll (with the store
// open) and stops it on shutdown.
type CronProvider interface {
	// Start runs the scheduler until the context is cancelled or Stop
	// is called. Blocks.
	Start(ctx context.Context, deps CronDeps) error
	// Stop shuts the scheduler down. Called by the host on shutdown.
	Stop() error
}

// CronDeps carries everything the cron runner needs. Ctx is the
// fully-wired plugin Context (Memory for the memory deliver target,
// Logger for run logs), Agent runs each fire as a full agent turn,
// Store persists the cron:<name> sessions.
type CronDeps struct {
	Ctx   *Context
	Agent *Agent
	Store StoreProvider
}

// Frontend is the user-facing interface seam. The default implementation
// is the terminal TUI (modules/tui/). A custom implementation could
// serve a web UI or any other interactive frontend. The host calls Run
// after mounting all extensions; Run blocks until the frontend exits.
type Frontend interface {
	Run(ctx context.Context, pctx *Context, opts FrontendOptions) error
}

// TrustProvider gates project context (AGENTS.md) behind a trust
// check. The default implementation reads trust.yaml from omega home.
// The host calls ResolveContext before building the agent; State is
// used for the TUI status bar.
type TrustProvider interface {
	ResolveContext(cwd string, approve, noApprove, interactive bool) string
	State(cwd string, approve, noApprove bool) string
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
