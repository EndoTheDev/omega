package agent

import (
	"context"
	"fmt"

	"github.com/EndoTheDev/omega/core"
)

// The agent's need declarations (restructure part 3, the seam
// constitution, wave 2). In Go the interface belongs to the CONSUMER:
// each of these describes what the loop machinery needs, never what a
// supplier is. The capsules satisfy them structurally and offer their
// supplies under the need keys (provider, store, logger, compactor,
// prompt, nudger, approvals, loop) through the universal registry; the
// consumer resolves at bind and asserts shape there - a wrong-shaped
// offer dies at boot naming the need, never a silent nil mid-turn.
// Suppliers never import these names; deleting a supply capsule leaves
// the need statement (the consumer's own text) plus the floor.

// LoopProvider drives the multi-turn conversation. The default
// implementation (loop.Loop in agent/loop/) runs the
// standard turn loop: stream provider responses, execute tool calls,
// feed results back. A custom implementation can replace the entire
// loop logic.
type LoopProvider interface {
	Run(ctx context.Context, opts LoopOptions) error
}

// LoopOptions carries everything the loop needs to run one agent
// session. The Agent struct builds this from its configured fields.
type LoopOptions struct {
	Provider      Provider
	Messages      []core.Message
	Tools         map[string]core.Tool
	ToolProviders []core.ToolProvider // additive tool sources (extensions)
	// DisabledTools is the config.yaml disabled.tools opt-out (v0.14.1
	// registry-browser). The filter runs AFTER the merge: a name no
	// built-in occupies drops, a built-in keeps its seat (the merge
	// law is untouched; the fold settles existence, not precedence).
	DisabledTools      []string
	CompactionProvider CompactionProvider
	PromptBuilder      PromptBuilder        // builds system prompt + guidelines
	ExtensionInfos     []core.ExtensionInfo // for prompt building context
	MaxTurns           int
	MaxToolOutput      int
	CWD                string
	PromptCustom       string   // user-supplied prompt from config, for extension-built prompts
	PromptAppend       []string // extra prompts from --append-system-prompt
	PromptContext      string   // trust-gated AGENTS.md project context
	Events             chan<- core.Event
	// InjectedMessages is the run-affinity drain seam for subagent
	// results (t_f3fb7c52): the loop polls/waits for its OWN run
	// affinity key (agent.RunAffinityFromContext), so a sibling
	// run's queued results are unreachable from here. Nil if the
	// delegate extension is absent.
	InjectedMessages core.InjectedStream
	UserInput        <-chan string // user messages while running (nil for one-shot mode)
	// PendingDelegations counts THIS run's running subagent tasks, per
	// run affinity (t_f3fb7c52); a serve-wide count would park a run's
	// blocking drain on foreign children forever.
	PendingDelegations func(affinity string) int
	// Approvals is the smart-approval seam (card t_c2b52cb5). Nil =
	// no gate: the loop executes tools unchanged. Non-nil, the loop
	// consults it before every tool run and turns a non-nil error
	// into the tool result (the command never executes).
	Approvals ApprovalProvider
	Nudge     TurnNudger     // learning-loop reminder seam (t_9972dfd0; nil = off)
	Logger    LoggerProvider // optional logger; nil = no logging
}

// CompactionProvider handles context compaction when the token budget
// is exceeded. The compactor-seam extension implements this; when no
// compactor extension is loaded, compaction is disabled and the agent
// surfaces a friendly error on context overflow.
type CompactionProvider interface {
	// Compact compacts the message history. Returns the compacted
	// history, or the original if no compaction is needed. A nil
	// CompactionProvider means compaction is disabled.
	Compact(ctx context.Context, messages []core.Message) ([]core.Message, error)
}

// TurnNudger is the learning-loop seam (t_9972dfd0): the loop reports
// every executed tool call, the implementer counts tool iterations
// between persisted lessons (skills writes) and hands back the
// one-line reminder that rides the next turn prep. Nil seam = no
// nudging; the loop pays nothing.
type TurnNudger interface {
	// ObserveToolRun notes one executed tool call. Called from the
	// execution goroutine, so the implementation must be safe for
	// concurrent calls. runErr is the tool's own result error - a
	// failed call still executed.
	ObserveToolRun(name string, args map[string]any, runErr error)
	// NudgeLine returns the reminder line due for the next turn prep
	// ("" = none). Called once per turn after the tool batch lands in
	// the history; a due nudge consumes itself (the counter resets)
	// so the next one fires a full interval later.
	NudgeLine() string
}

