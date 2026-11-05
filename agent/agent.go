package agent

import (
	"context"
	"sort"
	"sync"

	"github.com/EndoTheDev/omega/core"
)

// Agent runs the multi-turn conversation loop between a provider and a
// set of tools. It holds configuration and delegates execution to a
// LoopProvider. Harness concerns (system prompt, compaction) are injected
// via interfaces. The loop itself is swappable via SetLoopProvider.
// The loop is not set by default — the host must wire one (e.g. via
// the agent-loop plugin in agent/loop/).
type Agent struct {
	provider      Provider
	tools         map[string]core.Tool
	toolProviders []core.ToolProvider
	// tpMu guards toolProviders and extensionInfos: a live module
	// toggle (v0.12 E2) swaps both sets mid-process while concurrent
	// runs and the /tools and /extensions inventories read them under
	// the loop.
	tpMu   sync.RWMutex
	tpLive bool // NewFromContext built this agent from a mounted Context
	// disabledTools is the config.yaml disabled.tools opt-out (v0.14.1
	// registry-browser). The merge law is untouched - a built-in still
	// wins a name conflict - and the disable filter runs AFTER the
	// merge, dropping only names no built-in occupies: the merge
	// settles who answers a name, the disable list settles whether the
	// tool exists at all.
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
	nudge          TurnNudger // learning-loop reminder seam (t_9972dfd0; nil = off)
	extensionInfos []core.ExtensionInfo
	captioner      core.Captioner
	userInput      chan string
	injectedStream core.InjectedStream
	pendingDeleg   func(affinity string) int
	loop           LoopProvider
	logger         LoggerProvider
	// eventObserverSource reads the Context's spine subscriptions at
	// each Run (card t_72ecca67, M3). Nil-safe: an Agent built without
	// NewFromContext runs observer-free.
	eventObserverSource func() []core.EventObserver
}

// EventObservers returns the agent's spine snapshot source; nil is the
// empty subscription set (a bare Agent has none).
func (a *Agent) eventObservers() []core.EventObserver {
	if a.eventObserverSource == nil {
		return nil
	}
	return a.eventObserverSource()
}

// SetEventObserverSource wires the Context's spine subscriptions (a
// snapshot per run - a live toggle's remount re-reads it on the next
// run, never mid-run).
func (a *Agent) SetEventObserverSource(src func() []core.EventObserver) {
	a.eventObserverSource = src
}

// NewAgent creates an Agent. A maxTurns <= 0 uses the default cap.
// The agent starts with no loop — the host must wire one via
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
// Results exceeding this are truncated. A value <= 0 disables truncation.
func (a *Agent) SetMaxToolOutput(n int) {
	a.maxToolOutput = n
}

// SetCWD sets the working directory passed to extension-built prompts
// via PromptBuildOptions.
func (a *Agent) SetCWD(dir string) {
	a.cwd = dir
}

// SetPromptCustom stores the user-supplied custom prompt from config.
// Passed to extensions via PromptBuildOptions.Custom.
func (a *Agent) SetPromptCustom(s string) {
	a.promptCustom = s
}

// SetPromptAppend stores extra prompts from --append-system-prompt.
// Passed to extensions via PromptBuildOptions.Append.
func (a *Agent) SetPromptAppend(prompts []string) {
	a.promptAppend = prompts
}

// SetPromptContext stores the trust-gated AGENTS.md project context.
// Passed to extensions via PromptBuildOptions.ProjectContext.
func (a *Agent) SetPromptContext(s string) {
	a.promptContext = s
}

// SetToolProviders installs additional tool providers (from extensions).
// Their tools are merged on each Run. The live-toggle path (v0.12 E2)
// calls this mid-process; concurrent runs read it under the tpMu lock -
// the loop always receives a snapshot taken under the same lock.
func (a *Agent) SetToolProviders(tps []core.ToolProvider) {
	a.tpMu.Lock()
	defer a.tpMu.Unlock()
	a.toolProviders = tps
}

