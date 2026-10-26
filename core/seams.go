package core

import (
	"context"
	"time"
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
	Provider           Provider
	Messages           []Message
	Tools              map[string]Tool
	ToolProviders      []ToolProvider // additive tool sources (extensions)
	CompactionProvider CompactionProvider
	PromptBuilder      PromptBuilder   // builds system prompt + guidelines
	ExtensionInfos     []ExtensionInfo // for prompt building context
	MaxTurns           int
	MaxToolOutput      int
	CWD                string
	PromptCustom       string   // user-supplied prompt from config, for extension-built prompts
	PromptAppend       []string // extra prompts from --append-system-prompt
	PromptContext      string   // trust-gated AGENTS.md project context
	Events             chan<- Event
	InjectedMessages   <-chan InjectedMessage // subagent results (nil if no delegate extension)
	UserInput          <-chan string          // user messages while running (nil for one-shot mode)
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
	Compact(ctx context.Context, messages []Message) ([]Message, error)
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
	AppendMessage(ctx context.Context, sessionID string, msg Message) error
	GetMessages(ctx context.Context, sessionID string) ([]Message, error)

	// GetMessagesWithTimes returns a session's messages in append
	// order, each paired with its row id (the tree identity) and
	// store-level created_at.
	GetMessagesWithTimes(ctx context.Context, sessionID string) ([]StoredMessage, error)

	// GetActiveView returns the session's tree-derived active view
	// (t_17365247): the active path for context assembly + transcript
	// rendering, plus the branch-point metadata the variant
	// navigation renders from. Sessions persisted without parent
	// pointers derive the same single linear path they always had.
	GetActiveView(ctx context.Context, sessionID string) (ActiveView, error)

	// GetActiveMessages returns the active path as plain messages -
	// the session-load seam (TUI resume, exports).
	GetActiveMessages(ctx context.Context, sessionID string) ([]Message, error)

	GetSessionTree(ctx context.Context) ([]*SessionNode, error)
	GetAncestorMessages(ctx context.Context, sessionID string) ([]Message, error)
	SearchMessages(ctx context.Context, query string) ([]SearchResult, error)

	// SearchSessions is the unified search behind /sessions/search:
	// one phrase-quoted term queries both session labels and message
	// content, merged to one hit per session. Label matches come
	// first (Title set, Snippet = label), then content-only sessions
	// by best rank.
	SearchSessions(ctx context.Context, query string) ([]SearchResult, error)
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

	// SetSessionArchived sets the archived flag on a session. It is a
	// user-list concern and does not touch updated_at.
	SetSessionArchived(ctx context.Context, sessionID string, archived bool) error

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

// AgentSessionStore is the optional agent-scoped session surface
// (card t_7e2b7002). Sessions are bound to an agent at creation and
// never switch agents mid-conversation. The core StoreProvider stays
// untouched so the default surface (and every fake) keeps compiling:
// implementers opt in by type-asserting to this interface, the same
// pattern CronLister and SkillsLister use.
type AgentSessionStore interface {
	// CreateAgentSession creates a session BOUND to agentName - the
	// row's agent column is written, not inferred. Unknown agent
	// names are the caller's problem (profile validation happens at
	// the surface, the store knows only rows).
	CreateAgentSession(ctx context.Context, id, parentID, label, agentName string) error
	// ListAgentSessions returns the sessions bound to agentName,
	// ordered by creation time.
	ListAgentSessions(ctx context.Context, agentName string) ([]Session, error)
}

// SessionContextStore is the optional session-context surface (card
// t_9de5b009): the per-session pinned model and working folder. The core
// StoreProvider stays untouched so the default surface (and every fake)
// keeps compiling: implementers opt in by type-asserting to this
// interface, the same pattern AgentSessionStore, CronLister and
// SkillsLister use. Both setters are user-list concerns - they do not
// touch updated_at.
type SessionContextStore interface {
	// SetSessionModel pins the model_name the session remembers (the
	// name the picker wrote; every send resolves through it). Empty
	// clears the pin - the channel's default model serves again.
	SetSessionModel(ctx context.Context, sessionID, model string) error
	// SetSessionWorkspace sets the session's working folder (absolute,
	// existing directory - validated at the surface). Empty clears it:
	// relative file operations root at the process working directory.
	SetSessionWorkspace(ctx context.Context, sessionID, workspace string) error
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

// ModuleStatus is one module in the registry introspection (v0.12 E2,
// the settings-toggle small law: the settings capsule RENDERS the
// registry, core provides the vocabulary). State is "mounted" or
// "disabled" (the omega.json toggle list). Dependents names the live
// modules that require a seam this one alone provides - the Q3 edge
// semantics surfaced, so a disable confirms with what goes down too.
type ModuleStatus struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Provides    []string `json:"provides,omitempty"`
	Requires    []string `json:"requires,omitempty"`
	State       string   `json:"state"` // "mounted" | "disabled"
	Toggleable  bool     `json:"toggleable"`
	Dependents  []string `json:"dependents,omitempty"`
}

// ModuleToggles is the module-toggle seam (v0.12 E2). The host wires
// the mount registry behind it; the serving channel renders Modules
// and applies Toggle. Toggle goes through the same mount machinery as
// boot (Q7: one code path) and rejects loudly (Q11); a host that does
// not expose toggles is nil and endpoints answer 501.
type ModuleToggles interface {
	// Modules returns every module in the build's registry catalog
	// with its live state (engine ring modules never appear - the
	// serving surface filters them, tier law).
	Modules() []ModuleStatus
	// Toggle enables or disables one module at runtime. A rejection
	// (tier law, Q3 edge, roll-back panic) returns an error naming
	// the module and the failed check.
	Toggle(ctx context.Context, name string, enabled bool) error
	// Refusals returns the recorded boundary rejections (Q12) - the
	// toggle ledger the UI surfaces as events.
	Refusals() []Refusal
}

// ChannelDeps carries everything a channel needs to run. Ctx is the
// fully-wired plugin Context — channels create agents from it. Store
// persists messages. Settings (optional) exposes the config knobs.
// Modules (optional) exposes the module-toggle surface. Version is
// the host's build tag, surfaced through /health.
type ChannelDeps struct {
	Ctx      *Context
	Store    StoreProvider
	Settings Settings
	Modules  ModuleToggles
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

// CronJobInfo is one read-only row in the cron inspector listing
// (GET /cron/jobs, card t_db430fea): the job's name, its 5-field cron
// schedule, whether the scheduler fires it, and the next fire time in
// RFC3339 local time. Next is empty when the job is disabled, the
// schedule does not parse, or nothing ever fires - a disabled job must
// never promise a next fire.
type CronJobInfo struct {
	Name     string `json:"name"`
	Schedule string `json:"schedule"`
	Enabled  bool   `json:"enabled"`
	Next     string `json:"next"`
}

// CronLister is the read-only inspector view over the cron scheduler.
// It is a separate interface from CronProvider on purpose: starting a
// scheduler and reading its state are different concerns, and any type
// implementing CronProvider may grow this method without breaking the
// serve host. A CronProvider that is not a CronLister serves an empty
// listing.
type CronLister interface {
	// Jobs returns the current job set with computed next fire times.
	Jobs() []CronJobInfo
}

// SkillsLister is the read-only inspector view over the skill
// inventory (GET /skills, card t_db430fea). SkillsProvider loads one
// shot from a directory; a SkillsLister re-reads on demand, so the
// listing tracks the directory without a reload step.
type SkillsLister interface {
	// ListSkills returns the currently loaded skills, name and
	// description included (content is inspector-irrelevant).
	ListSkills() ([]Skill, error)
}

// VerdictPrompt is one agent-issued question (verdict cards v1) as it
// is registered with the verdicts seam: the tool's arguments plus the
// runtime identity (id, session, deadline). Each option carries a
// label and an optional description (empty = no options, allow-text
// only). Multi allows several picks in one answer; AllowText
// additionally allows a typed answer. Defaults ride the seam
// implementation (20 minutes).
type VerdictPrompt struct {
	ID        string
	SessionID string
	Title     string
	Body      string
	Options   []VerdictOption
	Multi     bool
	AllowText bool
	TimeoutS  int
	ExpiresAt time.Time
}

// Verdict is the recorded answer to one VerdictPrompt: a single pick
// (index is 1-based; Option the label), a multi pick (OptionIndexes
// every selected index, Options the labels), free text, a recorded
// skip (no silent dismissal), or the expiry firing. Exactly one of
// the five applies.
type Verdict struct {
	CardID        string
	OptionIndex   int
	Option        string
	OptionIndexes []int
	Options       []string
	Text          string
	Skipped       bool
	Expired       bool
}

// Kind labels the verdict for tool results and wire payloads:
// "answer", "skip", "expired".
func (v Verdict) Kind() string {
	switch {
	case v.Expired:
		return "expired"
	case v.Skipped:
		return "skip"
	default:
		return "answer"
	}
}

// VerdictsProvider is the interactive-question seam (verdict cards v1).
// The verdict module implements it; the tool blocks on Ask, the HTTP
// channel and the TUI answer through it. Whichever surface answers
// first wins - Answer enforces the race. Nil on the Context (module
// absent) is the only valid "not implemented" answer: the tool refuses
// to block without the seam.
type VerdictsProvider interface {
	// Ask registers the card and BLOCKS until the user answers, the
	// card expires, ctx is cancelled (run stop / shutdown), or the
	// seam stops. Expired is a normal return (Verdict.Expired), not an
	// error. The card persists to the session transcript before
	// blocking when a store is behind the seam.
	Ask(ctx context.Context, card VerdictPrompt) (Verdict, error)
	// Answer records the user's answer to one pending card. The FIRST
	// answer wins; a second one errors naming the reason (already
	// answered, already expired, unknown card). The record persists
	// before Ask wakes. Returns the RESOLVED verdict (card id and the
	// picked option label filled in) so the answering surface can echo
	// what actually landed.
	Answer(sessionID, cardID string, v Verdict) (Verdict, error)
	// Pending lists the unanswered, unexpired cards, oldest first.
	// A session filter of "" lists every session's pending cards.
	Pending(sessionID string) []VerdictPrompt
}

// Run origins (verdict cards v1): which surface launched the agent
// verdict.request only issues from interactive turns - cron fires
// and delegate subagents must never hard-pause waiting for a click.
const (
	RunOriginInteractive = "interactive"
	RunOriginCron        = "cron"
	RunOriginDelegate    = "delegate"
)

// WithRunOrigin tags the run context with its launching surface. The
// verdict.request tool reads it to refuse background origins.
func WithRunOrigin(ctx context.Context, origin string) context.Context {
	if origin == "" {
		return ctx
	}
	return context.WithValue(ctx, runOriginKey{}, origin)
}

// RunOriginFromContext returns the run's origin tag ("" when unset -
// callers treat that as interactive).
func RunOriginFromContext(ctx context.Context) string {
	origin, _ := ctx.Value(runOriginKey{}).(string)
	return origin
}

type runOriginKey struct{}

// runScope is one run's per-session working-folder override (card
// t_9de5b009). CWD roots the run's relative file operations (the tools
// and the prompt's CWD line); PromptContext is the trust-gated
// AGENTS.md project context of THAT folder, resolved by the channel at
// send time.
type runScope struct {
	CWD           string
	PromptContext string
}

type runScopeKey struct{}

// RunWorkspace returns the run's session workspace root ("" = none - the
// run is not rooted in a session folder). File tools resolve relative
// paths against it; the loop serves it as the prompt's CWD. The channel
// injects the value from the session's Workspace column at send time.
func RunWorkspace(ctx context.Context) string {
	scope, _ := ctx.Value(runScopeKey{}).(runScope)
	return scope.CWD
}

// RunPromptContext returns the run's trust-gated project context override
// ("" = unset - the agent's host-level prompt context applies).
func RunPromptContext(ctx context.Context) string {
	scope, _ := ctx.Value(runScopeKey{}).(runScope)
	return scope.PromptContext
}

// WithRunWorkspace returns a child context carrying the run's session
// workspace root and its trust-gated project context. An empty dir is
// the unset case: the child wraps nothing, the host-level values apply.
// Values are run-scoped: each Run caller wraps its own context, so
// parallel runs never share one.
func WithRunWorkspace(ctx context.Context, dir, promptContext string) context.Context {
	if dir == "" {
		return ctx
	}
	return context.WithValue(ctx, runScopeKey{}, runScope{CWD: dir, PromptContext: promptContext})
}

// Frontend is the user-facing interface seam. The default implementation
// is the terminal TUI (tui/). A custom implementation could
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

// Notification is one finished-thing record (card t_5d9032d3): a kanban
// task completed, a cron fire finished, or a dispatched chat run ended.
// The title line is METADATA ONLY (source kind + id + verdict) - prompt
// and session content never ride a notification (privacy law).
type Notification struct {
	ID        int64  `json:"id"`
	Kind      string `json:"kind"` // task | cron | chat
	SourceID  string `json:"source_id"`
	Title     string `json:"title"`   // metadata line, no prompt/session content
	Verdict   string `json:"verdict"` // ok | failed
	CreatedAt string `json:"created_at"`
	Read      bool   `json:"read"`
}

// NotificationsProvider is the finished-things store seam. The default
// implementation is modules/notifications (a small SQLite db in the
// data dir, same family as the board). Emitters (kanban, cron,
// delegate) call Emit; the HTTP channel serves List/Mark* to the webui
// bell and live-forwards Emit traffic to Subscribe clients (toasts).
type NotificationsProvider interface {
	// Emit records one notification. The store fills ID and CreatedAt
	// and validates Kind ("task", "cron", "chat") and Verdict ("",
	// "ok", "failed" - "" normalizes to ok). Retention keeps the 200
	// newest records; the oldest pruned on insert.
	Emit(n Notification) (Notification, error)
	// List returns records newest-first. A non-empty unreadOnly
	// filters to unread rows. limit <= 0 means the server default;
	// offset pages deeper. The second return is the current unread
	// count (computed over the whole set, not the page).
	List(ctx context.Context, unreadOnly bool, limit, offset int) (notes []Notification, unread int, err error)
	// MarkRead flags one record read. A missing id errors.
	MarkRead(ctx context.Context, id int64) error
	// MarkAllRead flags every record read.
	MarkAllRead(ctx context.Context) error
	// Subscribe returns a live channel of newly emitted records plus a
	// cancel func that releases the subscription. Delivery is
	// best-effort: a slow reader drops frames rather than blocking
	// emission. Cancel is idempotent.
	Subscribe() (<-chan Notification, func())
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
