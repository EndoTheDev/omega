package agent

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/EndoTheDev/omega/core"
)

// Agent runs the multi-turn conversation loop between a provider and a set of tools.
// The loop is not set by default — the host must wire one (e.g. via the agent-loop
// plugin in agent/loop/).
type Agent struct {
	provider      Provider
	tools         map[string]core.Tool
	toolProviders []core.ToolProvider
	// tpMu guards toolProviders and extensionInfos: a live module toggle swaps
	// both sets mid-process while concurrent runs and the /tools and /extensions
	// inventories read them under the loop.
	tpMu   sync.RWMutex
	tpLive bool // NewFromContext built this agent from a mounted Context
	disabledTools  map[string]bool
	approvals      ApprovalProvider // smart-approval seam snapshot (tpMu-guarded)
	maxTurns       int
	compactor      CompactionProvider
	maxToolOutput  int
	cwd            string
	promptCustom   string
	promptAppend   []string
	promptContext  string
	promptBuilder  PromptBuilder
	nudge          TurnNudger // learning-loop reminder seam
	extensionInfos []core.ExtensionInfo
	captioner      core.Captioner
	userInput      chan string
	loop           LoopProvider
	logger         LoggerProvider
	// The yield harness: the pending-calls table, the wake injector and the dispatched.
	// All nil-safe - a host without the harness runs every tool loop-back (see
	// LoopOptions.YieldLedger).
	yieldLedger  YieldLedger
	yieldWake    func(sessionID string, res core.YieldResult)
	yieldTimeout time.Duration
	// eventObserverSource reads the Context's spine subscriptions at each Run. Nil-safe:
	// an Agent built without NewFromContext runs observer-free.
	eventObserverSource func() []core.EventObserver
	// sessionObserverSource is the session-attributed spine subscriptions: same
	// snapshot-per-run rule, and the forward goroutine hands each observer the run's
	// session id alongside the event. Nil-safe like the plain source.
	sessionObserverSource func() []core.SessionEventObserver
}

// EventObservers returns the agent's spine snapshot source; nil is the empty.
func (a *Agent) eventObservers() []core.EventObserver {
	if a.eventObserverSource == nil {
		return nil
	}
	return a.eventObserverSource()
}

// SetEventObserverSource wires the Context's spine subscriptions (a snapshot per run -
// a live toggle's remount re-reads it on the next run, never mid-run).
func (a *Agent) SetEventObserverSource(src func() []core.EventObserver) {
	a.eventObserverSource = src
}

// sessionEventObservers returns the session-attributed spine snapshot source.
func (a *Agent) sessionEventObservers() []core.SessionEventObserver {
	if a.sessionObserverSource == nil {
		return nil
	}
	return a.sessionObserverSource()
}

// SetSessionEventObserverSource wires the Context's session-attributed spine.
func (a *Agent) SetSessionEventObserverSource(src func() []core.SessionEventObserver) {
	a.sessionObserverSource = src
}

// NewAgent creates an Agent. The agent starts with no loop — the host must wire one via
// SetLoopProvider or by mounting the agent-loop plugin.
func NewAgent(provider Provider, tools map[string]core.Tool, maxTurns int) *Agent {
	return &Agent{
		provider: provider,
		tools:    tools,
		maxTurns: maxTurns,
	}
}

// SetCompactionProvider installs the compactor. A nil value disables compaction.
func (a *Agent) SetCompactionProvider(c CompactionProvider) {
	a.compactor = c
}

// SetMaxToolOutput sets the maximum tool result length in characters.
func (a *Agent) SetMaxToolOutput(n int) {
	a.maxToolOutput = n
}

// SetMaxTurns swaps the per-run turn cap live.
func (a *Agent) SetMaxTurns(n int) {
	a.tpMu.Lock()
	defer a.tpMu.Unlock()
	a.maxTurns = n
}

// SetCWD sets the working directory passed to extension-built prompts.
func (a *Agent) SetCWD(dir string) {
	a.cwd = dir
}