// RefreshToolProviders re-reads the tool set from the mounted Context
// the agent was built from (NewFromContext): module providers a live
// toggle appended or removed land here. An agent built without a
// Context keeps its set - the classic constructor has nothing to
// re-read. The copy is defensive: the loop and the inventory see a
// frozen snapshot, the Context keeps mutating underneath.
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

// Tools returns a snapshot of the agent's executable tool registry: the
// explicit tools plus every core.ToolProvider's map - the union Run actually
// loops with (see RunWithProvider / the loop's merge order). The copy is
// defensive: callers (e.g. the HTTP channel's /tools listing) can never
// mutate the agent's underlying maps.
func (a *Agent) Tools() map[string]core.Tool {
	a.tpMu.RLock()
	providers := a.toolProviders
	disabled := a.disabledTools
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
	// The disable filter is LAST in every union: the merge law above is
	// untouched, a disabled name means absent (v0.14.1 registry-browser;
	// the run loop applies the same final filter through LoopOptions).
	for n := range disabled {
		delete(tools, n)
	}
	return tools
}

// SetDisabledTools installs the config.yaml disabled.tools opt-out
// (v0.14.1). Built-ins win name conflicts before this map is
// consulted; see the field comment.
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

// SetApprovals installs the approval-gate seam snapshot. A nil value
// means the gate is absent (mode off or no module): the loop pays
// nothing. The live-toggle path calls the Refresh form below.
func (a *Agent) SetApprovals(ap ApprovalProvider) {
	a.tpMu.Lock()
	defer a.tpMu.Unlock()
	a.approvals = ap
}

// RefreshApprovals re-reads the approval seam from the mounted Context
// the agent was built from (NewFromContext): a live module toggle that
// mounts or unmounts the gate lands here, the same law as
// RefreshToolProviders. An agent built without a Context keeps its set.
func (a *Agent) RefreshApprovals(ctx *Context) {
	a.tpMu.Lock()
	defer a.tpMu.Unlock()
	if !a.tpLive || ctx == nil {
		return
	}
	a.approvals = bindNeed[ApprovalProvider](ctx, "approvals")
}

// RefreshDisabledTools re-reads the disable list from the mounted
// Context the agent was built from (NewFromContext): the host updates
// ctx.DisabledTools on every registry toggle, and a serving surface
// that re-reads after every successful toggle (core.LiveSeams, the
// same law as RefreshToolProviders) never serves a tool the fold says
// is off. An agent built without a Context keeps its set.
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

// disabledToolList flattens the disable set for LoopOptions (sorted,
// nil when none - a zero-name option is indistinguishable from empty).
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

// SetTurnNudger installs the learning-loop reminder seam (t_9972dfd0).
// Nil = no nudging, the loop runs unchanged.
func (a *Agent) SetTurnNudger(n TurnNudger) {
	a.nudge = n
}

// ExtensionInfos returns the metadata about loaded extensions (name,
// description, seams, tool/command counts). The http_channel inspector
// surfaces it on GET /extensions - the webui parity surface.
func (a *Agent) ExtensionInfos() []core.ExtensionInfo {
	a.tpMu.RLock()
	defer a.tpMu.RUnlock()
	return a.extensionInfos
}

// SetExtensionInfos sets the metadata about loaded extensions, used
// for prompt building and the /extensions command.
func (a *Agent) SetExtensionInfos(infos []core.ExtensionInfo) {
	a.tpMu.Lock()
	defer a.tpMu.Unlock()
	a.extensionInfos = infos
}

// RefreshExtensionInfos re-reads the extension metadata from the
// mounted Context the agent was built from (NewFromContext): the
// toggle journal keeps ctx.Infos exact across live module toggles
// (dismount ctxUndo, remount ctxApply), so a serving surface that
// re-reads it after every successful toggle (core.LiveSeams, the same
// law as RefreshToolProviders) always lists what is actually mounted.
// An agent built without a Context keeps its set - the classic
// constructor has nothing to re-read. The copy is defensive: the
// inventory sees a frozen snapshot, the Context keeps mutating
// underneath.
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

