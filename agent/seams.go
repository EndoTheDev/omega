package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/EndoTheDev/omega/core"
)

// The agent's need declarations. In Go the interface belongs to the CONSUMER: each
// describes what the loop machinery needs, never what a supplier is. Capsules satisfy them
// structurally and offer supply under the need keys through the universal registry; the
// consumer resolves at bind and asserts the shape there, so a wrong-shaped offer dies at
// boot naming the need, never as a silent nil mid-turn. Suppliers never import these names.

type LoopProvider interface {
	Run(ctx context.Context, opts LoopOptions) error
}

type LoopOptions struct {
	Provider      Provider
	Messages      []core.Message
	Tools         map[string]core.Tool
	ToolProviders []core.ToolProvider // additive tool sources (extensions)
	// DisabledTools is the config.yaml disabled.tools opt-out, filtered AFTER the merge: a
	// name no built-in occupies drops, a built-in keeps its seat (existence only).
	DisabledTools      []string
	CompactionProvider CompactionProvider
	PromptBuilder      PromptBuilder
	ExtensionInfos     []core.ExtensionInfo
	MaxTurns           int
	MaxToolOutput      int
	CWD                string
	PromptCustom       string
	PromptAppend       []string
	PromptContext      string // trust-gated AGENTS.md project context
	Events             chan<- core.Event
	// UserInput is the user messages while running (nil for one-shot mode).
	UserInput <-chan string
	// Approvals is the smart-approval seam. Nil = no gate: tools execute unchanged.
	// Non-nil, the loop consults it before every tool run and turns a non-nil error into the
	// tool result (the command never executes).
	Approvals ApprovalProvider
	Nudge     TurnNudger
	Logger    LoggerProvider

	// The yield harness. EITHER nil = the whole harness is off: every tool blocks to
	// completion inline - the documented degradation for hosts that cannot re-enter a run.
	YieldLedger  YieldLedger
	YieldWake    func(sessionID string, res core.YieldResult)
	YieldTimeout time.Duration
	// Stop is the run's user-stop latch, read off the run context by agent.Run. A dispatched
	// call drops the run context's cancellation (it outlives the turn by design) but NOT
	// this latch: a user stop cancels it with core.ErrUserStopped, so a settled result never
	// wakes a home the human just killed. Nil = a run no human can stop.
	Stop *core.RunStop
}

// CompactionProvider compacts history when the budget is exceeded; nil means compaction is
// off.
type CompactionProvider interface {
	Compact(ctx context.Context, messages []core.Message) ([]core.Message, error)
}

// TurnNudger is the learning-loop seam. Nil seam = no nudging; the loop pays nothing.
type TurnNudger interface {
	// ObserveToolRun runs on the execution goroutine, so implementations must be safe for
	// concurrent calls.
	ObserveToolRun(name string, args map[string]any, runErr error)
	NudgeLine() string
}

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

	GetMessagesWithTimes(ctx context.Context, sessionID string) ([]core.StoredMessage, error)

	GetActiveView(ctx context.Context, sessionID string) (core.ActiveView, error)

	GetActiveMessages(ctx context.Context, sessionID string) ([]core.Message, error)

	GetSessionTree(ctx context.Context) ([]*core.SessionNode, error)
	GetAncestorMessages(ctx context.Context, sessionID string) ([]core.Message, error)
	SearchMessages(ctx context.Context, query string) ([]core.SearchResult, error)

	SearchSessions(ctx context.Context, query string) ([]core.SearchResult, error)
	ComputeInsights(ctx context.Context, days int) (*core.Insights, error)
	CountMessages(ctx context.Context, sessionID string) (int, error)

	SessionCounts(ctx context.Context) (map[string]core.SessionCounts, error)

	AddSessionTokens(ctx context.Context, sessionID string, prompt, completion int) error

	// SetSessionLastRun writes the last-run stats block, or clears it with nil.
	SetSessionLastRun(ctx context.Context, sessionID string, stats *core.LastRunStats) error

	SetSessionPinned(ctx context.Context, sessionID string, pinned bool) error

	SetSessionRead(ctx context.Context, sessionID string) error

	SetSessionArchived(ctx context.Context, sessionID string, archived bool) error

	// SetSessionTitle writes a generated label with its title-state transition; a session
	// already at 2 - including every pre-title-state row that carried a label - is never
	// touched.
	SetSessionTitle(ctx context.Context, sessionID string, title string, final bool) error

	SessionModels(ctx context.Context) (map[string]string, error)
}

