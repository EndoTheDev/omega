package agent

import (
	"context"
	"sync"

	"github.com/EndoTheDev/omega/ai"
)

// defaultMaxTurns caps the conversation loop when no explicit cap is set.
const defaultMaxTurns = 10

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
// set of tools. It holds configuration and delegates execution to an
// AgentLoop. Harness concerns (system prompt, compaction) are injected
// via interfaces. The loop itself is swappable via SetAgentLoop.
type Agent struct {
	provider      ai.Provider
	tools         map[string]Tool
	toolProvider  ToolProvider
	extensions    ExtensionManager
	skills        []Skill
	maxTurns      int
	promptBuilder PromptBuilder
	compactor     Compactor
	maxToolOutput int
	cwd           string
	loop          AgentLoop
	mu            sync.Mutex
	running       bool
}

// NewAgent creates an Agent. A maxTurns <= 0 uses the default cap.
// The agent starts with the default agent loop, no prompt builder
// (empty prompt), and no compactor (compaction disabled). Use
// SetPromptBuilder, SetCompactor, and SetAgentLoop to customize.
func NewAgent(provider ai.Provider, tools map[string]Tool, maxTurns int) *Agent {
	return &Agent{
		provider:      provider,
		tools:         tools,
		extensions:    NoopManager{},
		maxTurns:      maxTurns,
		promptBuilder: DefaultPromptBuilder{},
		loop:          DefaultAgentLoop{},
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

// SetPromptBuilder installs the prompt builder. A nil value sets the
// default empty-prompt builder.
func (a *Agent) SetPromptBuilder(pb PromptBuilder) {
	if pb == nil {
		a.promptBuilder = DefaultPromptBuilder{}
		return
	}
	a.promptBuilder = pb
}

// SetCompactor installs the compactor. A nil value disables compaction.
func (a *Agent) SetCompactor(c Compactor) {
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

// SetToolProvider installs a tool provider. When set, the agent merges
// the provider's tools with its own on each Run. A nil value is ignored.
func (a *Agent) SetToolProvider(tp ToolProvider) {
	a.toolProvider = tp
}

// SetSkills stores loaded skills for the system prompt listing.
// The skills.read tool is now provided by the core-tools extension,
// which reads OMEGA_SKILLS_DIR to scan and parse skills on demand.
func (a *Agent) SetSkills(skills []Skill) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.skills = skills
}

// SetAgentLoop installs a custom agent loop. A nil value restores the
// default loop.
func (a *Agent) SetAgentLoop(loop AgentLoop) {
	if loop == nil {
		a.loop = DefaultAgentLoop{}
		return
	}
	a.loop = loop
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
			Provider:      a.provider,
			Messages:      messages,
			Tools:         runTools,
			ToolProvider:  a.toolProvider,
			PromptBuilder: a.promptBuilder,
			Compactor:     a.compactor,
			Extensions:    a.extensions,
			MaxTurns:      a.maxTurns,
			MaxToolOutput: a.maxToolOutput,
			CWD:           a.cwd,
			Events:        events,
		})
	}()
	return events
}