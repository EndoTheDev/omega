package agent

import (
	"context"
	"strings"
	"sync"

	"github.com/EndoTheDev/omega-dev/internal/ai"
)

// defaultMaxTurns caps the conversation loop when no explicit cap is set.
const defaultMaxTurns = 10

// Tool is a callable the model may invoke. The map key is the tool name.
type Tool struct {
	Description string
	Parameters  map[string]any
	Run         func(ctx context.Context, args map[string]any) (string, error)
}

// Agent runs the multi-turn conversation loop between a provider and a
// set of tools. It consumes provider stream events, executes tool calls,
// and appends results back into the message history.
type Agent struct {
	provider     ai.Provider
	tools        map[string]Tool
	maxTurns     int
	compaction   *CompactionConfig
	systemPrompt string
	mu           sync.Mutex
	running      bool
}

// NewAgent creates an Agent. A maxTurns <= 0 uses the default cap.
func NewAgent(provider ai.Provider, tools map[string]Tool, maxTurns int) *Agent {
	return &Agent{provider: provider, tools: tools, maxTurns: maxTurns}
}

// SetSystemPrompt sets the system prompt prepended to every run's
// message history. An empty prompt is ignored.
func (a *Agent) SetSystemPrompt(prompt string) {
	a.systemPrompt = prompt
}

// SetCompaction enables context compaction for this agent. A nil config
// disables it.
func (a *Agent) SetCompaction(cfg *CompactionConfig) {
	a.compaction = cfg
}

// ModelName returns the name of the model the agent's provider serves.
func (a *Agent) ModelName() string {
	return a.provider.ModelName()
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
		a.run(ctx, events, messages, tools)
	}()
	return events
}

func (a *Agent) run(ctx context.Context, events chan<- Event, messages []ai.Message, tools map[string]Tool) {
	defer close(events)

	if tools == nil {
		tools = a.tools
	}
	maxTurns := a.maxTurns
	if maxTurns <= 0 {
		maxTurns = defaultMaxTurns
	}

	// Prepend the system prompt once, before the loop. It is not
	// persisted to the store; it is injected per run.
	if a.systemPrompt != "" {
		messages = append([]ai.Message{ai.NewSystem(a.systemPrompt)}, messages...)
	}

	events <- AgentStart{Type: "agent_start", ModelName: a.provider.ModelName()}

	turns := 0
	for {
		if ctx.Err() != nil {
			events <- AgentEnd{Type: "agent_end", Turns: turns, FinishReason: "cancelled", Error: ctx.Err().Error()}
			return
		}
		if turns >= maxTurns {
			events <- AgentEnd{Type: "agent_end", Turns: turns, FinishReason: "max_turns"}
			return
		}

		if a.compaction != nil && a.compaction.Enabled {
			if EstimateTokens(messages) > a.compaction.budget() {
				compacted, err := compact(ctx, a.provider, messages, a.compaction.KeepFirst, a.compaction.KeepLast)
				if err != nil {
					events <- AgentEnd{Type: "agent_end", Turns: turns, FinishReason: "error", Error: err.Error()}
					return
				}
				messages = compacted
			}
		}

		turns++
		events <- TurnStart{Type: "turn_start", Turn: turns}

		var content, thinking strings.Builder
		var toolCalls []ai.ToolCall
		finishReason := "stop"
		streamErr := ""

		for event := range a.provider.Stream(ctx, messages, toolSchemas(tools)) {
			switch e := event.(type) {
			case ai.ResponseChunk:
				content.WriteString(e.Content)
			case ai.ThinkingChunk:
				thinking.WriteString(e.Content)
			case ai.ToolCallEvent:
				toolCalls = append(toolCalls, e.ToolCall)
			case ai.StreamEnd:
				finishReason = e.FinishReason
				streamErr = e.Error
			}
			events <- StreamEvent{Event: event}
		}

		assistant := ai.NewAssistant(content.String())
		if thinking.Len() > 0 {
			text := thinking.String()
			assistant.Thinking = &text
		}
		assistant.ToolCalls = toolCalls
		messages = append(messages, assistant)
		events <- AssistantMessageEvent{Type: "assistant_message", Message: assistant}

		if streamErr != "" {
			events <- AgentEnd{Type: "agent_end", Turns: turns, FinishReason: "error", Error: streamErr}
			return
		}

		executed := 0
		for _, call := range toolCalls {
			tool, ok := tools[call.Name]
			if !ok {
				msg := ai.NewToolResult("unknown tool: "+call.Name, call.ID, true)
				messages = append(messages, msg)
				events <- ToolResultEvent{Type: "tool_result", Message: msg}
				executed++
				continue
			}
			result, err := tool.Run(ctx, call.Arguments)
			var msg ai.ToolResult
			if err != nil {
				msg = ai.NewToolResult(err.Error(), call.ID, true)
			} else {
				msg = ai.NewToolResult(result, call.ID, false)
			}
			messages = append(messages, msg)
			events <- ToolResultEvent{Type: "tool_result", Message: msg}
			executed++
		}

		events <- TurnEnd{Type: "turn_end", Turn: turns, ToolCalls: executed}

		if len(toolCalls) == 0 {
			events <- AgentEnd{Type: "agent_end", Turns: turns, FinishReason: finishReason, Message: assistant}
			return
		}
	}
}

// toolSchemas converts a tools map to the provider schema list.
func toolSchemas(tools map[string]Tool) []ai.ToolSchema {
	if len(tools) == 0 {
		return nil
	}
	result := make([]ai.ToolSchema, 0, len(tools))
	for name, tool := range tools {
		result = append(result, ai.ToolSchema{
			Name:        name,
			Description: tool.Description,
			Parameters:  tool.Parameters,
		})
	}
	return result
}