// AgentSessionStore is the optional agent-scoped surface; a session never switches agents.
type AgentSessionStore interface {
	// CreateAgentSession binds a session to agentName; unknown names are the caller's
	// problem - the store knows only rows.
	CreateAgentSession(ctx context.Context, id, parentID, label, agentName string) error
	ListAgentSessions(ctx context.Context, agentName string) ([]core.Session, error)
}

type SessionOwnerStore interface {
	SessionOwner(sessionID string) string
}

// AgentHomeForgetter is the optional release surface the roster's DELETE verb asks.
type AgentHomeForgetter interface {
	ForgetHome(agentName string) bool
}

// SessionContextStore's setters are user-list concerns: none of them touches updated_at.
type SessionContextStore interface {
	SetSessionModel(ctx context.Context, sessionID, model string) error
	SetSessionProvider(ctx context.Context, sessionID, provider string) error
	SetSessionWorkspace(ctx context.Context, sessionID, workspace string) error
	SetSessionContextStart(ctx context.Context, sessionID, start, summary string) error
}

// DelegateSessionStore is the optional delegate-lineage surface. Delegate children NEVER
// inherit their caller's context: a delegate child's row records its spawner in
// delegate_parent while parent_id stays NULL (parent_id is the FORK lineage - the ancestor
// merge reads only that).
type DelegateSessionStore interface {
	CreateDelegateSession(ctx context.Context, id, delegateParent, label string) error
}

// StorePinger is the optional store health surface. Ping is a liveness read the caller
// bounds with ctx - a pooled round trip, never a full-file scan.
type StorePinger interface {
	Ping(ctx context.Context) error
}

type LoggerProvider interface {
	Printf(format string, args ...any)
	Warnf(format string, args ...any)
	Errorf(format string, args ...any)
	Close() error
}

// LoggerLastError is the optional logger upgrade behind the health popover.
type LoggerLastError interface {
	LastError() (message string, at time.Time, ok bool)
}

type PromptBuilder interface {
	BuildPrompt(ctx context.Context, opts core.PromptBuildOptions) (string, bool)
	// Guidelines takes the same build options: the guidelines are prompt prose, so they
	// obey the same fold law as every section (a line may not name a disabled tool).
	Guidelines(opts core.PromptBuildOptions) []string
}

type Provider interface {
	Stream(ctx context.Context, messages []core.Message, tools []core.ToolSchema) <-chan core.ProviderStreamEvent
	ModelName() string
	SetThinkingLevel(level string)
	// WithModel returns a copy serving the given model; the receiver is left unchanged, so
	// callers derive per-run providers without mutating shared state. WithThinkingLevel holds
	// the same copy discipline.
	WithModel(model string) Provider
	WithThinkingLevel(level string) Provider
	ListModels() ([]string, error)
	ModelInfo() (core.ModelInfo, error)
}

// ProviderInstances is the optional upgrade for provider seams that carry NAMED instances.
// An unknown name (the instance was removed from config) fails the run loudly - never a
// silent fallback to the default.
type ProviderInstances interface {
	// Instance resolves one named instance; it must not mutate the receiver.
	Instance(name string) (Provider, bool)
}

type ModelCapabilitiesProvider interface {
	ListModelEntries() ([]core.ModelEntry, error)
}

type ProviderInstanceNames interface {
	InstanceNames() []string
}

// ApprovalProvider is the smart-approval seam: the implementation may consult a decision
// model, but the returned contract is the deterministic outcome.
type ApprovalProvider interface {
	// Gate returns (line, nil) when the call may execute, or (line, err) naming the
	// refusal. It must be safe for concurrent calls: the loop executes a turn's tool calls
	// in parallel goroutines.
	Gate(ctx context.Context, toolName string, args map[string]any) (string, error)
}

// GateProvenance is the optional upgrade to ApprovalProvider: a gate that judges a call's
// PROVENANCE needs the conversation the call belongs to, and the loop is the one place
// that holds it. The loop hands the run's message snapshot with every gated call; what
// COUNTS as provenance is the gate's own law (the approval capsule reads human-authored
// turns only - a host the model wrote earlier is no more sourced than one it writes now).
// A gate that does not implement this keeps the plain consult, the documented floor.
type GateProvenance interface {
	GateWithConversation(ctx context.Context, toolName string, args map[string]any, conversation []core.Message) (string, error)
}

// bindNeed resolves one offer under key and asserts the consumer face against it. Absence
// is the documented floor (nil rides, the loop's nil-safe degradation applies); a PRESENT
// offer of the wrong shape is the loud boot failure naming the need - never a silent nil
// found mid-turn, never a lazy resolve.
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