// StoreProvider is the session persistence seam. The default
// implementation is SQLite (store.Store in harness/store/), provided via the store
// plugin. All session and message operations go through this interface.
type StoreProvider interface {
	Open(dsn string) error
	Close() error
	CreateSession(ctx context.Context, id, parentID, label string) error
	GetSession(ctx context.Context, id string) (core.Session, error)
	ListSessions(ctx context.Context) ([]core.Session, error)
	DeleteSession(ctx context.Context, id string) error
	UpdateSession(ctx context.Context, id, label string) error
	AppendMessage(ctx context.Context, sessionID string, msg core.Message) error
	GetMessages(ctx context.Context, sessionID string) ([]core.Message, error)

	// GetMessagesWithTimes returns a session's messages in append
	// order, each paired with its row id (the tree identity) and
	// store-level created_at.
	GetMessagesWithTimes(ctx context.Context, sessionID string) ([]core.StoredMessage, error)

	// GetActiveView returns the session's tree-derived active view
	// (t_17365247): the active path for context assembly + transcript
	// rendering, plus the branch-point metadata the variant
	// navigation renders from. Sessions persisted without parent
	// pointers derive the same single linear path they always had.
	GetActiveView(ctx context.Context, sessionID string) (core.ActiveView, error)

	// GetActiveMessages returns the active path as plain messages -
	// the session-load seam (CLI resume, exports).
	GetActiveMessages(ctx context.Context, sessionID string) ([]core.Message, error)

	GetSessionTree(ctx context.Context) ([]*core.SessionNode, error)
	GetAncestorMessages(ctx context.Context, sessionID string) ([]core.Message, error)
	SearchMessages(ctx context.Context, query string) ([]core.SearchResult, error)

	// SearchSessions is the unified search behind /sessions/search:
	// one phrase-quoted term queries both session labels and message
	// content, merged to one hit per session. Label matches come
	// first (Title set, Snippet = label), then content-only sessions
	// by best rank.
	SearchSessions(ctx context.Context, query string) ([]core.SearchResult, error)
	ComputeInsights(ctx context.Context, days int) (*core.Insights, error)
	CountMessages(ctx context.Context, sessionID string) (int, error)

	// SessionCounts computes per-session message and tool-result counts
	// at read time (SQL COUNT, no maintained counters). Sessions with
	// no messages are absent from the map.
	SessionCounts(ctx context.Context) (map[string]core.SessionCounts, error)

	// AddSessionTokens adds a turn's real provider-reported usage to the
	// session's accumulated token totals. Zero usage is a no-op.
	AddSessionTokens(ctx context.Context, sessionID string, prompt, completion int) error

	// SetSessionLastRun writes the last-run stats block (or clears it
	// with nil) on the session's row, so the webui status bar can show
	// the last-known rates after a reload - no stream required.
	SetSessionLastRun(ctx context.Context, sessionID string, stats *core.LastRunStats) error

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
// never switch agents mid-conversation. The store's default surface
// stays untouched so every fake keeps resolving: implementers opt in
// by type-asserting to this interface.
type AgentSessionStore interface {
	// CreateAgentSession creates a session BOUND to agentName - the
	// row's agent column is written, not inferred. Unknown agent
	// names are the caller's problem (profile validation happens at
	// the surface, the store knows only rows).
	CreateAgentSession(ctx context.Context, id, parentID, label, agentName string) error
	// ListAgentSessions returns the sessions bound to agentName,
	// ordered by creation time.
	ListAgentSessions(ctx context.Context, agentName string) ([]core.Session, error)
}

// SessionContextStore is the optional session-context surface (card
// t_9de5b009): the per-session pinned model and working folder. The
// store's default surface stays untouched so every fake keeps
// resolving: implementers opt in by type-asserting to this interface.
// Both setters are user-list concerns - they do not touch updated_at.
type SessionContextStore interface {
	// SetSessionModel pins the model_name the session remembers (the
	// name the picker wrote; every send resolves through it). Empty
	// clears the pin - the channel's default model serves again.
	SetSessionModel(ctx context.Context, sessionID, model string) error
	// SetSessionProvider pins the provider INSTANCE name the session
	// remembers (card t_7dcd4280; every send resolves the name against
	// the live config and fails the run loudly on an unknown one).
	// Empty clears the pin - the default provider serves again.
	SetSessionProvider(ctx context.Context, sessionID, provider string) error
	// SetSessionWorkspace sets the session's working folder (absolute,
	// existing directory - validated at the surface). Empty clears it:
	// relative file operations root at the process working directory.
	SetSessionWorkspace(ctx context.Context, sessionID, workspace string) error
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

// PromptBuilder builds the system prompt and supplies guideline
// lines appended to it. The default implementation is provided via
// the prompt extension. When no prompt extension is loaded, the
// loop gets no prompt and no guidelines (the floor).
type PromptBuilder interface {
	// BuildPrompt assembles the full system prompt. Returns ok=false
	// if the builder does not want to provide a prompt; the agent gets
	// no system prompt in that case.
	BuildPrompt(ctx context.Context, opts core.PromptBuildOptions) (string, bool)
	// Guidelines returns extra lines appended under
	// "## Extension Guidelines" in the system prompt.
	Guidelines() []string
}

// Provider is the model client face the loop machinery consults.
// Stream returns a channel of stream events. Errors are encoded as
// StreamEnd(FinishReason="error", Error=...), not returned as Go
// errors. The channel is closed when the stream ends.
type Provider interface {
	Stream(ctx context.Context, messages []core.Message, tools []core.ToolSchema) <-chan core.ProviderStreamEvent
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
	ModelInfo() (core.ModelInfo, error)
}

// ProviderInstances is the optional upgrade for provider seams that
// carry NAMED instances (config.yaml providers:, card t_7dcd4280).
// The shell resolves a chat run's pinned instance per run; an unknown
// name (the instance was removed from the config) fails the run
// loudly - never a silent fallback to the default. The Provider
// interface stays name-only - a provider without configured instances
// is a full member, the upgrade is probed at runtime like
// ModelCapabilitiesProvider.
type ProviderInstances interface {
	// Instance returns the provider serving the named instance and
	// whether it exists. Must not mutate the receiver.
	Instance(name string) (Provider, bool)
}

// ModelCapabilitiesProvider is an optional interface upgrade for
// providers whose model-list endpoint carries capability flags
// (the ollama family's /api/tags "capabilities" array today).
// The Provider interface stays name-only - a provider without
// capabilities is a full member, the upgrade is probed at runtime.
type ModelCapabilitiesProvider interface {
	ListModelEntries() ([]core.ModelEntry, error)
}

// ProviderInstanceNames is the optional upgrade for providers that
// carry NAMED instances: the id list the serving shell reads when it
// renders the grouped model catalog (card t_1914a8c9). Paired with
// ProviderInstances - a provider without the list is a full member
// (the shell then renders the default section only), the upgrade is
// probed at runtime like ModelCapabilitiesProvider. A copy that
// carries the instances set (WithModel / WithThinkingLevel) reports
// the same names.
type ProviderInstanceNames interface {
	InstanceNames() []string
}

// ApprovalProvider is the smart-approval seam (card t_c2b52cb5). The
// agent loop consults it before EVERY tool execution; the approval
// capsule decides whether the call may run. Model advises, Go decides:
// the implementation may consult a decision model, but the returned
// contract is the deterministic outcome.
type ApprovalProvider interface {
	// Gate returns (line, nil) when the call may execute, or (line,
	// err) naming the reason it may not (denied, escalated-not-
	// executed, answer pending that resolved against execution). The
	// error becomes the tool result (IsError) - the run continues, the
	// command did not happen. Line is the gate's transcript record
	// (card t_1b3e2a8b): the exact decision vocabulary ("smart gate:
	// ...") the webui renders on the call's own row, stamped onto the
	// tool result the loop writes for the call. Empty line = no
	// machine decision (benign command, manual mode, an escalation
	// with no resolution) - no transcript line. The vocabulary lives
	// in the implementing capsule. Gate must be safe for concurrent
	// calls: the loop executes a turn's tool calls in parallel
	// goroutines.
	Gate(ctx context.Context, toolName string, args map[string]any) (string, error)
}

// bindNeed resolves one offer under key and asserts the consumer face
// against it. Absence is the documented floor (nil rides, the
// defaulting law - the loop's nil-safe degradation applies); a PRESENT
// offer of the wrong shape is the loud boot failure naming the need.
// Never a silent nil discovered mid-turn, never a lazy resolve (part
// 3, wave 2: all needs assert at bind).
func bindNeed[T any](ctx *Context, key string) T {
	svc := ctx.Services().Resolve(key)
	if svc == nil {
		var zero T
		return zero
	}
	need, ok := svc.(T)
	if !ok {
		panic(fmt.Sprintf("seam %q: the offered service (%T) does not satisfy the %q need (wrong shape) - fix the offering capsule", key, svc, key))
	}
	return need
}