// SetPromptCustom stores the user-supplied custom prompt from config.
func (a *Agent) SetPromptCustom(s string) {
	a.tpMu.Lock()
	defer a.tpMu.Unlock()
	a.promptCustom = s
}

// PromptCustom returns the live custom prompt snapshot.
func (a *Agent) PromptCustom() string {
	a.tpMu.RLock()
	defer a.tpMu.RUnlock()
	return a.promptCustom
}

// MaxTurns returns the live per-run turn cap snapshot.
func (a *Agent) MaxTurns() int {
	a.tpMu.RLock()
	defer a.tpMu.RUnlock()
	return a.maxTurns
}

// SetPromptAppend stores extra prompts from --append-system-prompt.
func (a *Agent) SetPromptAppend(prompts []string) {
	a.promptAppend = prompts
}

// SetPromptContext stores the trust-gated AGENTS.md project context.
func (a *Agent) SetPromptContext(s string) {
	a.promptContext = s
}

// SetToolProviders installs additional tool providers (from extensions). The
// live-toggle path calls this mid-process; concurrent runs read it under the
// tpMu lock - the loop always receives a snapshot taken under the same lock.
func (a *Agent) SetToolProviders(tps []core.ToolProvider) {
	a.tpMu.Lock()
	defer a.tpMu.Unlock()
	a.toolProviders = tps
}

// RefreshToolProviders re-reads the tool set from the mounted Context the agent was.
func (a *Agent) RefreshToolProviders(ctx *Context) {
	a.tpMu.Lock()
	defer a.tpMu.Unlock()
	if !a.tpLive || ctx == nil {
		return
	}
	providers := make([]core.ToolProvider, len(ctx.ToolProviders))
	copy(providers, ctx.ToolProviders)
	a.toolProviders = providers
}

// toolUnion copies the registry union Run loops with. The copy is defensive: callers
// can never mutate the agent's underlying maps. The disabled fold is NOT applied here -
// the caller decides (Tools filters, ToolsWithDisabled ships the fold alongside).
func (a *Agent) toolUnion() map[string]core.Tool {
	a.tpMu.RLock()
	providers := a.toolProviders
	a.tpMu.RUnlock()
	size := len(a.tools)
	for _, tp := range providers {
		if tp != nil {
			size += len(tp.Tools())
		}
	}
	tools := make(map[string]core.Tool, size)
	for name, t := range a.tools {
		tools[name] = t
	}
	for _, tp := range providers {
		if tp == nil {
			continue
		}
		for name, t := range tp.Tools() {
			if _, exists := tools[name]; !exists {
				tools[name] = t
			}
		}
	}
	return tools
}

// Tools returns a snapshot of the agent's executable tool registry.
func (a *Agent) Tools() map[string]core.Tool {
	tools, disabled := a.ToolsWithDisabled()
	for n := range disabled {
		delete(tools, n)
	}
	return tools
}

// ToolsWithDisabled is the fold-aware management read. Tools stays the executable
// registry the loop and the bare GET /tools serve: the model must not see disabled
// tools.
func (a *Agent) ToolsWithDisabled() (tools map[string]core.Tool, disabled map[string]bool) {
	a.tpMu.RLock()
	fold := a.disabledTools
	a.tpMu.RUnlock()
	tools = a.toolUnion()
	out := make(map[string]bool, len(fold))
	for n := range fold {
		out[n] = true
	}
	return tools, out
}

// SetDisabledTools installs the config.yaml disabled.tools opt-out.
func (a *Agent) SetDisabledTools(names []string) {
	a.tpMu.Lock()
	defer a.tpMu.Unlock()
	if len(names) == 0 {
		a.disabledTools = nil
		return
	}
	out := make(map[string]bool, len(names))
	for _, n := range names {
		out[n] = true
	}
	a.disabledTools = out
}

// SetApprovals installs the approval-gate seam snapshot. A nil value means the gate is
// absent (mode off or no module): the loop pays nothing.
func (a *Agent) SetApprovals(ap ApprovalProvider) {
	a.tpMu.Lock()
	defer a.tpMu.Unlock()
	a.approvals = ap
}