// SetCaptioner installs the image captioner (agent/caption.go). When
// set, image-carrying user messages are described once before the loop
// and the loop-visible text carries the caption. Nil = images untouched.
func (a *Agent) SetCaptioner(c core.Captioner) {
	a.captioner = c
}

// SetLoopProvider installs a custom agent loop. A nil value clears
// the loop; Run will return an error if called without a loop set.
func (a *Agent) SetLoopProvider(loop LoopProvider) {
	a.loop = loop
}

// SetUserInput sets a channel for receiving user messages while the
// agent loop is running. The serving channels use this so the user can chat while
// subagents are running. One-shot mode (omega run) leaves it nil.
func (a *Agent) SetUserInput(ch chan string) {
	a.userInput = ch
}

// SetInjectedStream sets the run-affinity drain seam for subagent
// result injection (t_f3fb7c52). The loop polls and waits through this
// seam addressed by its OWN run affinity key, so concurrent runs never
// drain each other's results.
func (a *Agent) SetInjectedStream(stream core.InjectedStream) {
	a.injectedStream = stream
}

// SetPendingDelegations sets the function that returns the count of
// this run's running subagent tasks (per-run affinity, t_f3fb7c52).
func (a *Agent) SetPendingDelegations(f func(affinity string) int) {
	a.pendingDeleg = f
}

// SetLogger installs a logger for the agent loop. A nil value disables
// logging. The logger is passed to the loop via LoopOptions.
func (a *Agent) SetLogger(l LoggerProvider) {
	a.logger = l
}

// AgentOptions carries the host-level settings NewFromContext cannot
// get from the Context itself. All fields are optional zero-values.
type AgentOptions struct {
	MaxTurns      int
	MaxToolOutput int
	PromptCustom  string
	PromptAppend  []string
	PromptContext string
	CWD           string
}

// NewFromContext creates a fully-wired Agent from a mounted Context.
// Channels, newAgent, and the CLI loop use this. The agent gets the loop,
// tool providers, prompt builder, compactor, injected messages,
// pending delegations, and logger from ctx. All AgentOptions fields
// are optional zero-values — setters tolerate zero like the
// constructor defaults do.
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
	ag.SetCompactionProvider(bindNeed[CompactionProvider](ctx, "compactor"))
	// The smart-approval seam (card t_c2b52cb5): nil rides as nil -
	// a host with no gate (mode off, no module) runs the loop ungated.
	ag.SetApprovals(bindNeed[ApprovalProvider](ctx, "approvals"))
	ag.SetTurnNudger(bindNeed[TurnNudger](ctx, "nudger"))
	if ctx.InjectedMessages != nil {
		ag.SetInjectedStream(ctx.InjectedMessages)
	}
	if ctx.PendingDelegations != nil {
		ag.SetPendingDelegations(ctx.PendingDelegations)
	}
	if ctx.Captioner != nil {
		ag.SetCaptioner(ctx.Captioner)
	}
	return ag
}

// Provider returns the agent's base provider, so callers can derive
// per-run copies via WithModel without mutating shared state.
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

// ListModelEntries returns the provider's model list with capability
// flags (card t_70c28128). The optional ModelCapabilitiesProvider
// upgrade supplies the flags when the provider carries them; a
// name-only provider (interface stayed un-upgraded) answers through
// ListModels with empty capability sets.
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

// Run executes the conversation loop and returns a channel of events.
// The channel is closed when the loop ends. A non-nil tools map overrides
// the agent's registered tools for this run.
//
// Run always spawns - there is no running guard. The loop is stateless
// (all state rides LoopOptions) and each Run creates its own events
// channel, so parallel runs are safe by construction; use RunWithProvider
// to give a run its own model.
//
// Run affinity (t_f3fb7c52): injected subagent results are queued per
// run and drained through the core.InjectedStream seam addressed by the
// run's own affinity key, so concurrent runs can no longer drain each
// other's delegate results (the ponytail note this replaces: the shared
// channel drained into an arbitrary active run).
func (a *Agent) Run(ctx context.Context, messages []core.Message, tools map[string]core.Tool) <-chan core.Event {
	return a.RunWithProvider(ctx, a.provider, messages, tools)
}

