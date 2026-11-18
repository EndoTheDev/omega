package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/EndoTheDev/omega/core"
)

// The agent's need declarations (restructure part 3, the seam constitution, wave 2). In
// Go the interface belongs to the CONSUMER: each of these describes what the loop
// machinery needs, never what a supplier is. The capsules satisfy them structurally and
// offer their supplies under the need keys (provider, store, logger, compactor, prompt,
// nudger, approvals, loop) through the universal registry; the consumer resolves at
// bind and asserts shape there - a wrong-shaped offer dies at boot naming the need,
// never a silent nil mid-turn. Suppliers never import these names; deleting a supply
// capsule leaves the need statement (the consumer's own text) plus the floor.

// LoopProvider drives the multi-turn conversation.
type LoopProvider interface {
	Run(ctx context.Context, opts LoopOptions) error
}

// LoopOptions carries everything the loop needs to run one agent session.
type LoopOptions struct {
	Provider      Provider
	Messages      []core.Message
	Tools         map[string]core.Tool
	ToolProviders []core.ToolProvider // additive tool sources (extensions)
	// DisabledTools is the config.yaml disabled.tools opt-out (v0.14.1 registry-browser).
	// The filter runs AFTER the merge: a name no built-in occupies drops, a built-in keeps
	// its seat (the merge law is untouched; the fold settles existence, not precedence).
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
	// UserInput is the user messages while running (nil for one-shot mode).
	UserInput <-chan string
	// Approvals is the smart-approval seam. Nil = no gate: the loop executes tools
	// unchanged. Non-nil, the loop consults it before every tool run and turns a non-nil
	// error into the tool result (the command never executes).
	Approvals ApprovalProvider
	Nudge     TurnNudger     // learning-loop reminder seam
	Logger    LoggerProvider // optional logging; nil = no logging

	// The yield harness. EITHER nil = the whole harness is off: every tool blocks to
	// completion inline (today's degradation), so one-shot hosts (omega run, the CLI loop,
	// tests) never dispatch what cannot re-enter.
	YieldLedger  YieldLedger
	YieldWake    func(sessionID string, res core.YieldResult)
	YieldTimeout time.Duration
	// Stop is the run's user-stop latch, read off the run context by agent.Run. A
	// dispatched call drops the run context's cancellation (it outlives the turn by
	// design) but NOT this latch: a user stop cancels it with core.ErrUserStopped, and a
	// settled result never wakes a home the human just killed. Nil = a run no human can
	// stop (cron, one-shot, the CLI loop) - the documented degradation, identical to the
	// old behavior.
	Stop *core.RunStop
}

// CompactionProvider handles context compaction when the token budget is exceeded.
type CompactionProvider interface {
	// Compact compacts the message history. A nil CompactionProvider means compaction is
	// disabled.
	Compact(ctx context.Context, messages []core.Message) ([]core.Message, error)
}

// TurnNudger is the learning-loop seam. Nil seam = no nudging; the loop pays nothing.
type TurnNudger interface {
	// ObserveToolRun notes one executed tool call. Called from the execution goroutine, so
	// the implementation must be safe for concurrent calls.
	ObserveToolRun(name string, args map[string]any, runErr error)
	// NudgeLine returns the reminder line due for the next turn prep ("" = none).
	NudgeLine() string
}

// StoreProvider is the session persistence seam.
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

	// GetMessagesWithTimes returns a session's messages in append order, each paired.
	GetMessagesWithTimes(ctx context.Context, sessionID string) ([]core.StoredMessage, error)

	// GetActiveView returns the session's tree-derived active view.
	GetActiveView(ctx context.Context, sessionID string) (core.ActiveView, error)

	// GetActiveMessages returns the active path as plain messages.
	GetActiveMessages(ctx context.Context, sessionID string) ([]core.Message, error)

	GetSessionTree(ctx context.Context) ([]*core.SessionNode, error)
	GetAncestorMessages(ctx context.Context, sessionID string) ([]core.Message, error)
	SearchMessages(ctx context.Context, query string) ([]core.SearchResult, error)

	// SearchSessions is the unified search behind /sessions/search.
	SearchSessions(ctx context.Context, query string) ([]core.SearchResult, error)
	ComputeInsights(ctx context.Context, days int) (*core.Insights, error)
	CountMessages(ctx context.Context, sessionID string) (int, error)

	// SessionCounts computes per-session message and tool-result counts at read time.
	SessionCounts(ctx context.Context) (map[string]core.SessionCounts, error)

	// AddSessionTokens adds a turn's real provider-reported usage.
	AddSessionTokens(ctx context.Context, sessionID string, prompt, completion int) error

	// SetSessionLastRun writes the last-run stats block (or clears it with nil).
	SetSessionLastRun(ctx context.Context, sessionID string, stats *core.LastRunStats) error

	// SetSessionPinned sets the pinned flag on a session.
	SetSessionPinned(ctx context.Context, sessionID string, pinned bool) error

	// SetSessionRead stamps a session's last-read time so unread clears.
	SetSessionRead(ctx context.Context, sessionID string) error

	// SetSessionArchived sets the archived flag on a session.
	SetSessionArchived(ctx context.Context, sessionID string, archived bool) error

	// SetSessionTitle writes a generated label together with its title state transition. A
	// session already at 2 - including every pre-title-state row that carried a label - is
	// never touched.
	SetSessionTitle(ctx context.Context, sessionID string, title string, final bool) error

	// SessionModels returns the last recorded model per session, derived.
	SessionModels(ctx context.Context) (map[string]string, error)
}

