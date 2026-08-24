package agent

import (
	"context"
	"sync"

	"github.com/EndoTheDev/omega/ai"
)

// defaultMaxTurns caps the conversation loop when no explicit cap is set.
const defaultMaxTurns = 100

// maxOverflowRetries caps how many times a turn is retried after a
// context overflow error; a second overflow surfaces the error.
// ponytail: fixed cap like the compaction threshold; upgrade path:
// expose as a config knob next to compaction settings.
const maxOverflowRetries = 1

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
type Agent struct {
	provider      ai.Provider
	tools         map[string]Tool
	toolProvider  ToolProvider
	extensions    ExtensionManager
	maxTurns      int
	compactor     CompactionProvider
	maxToolOutput int
	cwd           string
	promptCustom  string
	promptAppend  []string
	promptContext string
	userInput     chan string
	loop          LoopProvider
	mu            sync.Mutex
	running       bool
}

// NewAgent creates an Agent. A maxTurns <= 0 uses the default cap.
// The agent starts with the default agent loop and no compactor
// (compaction disabled). Use SetProvider, SetCompactionProvider, and
// SetLoopProvider to customize. The provider may be nil if it will be
// set later via SetProvider.
func NewAgent(provider ai.Provider, tools map[string]Tool, maxTurns int) *Agent {
	return &Agent{
		provider:   provider,
		tools:      tools,
		extensions: NoopManager{},
		maxTurns:   maxTurns,
		loop:       DefaultLoopProvider{},
	}
}

// SetExtensions installs the extension manager. A nil value sets the
// default no-op manager.
func (a *Agent) SetExtensions(mgr ExtensionManager) {
	if mgr == nil {
		a.extensions = NoopManager{}
		return
	}
	a.extensions = mgr
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

// SetLoopProvider installs a custom agent loop. A nil value restores
// the default loop.
func (a *Agent) SetLoopProvider(loop LoopProvider) {
	if loop == nil {
		a.loop = DefaultLoopProvider{}
		return
	}
	a.loop = loop
}

// SetUserInput sets a channel for receiving user messages while the
// agent loop is running. The TUI uses this so the user can chat while
// subagents are running. One-shot mode (omega run) leaves it nil.
func (a *Agent) SetUserInput(ch chan string) {
	a.userInput = ch
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
		runTools := tools
		if runTools == nil {
			runTools = a.tools
		}
		a.loop.Run(ctx, LoopOptions{
			Provider:        a.provider,
			Messages:        messages,
			Tools:           runTools,
			ToolProvider:    a.toolProvider,
			CompactionProvider: a.compactor,
			Extensions:      a.extensions,
			MaxTurns:        a.maxTurns,
			MaxToolOutput:   a.maxToolOutput,
			CWD:             a.cwd,
			PromptCustom:    a.promptCustom,
			PromptAppend:    a.promptAppend,
			PromptContext:   a.promptContext,
			Events:           events,
			InjectedMessages: a.extensions.InjectedMessages(),
			UserInput:        a.userInput,
		})
	}()
	return events
}