// RunWithProvider starts a run on the given provider - typically a copy
// derived from Provider() via WithModel, carrying a per-request model.
// Returns a channel of events, closed when the run ends.
func (a *Agent) RunWithProvider(ctx context.Context, provider Provider, messages []core.Message, tools map[string]core.Tool) <-chan core.Event {
	inner := make(chan core.Event)
	events := inner
	// Session attribution default (card t_e203c2f6): a run persisting
	// into a session IS that session's run, even when its host never
	// wrapped WithSessionID - the http channel and the CLI loop wrap
	// it, but a cron fire (harness/cron agentTurn) and `omega run
	// --session` set only the turn sink. The context's session id is
	// what the approval gate's evidence ledger (and every other
	// run-attributed record) reads; without this default its rows lose
	// the session exactly on the background origins. The store sink
	// never exists without a session id (NewStoreTurnSink turns an
	// empty one away), and a host that already wrapped one wins here.
	if SessionIDFromContext(ctx) == "" {
		if st, ok := RunTurnSink(ctx).(*storeTurnSink); ok {
			ctx = WithSessionID(ctx, st.sessionID)
		}
	}
	// The event spine (card t_72ecca67, M3): a snapshot of the
	// registered observers forwards every event to before the host
	// consumer sees it. Inline, never buffered: a slow observer slows
	// the run (the contract says cheap), but an event can NEVER be
	// delivered to the host before the observers saw it.
	if obs := a.eventObservers(); len(obs) > 0 {
		forwarded := make(chan core.Event)
		events = forwarded
		go func() {
			defer close(forwarded)
			for e := range inner {
				for _, o := range obs {
					o(e)
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
		// Image captioning (vision_model knob): captioned copies go to
		// the loop, the caller's messages stay original. A captioning
		// failure ends the run before any streaming - loud, not a
		// provider 400 about unseen pixels.
		messages, err := a.applyCaptions(ctx, messages)
		if err != nil {
			inner <- core.AgentEnd{Type: "agent_end", FinishReason: "error", Error: err.Error()}
			return
		}
		runTools := tools
		if runTools == nil {
			runTools = a.tools
		}
		// Per-run scope override (card t_9de5b009): a session rooted in
		// a workspace folder serves its CWD and trust-gated AGENTS.md
		// context for THIS run only - the agent's host-level values
		// (the process working directory) stay untouched for runs
		// without a session workspace. Empty overrides are unset.
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
		a.tpMu.RUnlock()
		a.loop.Run(ctx, LoopOptions{
			Provider:           provider,
			Messages:           messages,
			Tools:              runTools,
			ToolProviders:      toolProviders,
			DisabledTools:      disabledToolList(disabledTools),
			CompactionProvider: a.compactor,
			PromptBuilder:      a.promptBuilder,
			ExtensionInfos:     a.extensionInfos,
			MaxTurns:           a.maxTurns,
			MaxToolOutput:      a.maxToolOutput,
			CWD:                runCWD,
			PromptCustom:       a.promptCustom,
			PromptAppend:       a.promptAppend,
			PromptContext:      runPromptContext,
			Events:             inner,
			// InjectedMessages is the run-affinity drain seam
			// (t_f3fb7c52): the loop drains for its OWN run's affinity
			// key (agent.RunAffinityFromContext), so a sibling
			// run's queued results never drain into this Run.
			InjectedMessages:   a.injectedStream,
			UserInput:          a.userInput,
			PendingDelegations: a.pendingDeleg,
			Approvals:          approvals,
			Nudge:              a.nudge,
			Logger:             a.logger,
		})
	}()
	return events
}
