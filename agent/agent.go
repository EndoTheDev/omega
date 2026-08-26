package agent

import (
	"context"
	"sync"

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
// the agent-loop plugin in extensions/agent_loop/).
type Agent struct {
	provider       ai.Provider
	tools          map[string]Tool
	toolProvider   ToolProvider
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
	userInput      chan string
	injectedMsgs   <-chan InjectedMessage
	pendingDeleg   func() int
	loop           LoopProvider
	mu             sync.Mutex
	running        bool
}

// NewAgent creates an Agent. A maxTurns <= 0 uses the default cap.
// The agent starts with no loop — the host must wire one via
// SetLoopProvider or by mounting the agent-loop plugin. The provider
// may be nil if it will be set later via SetProvider.
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

// SetProvider installs the provider. Used when the provider is created
// after the agent (e.g. from a provider-seam extension loaded after
// the agent is constructed).
func (a *Agent) SetProvider(p ai.Provider) {
	a.provider = p
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

// SetToolProvider installs a tool provider. When set, the agent merges
// the provider's tools with its own on each Run. A nil value is ignored.
func (a *Agent) SetToolProvider(tp ToolProvider) {
	a.toolProvider = tp
}

// SetToolProviders installs additional tool providers (from extensions).
// Their tools are merged on each Run.
func (a *Agent) SetToolProviders(tps []ToolProvider) {
	a.toolProviders = tps
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
// the agent's registered tools for this run. Run returns nil if the agent
// is already running a loop.
func (a *Agent) Run(ctx context.Context, messages []ai.Message, tools map[string]Tool) <-chan Event {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return nil
	}
	a.running = true
	a.mu.Unlock()

	events := make(chan Event)
	go func() {
		defer func() {
			a.mu.Lock()
			a.running = false
			a.mu.Unlock()
		}()
		defer close(events)
		if a.loop == nil {
			events <- AgentEnd{Type: "agent_end", FinishReason: "error", Error: "no loop configured — mount the agent-loop plugin or call SetLoopProvider"}
			return
		}
		runTools := tools
		if runTools == nil {
			runTools = a.tools
		}
		a.loop.Run(ctx, LoopOptions{
			Provider:           a.provider,
			Messages:           messages,
			Tools:              runTools,
			ToolProvider:       a.toolProvider,
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
		})
	}()
	return events
}