// AgentSessionStore is the optional agent-scoped session surface. Sessions are bound to
// an agent at creation and never switch agents mid-conversation.
type AgentSessionStore interface {
	// CreateAgentSession creates a session BOUND to agentName. Unknown agent names are the
	// caller's problem (profile validation happens at the surface, the store knows only
	// rows).
	CreateAgentSession(ctx context.Context, id, parentID, label, agentName string) error
	// ListAgentSessions returns the sessions bound to agentName, ordered by creation time.
	ListAgentSessions(ctx context.Context, agentName string) ([]core.Session, error)
}

// SessionOwnerStore is the optional home-routing surface.
type SessionOwnerStore interface {
	SessionOwner(sessionID string) string
}

// AgentHomeForgetter is the optional release surface the roster's DELETE verb asks.
// A store with no routing answers false and the delete behaves exactly as it did.
type AgentHomeForgetter interface {
	// ForgetHome closes and evicts the cached store handle for one agent home, reporting.
	ForgetHome(agentName string) bool
}

// SessionContextStore is the optional session-context surface. Both setters are
// user-list concerns - they do not touch updated_at.
type SessionContextStore interface {
	// SetSessionModel pins the model_name the session remembers.
	SetSessionModel(ctx context.Context, sessionID, model string) error
	// SetSessionProvider pins the provider INSTANCE name the session remembers.
	SetSessionProvider(ctx context.Context, sessionID, provider string) error
	// SetSessionWorkspace sets the session's working folder.
	SetSessionWorkspace(ctx context.Context, sessionID, workspace string) error
	// SetSessionContextStart moves the provider-facing context boundary.
	SetSessionContextStart(ctx context.Context, sessionID, start, summary string) error
}

// DelegateSessionStore is the optional delegate-lineage surface. Delegate children
// NEVER inherit their caller's context: the rule splits the two lineage kinds, so a
// delegate child's row records its spawner in delegate_parent while parent_id stays
// NULL (parent_id is the FORK lineage - the ancestor merge reads only that).
type DelegateSessionStore interface {
	// CreateDelegateSession creates a session whose delegate_parent is the calling.
	CreateDelegateSession(ctx context.Context, id, delegateParent, label string) error
}

// StorePinger is the optional store health surface.
type StorePinger interface {
	// Ping answers whether the store's backing database answers a round
	// trip right now. The caller bounds it with ctx; a healthy store
	// returns nil within that budget, so an implementation must keep it
	// a liveness read (a pooled round trip), never a full-file scan.
	Ping(ctx context.Context) error
}

// LoggerProvider is the logging seam.
type LoggerProvider interface {
	Printf(format string, args ...any)
	// Warnf writes a warning-level log entry.
	Warnf(format string, args ...any)
	Errorf(format string, args ...any)
	Close() error
}

// LoggerLastError is the optional logger upgrade behind the health popover's "last.
type LoggerLastError interface {
	// LastError returns the newest error entry's message.
	LastError() (message string, at time.Time, ok bool)
}

// PromptBuilder builds the system prompt and supplies guideline lines appended to it.
type PromptBuilder interface {
	// BuildPrompt assembles the full system prompt.
	BuildPrompt(ctx context.Context, opts core.PromptBuildOptions) (string, bool)
	Guidelines() []string
}

// Provider is the model client face the loop machinery consults.
type Provider interface {
	Stream(ctx context.Context, messages []core.Message, tools []core.ToolSchema) <-chan core.ProviderStreamEvent
	ModelName() string
	SetThinkingLevel(level string)
	// WithModel returns a copy of the provider serving the given model. The receiver is
	// left unchanged, so callers derive per-run providers without mutating shared state.
	WithModel(model string) Provider
	// WithThinkingLevel returns a copy of the provider streaming at the given thinking.
	// Same copy discipline as WithModel: the receiver is left unchanged, so concurrent
	// per-run derivations don't interfere.
	WithThinkingLevel(level string) Provider
	ListModels() ([]string, error)
	ModelInfo() (core.ModelInfo, error)
}

// ProviderInstances is the optional upgrade for provider seams that carry NAMED. The
// shell resolves a chat run's pinned instance per run; an unknown name (the instance
// was removed from the config) fails the run loudly - never a silent fallback to the
// default.
type ProviderInstances interface {
	// Instance returns the provider serving the named instance and
	// whether it exists. Must not mutate the receiver.
	Instance(name string) (Provider, bool)
}

// ModelCapabilitiesProvider is an optional interface upgrade for providers.
type ModelCapabilitiesProvider interface {
	ListModelEntries() ([]core.ModelEntry, error)
}

// ProviderInstanceNames is the optional upgrade for providers that carry NAMED.
type ProviderInstanceNames interface {
	InstanceNames() []string
}

// ApprovalProvider is the smart-approval seam. Model advises, Go decides: the
// implementation may consult a decision model, but the returned contract is the
// deterministic outcome.
type ApprovalProvider interface {
	// Gate returns (line, nil) when the call may execute, or (line, err) naming the. Gate
	// must be safe for concurrent calls: the loop executes a turn's tool calls in parallel
	// goroutines.
	Gate(ctx context.Context, toolName string, args map[string]any) (string, error)
}

// bindNeed resolves one offer under key and asserts the consumer face against it.
// Absence is the documented floor (nil rides, the defaulting law - the loop's nil-safe
// degradation applies); a PRESENT offer of the wrong shape is the loud boot failure
// naming the need. Never a silent nil discovered mid-turn, never a lazy resolve (part
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