// RefreshApprovals re-reads the approval seam from the mounted Context the agent was.
func (a *Agent) RefreshApprovals(ctx *Context) {
	a.tpMu.Lock()
	defer a.tpMu.Unlock()
	if !a.tpLive || ctx == nil {
		return
	}
	a.approvals = bindNeed[ApprovalProvider](ctx, "approvals")
}

// RefreshDisabledTools re-reads the disable list from the mounted Context the agent was
// built from (NewFromContext): the host updates ctx.DisabledTools on every registry
// toggle, and a serving surface that re-reads after every successful toggle
// (core.LiveSeams, the same law as RefreshToolProviders) never serves a tool the fold
// says is off.
func (a *Agent) RefreshDisabledTools(ctx *Context) {
	a.tpMu.Lock()
	defer a.tpMu.Unlock()
	if !a.tpLive || ctx == nil {
		return
	}
	a.disabledTools = nil
	if len(ctx.DisabledTools) > 0 {
		out := make(map[string]bool, len(ctx.DisabledTools))
		for _, n := range ctx.DisabledTools {
			out[n] = true
		}
		a.disabledTools = out
	}
}

// disabledToolList flattens the disable set for LoopOptions.
func disabledToolList(set map[string]bool) []string {
	if len(set) == 0 {
		return nil
	}
	names := make([]string, 0, len(set))
	for n := range set {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// SetPromptBuilder installs the prompt builder for system prompt assembly.
func (a *Agent) SetPromptBuilder(pb PromptBuilder) {
	a.promptBuilder = pb
}

// SetTurnNudger installs the learning-loop reminder seam. Nil = no nudging, the loop
// runs unchanged.
func (a *Agent) SetTurnNudger(n TurnNudger) {
	a.nudge = n
}

// ExtensionInfos returns the metadata about loaded extensions.
func (a *Agent) ExtensionInfos() []core.ExtensionInfo {
	a.tpMu.RLock()
	defer a.tpMu.RUnlock()
	return a.extensionInfos
}

// SetExtensionInfos sets the metadata about loaded extensions, used for prompt.
func (a *Agent) SetExtensionInfos(infos []core.ExtensionInfo) {
	a.tpMu.Lock()
	defer a.tpMu.Unlock()
	a.extensionInfos = infos
}

// RefreshExtensionInfos re-reads the extension metadata from the mounted Context.
func (a *Agent) RefreshExtensionInfos(ctx *Context) {
	a.tpMu.Lock()
	defer a.tpMu.Unlock()
	if !a.tpLive || ctx == nil {
		return
	}
	infos := make([]core.ExtensionInfo, len(ctx.Infos))
	copy(infos, ctx.Infos)
	a.extensionInfos = infos
}

// SetCaptioner installs the image captioner (agent/caption.go). Nil = images untouched.
func (a *Agent) SetCaptioner(c core.Captioner) {
	a.captioner = c
}

// SetLoopProvider installs a custom agent loop. A nil value clears
// the loop; Run will return an error if called without a loop set.
func (a *Agent) SetLoopProvider(loop LoopProvider) {
	a.loop = loop
}

// SetUserInput sets a channel for receiving user messages while the agent loop runs.
// One-shot mode (omega run) leaves it nil.
func (a *Agent) SetUserInput(ch chan string) {
	a.userInput = ch
}

// SetYieldLedger installs the pending-calls table. Nil = the harness is off:
// yield-class tools run loop-back (the degradation documented on
// LoopOptions.YieldLedger).
func (a *Agent) SetYieldLedger(l YieldLedger) {
	a.yieldLedger = l
}

// SetYieldWake installs the wake injector the dispatched executor hands a settled. Nil
// = the harness is off.
func (a *Agent) SetYieldWake(wake func(sessionID string, res core.YieldResult)) {
	a.yieldWake = wake
}

// SetYieldTimeout sets a dispatched call's wall-clock cap.
func (a *Agent) SetYieldTimeout(d time.Duration) {
	a.yieldTimeout = d
}

// SetLogger installs a logger for the agent loop. A nil value disables logging.
func (a *Agent) SetLogger(l LoggerProvider) {
	a.logger = l
}

// AgentOptions carries the host-level settings NewFromContext cannot get.
type AgentOptions struct {
	MaxTurns      int
	MaxToolOutput int
	PromptCustom  string
	PromptAppend  []string
	PromptContext string
	CWD           string
}

// NewFromContext creates a fully-wired Agent from a mounted Context.
func NewFromContext(ctx *Context, opts AgentOptions) *Agent {
	ag := NewAgent(bindNeed[Provider](ctx, "provider"), nil, opts.MaxTurns)
	ag.SetToolProviders(ctx.ToolProviders)
	ag.SetDisabledTools(ctx.DisabledTools)
	ag.tpLive = true // the Context stays the source of truth: live toggles re-read it
	ag.SetLoopProvider(bindNeed[LoopProvider](ctx, "loop"))
	ag.SetPromptBuilder(bindNeed[PromptBuilder](ctx, "prompt"))
	ag.SetExtensionInfos(ctx.Infos)
	ag.SetLogger(bindNeed[LoggerProvider](ctx, "logger"))
	ag.SetMaxToolOutput(opts.MaxToolOutput)
	ag.SetPromptCustom(opts.PromptCustom)
	ag.SetPromptAppend(opts.PromptAppend)
	ag.SetPromptContext(opts.PromptContext)
	ag.SetCWD(opts.CWD)
	ag.SetEventObserverSource(ctx.EventObservers)
	ag.SetSessionEventObserverSource(ctx.SessionEventObservers)
	ag.SetCompactionProvider(bindNeed[CompactionProvider](ctx, "compactor"))
	// The smart-approval seam: nil rides as nil.
	ag.SetApprovals(bindNeed[ApprovalProvider](ctx, "approvals"))
	ag.SetTurnNudger(bindNeed[TurnNudger](ctx, "nudger"))
	if ctx.Captioner != nil {
		ag.SetCaptioner(ctx.Captioner)
	}
	return ag
}

// Provider returns the agent's base provider, so callers can derive per-run copies.
func (a *Agent) Provider() Provider {
	return a.provider
}

// ModelInfo returns the current model's metadata (context window).
func (a *Agent) ModelInfo() (core.ModelInfo, error) {
	return a.provider.ModelInfo()
}

// ModelName returns the name of the model the agent's provider serves.
func (a *Agent) ModelName() string {
	return a.provider.ModelName()
}

// ListModels returns the models available from the agent's provider.
func (a *Agent) ListModels() ([]string, error) {
	return a.provider.ListModels()
}

// ListModelEntries returns the provider's model list with capability flags.
func (a *Agent) ListModelEntries() ([]core.ModelEntry, error) {
	if lp, ok := a.provider.(ModelCapabilitiesProvider); ok {
		return lp.ListModelEntries()
	}
	names, err := a.provider.ListModels()
	if err != nil {
		return nil, err
	}
	entries := make([]core.ModelEntry, len(names))
	for i, name := range names {
		entries[i] = core.ModelEntry{Name: name}
	}
	return entries, nil
}

// Run executes the conversation loop and returns a channel of events. A non-nil tools
// map overrides the agent's registered tools for this run. Run affinity: each run
// drains its own events channel; loop state rides LoopOptions, so concurrent runs share
// no result plumbing.
func (a *Agent) Run(ctx context.Context, messages []core.Message, tools map[string]core.Tool) <-chan core.Event {
	return a.RunWithProvider(ctx, a.provider, messages, tools)
}

// RunWithProvider starts a run on the given provider.
func (a *Agent) RunWithProvider(ctx context.Context, provider Provider, messages []core.Message, tools map[string]core.Tool) <-chan core.Event {
	inner := make(chan core.Event)
	events := inner
	// Session attribution default: a run persisting into a session IS that session's run,
	// even when its host never wrapped WithSessionID - the http channel and the CLI loop
	// wrap it, but a cron fire (harness/cron agentTurn) and `omega run --session` set only
	// the turn sink. The context's session id is what the approval gate's evidence ledger
	// (and every other run-attributed record) reads; without this default its rows lose
	// the session exactly on the background origins. The store sink never exists without a
	// session id (NewStoreTurnSink turns an empty one away), and a host that already
	// wrapped one wins here.
	if SessionIDFromContext(ctx) == "" {
		if st, ok := RunTurnSink(ctx).(*storeTurnSink); ok {
			ctx = WithSessionID(ctx, st.sessionID)
		}
	}
	// The event spine: a snapshot of the registered observers forwards every event.
	// Inline, never buffered: a slow observer slows the run (the contract says cheap), but
	// an event can NEVER be delivered to the host before the observers saw it. The
	// session-attributed subscribers ride the same forward goroutine: the run's session id
	// is already settled above (the attribution default), so this is the one place a
	// consumer can learn WHICH conversation an event belongs.
	obs := a.eventObservers()
	sessObs := a.sessionEventObservers()
	if len(obs) > 0 || len(sessObs) > 0 {
		forwarded := make(chan core.Event)
		events = forwarded
		sessionID := SessionIDFromContext(ctx)
		go func() {
			defer close(forwarded)
			for e := range inner {
				for _, o := range obs {
					o(e)
				}
				for _, o := range sessObs {
					o(sessionID, e)
				}
				forwarded <- e
			}
		}()
	}
	go func() {
		defer close(inner)
		if a.loop == nil {
			inner <- core.AgentEnd{Type: "agent_end", FinishReason: "error", Error: "no loop configured — mount the agent-loop plugin or call SetLoopProvider"}
			return
		}
		// Image captioning (vision_model knob): captioned copies go to the loop, the caller's
		// messages stay original.
		messages, err := a.applyCaptions(ctx, provider, messages)
		if err != nil {
			inner <- core.AgentEnd{Type: "agent_end", FinishReason: "error", Error: err.Error()}
			return
		}
		runTools := tools
		if runTools == nil {
			runTools = a.tools
		}
		// Per-run scope override: a session rooted in a workspace folder serves its CWD.
		runCWD, runPromptContext := a.cwd, a.promptContext
		if dir := core.RunWorkspace(ctx); dir != "" {
			runCWD = dir
		}
		if pc := core.RunPromptContext(ctx); pc != "" {
			runPromptContext = pc
		}
		a.tpMu.RLock()
		toolProviders := a.toolProviders
		disabledTools := a.disabledTools
		approvals := a.approvals
		// maxTurns and the custom prompt are hot-apply knobs now - the run reads them under
		// the same lock the settings save writes them through, never racy.
		maxTurns := a.maxTurns
		promptCustom := a.promptCustom
		yieldLedger := a.yieldLedger
		yieldWake := a.yieldWake
		a.tpMu.RUnlock()
		a.loop.Run(ctx, LoopOptions{
			Provider:           provider,
			Messages:           messages,
			Tools:              runTools,
			ToolProviders:      toolProviders,
			DisabledTools:      disabledToolList(disabledTools),
			CompactionProvider: a.compactor,
			PromptBuilder:      a.promptBuilder,
			// The registry entry's disable flag rides the prompt build: the Tools section lists
			// what the model can actually call, never a tool the fold switched off.
			ExtensionInfos: core.StampDisabledTools(a.extensionInfos, disabledToolList(disabledTools)),
			MaxTurns:           maxTurns,
			MaxToolOutput:      a.maxToolOutput,
			CWD:                runCWD,
			PromptCustom:       promptCustom,
			PromptAppend:       a.promptAppend,
			PromptContext:      runPromptContext,
			Events:             inner,
			UserInput:          a.userInput,
			Approvals:          approvals,
			Nudge:              a.nudge,
			Logger:             a.logger,
			YieldLedger:        yieldLedger,
			YieldWake:          yieldWake,
			YieldTimeout:       a.yieldTimeout,
			Stop:               core.RunStopFrom(ctx),
		})
	}()
	return events
}
