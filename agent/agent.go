package agent

import (
	"context"

	"github.com/EndoTheDev/omega/ai"
)

// Tool is a callable the model may invoke. The map key is the tool name.
type Tool struct {
	Description string
	Parameters  map[string]any
	Run         func(ctx context.Context, args map[string]any) (string, error)
}

// Agent runs the multi-turn conversation loop between a provider and a
// set of tools. It holds configuration and delegates execution to a
// LoopProvider. Harness concerns (system prompt, compaction) are injected
// via interfaces. The loop itself is swappable via SetLoopProvider.
// The loop is not set by default — the host must wire one (e.g. via
// the agent-loop plugin in modules/agent_loop/).
type Agent struct {
	provider       ai.Provider
	tools          map[string]Tool
	toolProviders  []ToolProvider
	maxTurns       int
	compactor      CompactionProvider
	maxToolOutput  int
	cwd            string
	promptCustom   string
	promptAppend   []string
	promptContext  string
	promptBuilder  PromptBuilder
	extensionInfos []ExtensionInfo
	captioner      Captioner
	userInput      chan string
	injectedMsgs   <-chan InjectedMessage
	pendingDeleg   func() int
	loop           LoopProvider
	logger         LoggerProvider
}

// NewAgent creates an Agent. A maxTurns <= 0 uses the default cap.
// The agent starts with no loop — the host must wire one via
// SetLoopProvider or by mounting the agent-loop plugin.
func NewAgent(provider ai.Provider, tools map[string]Tool, maxTurns int) *Agent {
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
// Their tools are merged on each Run.
func (a *Agent) SetToolProviders(tps []ToolProvider) {
	a.toolProviders = tps
}

// Tools returns a snapshot of the agent's executable tool registry: the
// explicit tools plus every ToolProvider's map - the union Run actually
// loops with (see RunWithProvider / the loop's merge order). The copy is
// defensive: callers (e.g. the HTTP channel's /tools listing) can never
// mutate the agent's underlying maps.
func (a *Agent) Tools() map[string]Tool {
	size := len(a.tools)
	for _, tp := range a.toolProviders {
		if tp != nil {
			size += len(tp.Tools())
		}
	}
	tools := make(map[string]Tool, size)
	for name, t := range a.tools {
		tools[name] = t
	}
	for _, tp := range a.toolProviders {
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

// SetPromptBuilder installs the prompt builder for system prompt assembly.
func (a *Agent) SetPromptBuilder(pb PromptBuilder) {
	a.promptBuilder = pb
}

// SetExtensionInfos sets the metadata about loaded extensions, used
// for prompt building and the /extensions command.
func (a *Agent) SetExtensionInfos(infos []ExtensionInfo) {
	a.extensionInfos = infos
}

// SetCaptioner installs the image captioner (agent/caption.go). When
// set, image-carrying user messages are described once before the loop
// and the loop-visible text carries the caption. Nil = images untouched.
func (a *Agent) SetCaptioner(c Captioner) {
	a.captioner = c
}

// SetLoopProvider installs a custom agent loop. A nil value clears
// the loop; Run will return an error if called without a loop set.
func (a *Agent) SetLoopProvider(loop LoopProvider) {
	a.loop = loop
}

// SetUserInput sets a channel for receiving user messages while the
// agent loop is running. The TUI uses this so the user can chat while
// subagents are running. One-shot mode (omega run) leaves it nil.
func (a *Agent) SetUserInput(ch chan string) {
	a.userInput = ch
}

// SetInjectedMessages sets the channel for subagent result injection.
func (a *Agent) SetInjectedMessages(ch <-chan InjectedMessage) {
	a.injectedMsgs = ch
}

// SetPendingDelegations sets the function that returns the count of
// running subagent tasks.
func (a *Agent) SetPendingDelegations(f func() int) {
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
	MaxTurns       int
	MaxToolOutput  int
	PromptCustom   string
	PromptAppend   []string
	PromptContext  string
	CWD            string
}

// NewFromContext creates a fully-wired Agent from a mounted Context.
// Channels, newAgent, and the TUI use this. The agent gets the loop,
// tool providers, prompt builder, compactor, injected messages,
// pending delegations, and logger from ctx. All AgentOptions fields
// are optional zero-values — setters tolerate zero like the
// constructor defaults do.
func NewFromContext(ctx *Context, opts AgentOptions) *Agent {
	ag := NewAgent(ctx.Provider, nil, opts.MaxTurns)
	ag.SetToolProviders(ctx.ToolProviders)
	ag.SetLoopProvider(ctx.Loop)
	ag.SetPromptBuilder(ctx.PromptBuilder)
	ag.SetExtensionInfos(ctx.Infos)
	ag.SetLogger(ctx.Logger)
	ag.SetMaxToolOutput(opts.MaxToolOutput)
	ag.SetPromptCustom(opts.PromptCustom)
	ag.SetPromptAppend(opts.PromptAppend)
	ag.SetPromptContext(opts.PromptContext)
	ag.SetCWD(opts.CWD)
	if ctx.Compactor != nil {
		ag.SetCompactionProvider(ctx.Compactor)
	}
	if ctx.InjectedMessages != nil {
		ag.SetInjectedMessages(ctx.InjectedMessages)
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
func (a *Agent) Provider() ai.Provider {
	return a.provider
}

// ModelInfo returns the current model's metadata (context window).
func (a *Agent) ModelInfo() (ai.ModelInfo, error) {
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

// Run executes the conversation loop and returns a channel of events.
// The channel is closed when the loop ends. A non-nil tools map overrides
// the agent's registered tools for this run.
//
// Run always spawns - there is no running guard. The loop is stateless
// (all state rides LoopOptions) and each Run creates its own events
// channel, so parallel runs are safe by construction; use RunWithProvider
// to give a run its own model.
//
// ponytail: the injected-messages channel and pending delegations are
// shared across concurrent runs, so a delegated message drains into an
// arbitrary active run - fine at single-user load. Upgrade path: a
// per-run injected channel (a config-defined agent registry builds on
// that later).
func (a *Agent) Run(ctx context.Context, messages []ai.Message, tools map[string]Tool) <-chan Event {
	return a.RunWithProvider(ctx, a.provider, messages, tools)
}

// RunWithProvider starts a run on the given provider - typically a copy
// derived from Provider() via WithModel, carrying a per-request model.
// Returns a channel of events, closed when the run ends.
func (a *Agent) RunWithProvider(ctx context.Context, provider ai.Provider, messages []ai.Message, tools map[string]Tool) <-chan Event {
	events := make(chan Event)
	go func() {
		defer close(events)
		if a.loop == nil {
			events <- AgentEnd{Type: "agent_end", FinishReason: "error", Error: "no loop configured — mount the agent-loop plugin or call SetLoopProvider"}
			return
		}
		// Image captioning (vision_model knob): captioned copies go to
		// the loop, the caller's messages stay original. A captioning
		// failure ends the run before any streaming - loud, not a
		// provider 400 about unseen pixels.
		messages, err := a.applyCaptions(ctx, messages)
		if err != nil {
			events <- AgentEnd{Type: "agent_end", FinishReason: "error", Error: err.Error()}
			return
		}
		runTools := tools
		if runTools == nil {
			runTools = a.tools
		}
		a.loop.Run(ctx, LoopOptions{
			Provider:           provider,
			Messages:           messages,
			Tools:              runTools,
			ToolProviders:      a.toolProviders,
			CompactionProvider: a.compactor,
			PromptBuilder:      a.promptBuilder,
			ExtensionInfos:     a.extensionInfos,
			MaxTurns:           a.maxTurns,
			MaxToolOutput:      a.maxToolOutput,
			CWD:                a.cwd,
			PromptCustom:       a.promptCustom,
			PromptAppend:       a.promptAppend,
			PromptContext:      a.promptContext,
			Events:             events,
			InjectedMessages:   a.injectedMsgs,
			UserInput:          a.userInput,
			PendingDelegations: a.pendingDeleg,
			Logger:             a.logger,
		})
	}()
	return events
}
