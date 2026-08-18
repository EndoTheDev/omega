package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/EndoTheDev/omega/internal/ai"
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
// set of tools. It consumes provider stream events, executes tool calls,
// and appends results back into the message history.
type Agent struct {
	provider     ai.Provider
	tools        map[string]Tool
	extensions   ExtensionManager
	maxTurns     int
	compaction   *CompactionConfig
	systemPrompt string
	mu           sync.Mutex
	running      bool
}

// NewAgent creates an Agent. A maxTurns <= 0 uses the default cap.
func NewAgent(provider ai.Provider, tools map[string]Tool, maxTurns int) *Agent {
	return &Agent{
		provider:   provider,
		tools:      tools,
		extensions: NoopManager{},
		maxTurns:   maxTurns,
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

// SetSystemPrompt sets the system prompt prepended to every run's
// message history. An empty prompt is ignored.
func (a *Agent) SetSystemPrompt(prompt string) {
	a.systemPrompt = prompt
}

// SetSkills registers a load_skill tool that lets the agent pull in a
// skill's full content on demand. The system prompt advertises skills
// by name and description; this tool gives the agent a way to read the
// actual skill body and its directory path when it needs to follow the
// skill's instructions.
func (a *Agent) SetSkills(skills []Skill) {
	if len(skills) == 0 {
		return
	}
	a.tools["load_skill"] = Tool{
		Description: "Load a skill's full content by name. Returns the skill's markdown body and the directory path where its files (scripts, references, templates) live.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string", "description": "The skill name (from the Available Skills list)"},
			},
			"required": []string{"name"},
		},
		Run: func(ctx context.Context, args map[string]any) (string, error) {
			return runLoadSkill(skills, args)
		},
	}
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
		a.run(ctx, events, messages, tools)
	}()
	return events
}

func (a *Agent) run(ctx context.Context, events chan<- Event, messages []ai.Message, runTools map[string]Tool) {
	defer close(events)

	tools := runTools
	if tools == nil {
		tools = a.tools
	}

	// Merge extension tools into the active tool set. Built-in tools
	// take precedence on name conflict.
	if extTools := a.extensions.Tools(); len(extTools) > 0 {
		merged := make(map[string]Tool, len(tools)+len(extTools))
		for name, t := range tools {
			merged[name] = t
		}
		for name, t := range extTools {
			if _, exists := merged[name]; !exists {
				merged[name] = t
			}
		}
		tools = merged
	}

	maxTurns := a.maxTurns
	if maxTurns <= 0 {
		maxTurns = defaultMaxTurns
	}

	// Prepend the system prompt once, before the loop. It is not
	// persisted to the store; it is injected per run. Extension
	// guidelines are appended to the system prompt.
	if a.systemPrompt != "" {
		prompt := a.systemPrompt
		if guidelines := a.extensions.PromptGuidelines(); len(guidelines) > 0 {
			prompt += "\n## Extension Guidelines\n"
			for _, g := range guidelines {
				prompt += "- " + g + "\n"
			}
		}
		messages = append([]ai.Message{ai.NewSystem(prompt)}, messages...)
	}

	start := AgentStart{Type: "agent_start", ModelName: a.provider.ModelName()}
	events <- start
	a.extensions.DispatchEvent(start)

	turns := 0
	overflowRetries := 0
	for {
		if ctx.Err() != nil {
			end := AgentEnd{Type: "agent_end", Turns: turns, FinishReason: "cancelled", Error: ctx.Err().Error()}
			events <- end
			a.extensions.DispatchEvent(end)
			return
		}
		if turns >= maxTurns {
			end := AgentEnd{Type: "agent_end", Turns: turns, FinishReason: "max_turns"}
			events <- end
			a.extensions.DispatchEvent(end)
			return
		}

		if a.compaction != nil && a.compaction.Enabled {
			if EstimateTokens(messages) > a.compaction.budget() {
				compacted, err := a.compact(ctx, messages, "")
				if err != nil {
					end := AgentEnd{Type: "agent_end", Turns: turns, FinishReason: "error", Error: err.Error()}
					events <- end
					a.extensions.DispatchEvent(end)
					return
				}
				messages = compacted
			}
		}

		turns++
		turnStart := TurnStart{Type: "turn_start", Turn: turns}
		events <- turnStart
		a.extensions.DispatchEvent(turnStart)

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

		if streamErr != "" {
			// A context overflow error triggers one auto-compaction and
			// retry of the turn. The failed attempt counts as a turn and
			// emits TurnStart without TurnEnd - acceptable asymmetry, the
			// retried turn reports its own TurnEnd. Skip the retry when
			// response content was already streamed: the user saw it, and
			// retrying would duplicate it.
			if isOverflowError(streamErr) && a.compaction != nil && a.compaction.Enabled && overflowRetries < maxOverflowRetries && content.Len() == 0 {
				overflowRetries++
				compacted, err := a.compact(ctx, messages, "")
				if err != nil {
					end := AgentEnd{Type: "agent_end", Turns: turns, FinishReason: "error", Error: err.Error()}
					events <- end
					a.extensions.DispatchEvent(end)
					return
				}
				messages = compacted
				continue
			}
			end := AgentEnd{Type: "agent_end", Turns: turns, FinishReason: "error", Error: streamErr}
			events <- end
			a.extensions.DispatchEvent(end)
			return
		}

		assistant := ai.NewAssistant(content.String())
		if thinking.Len() > 0 {
			text := thinking.String()
			assistant.Thinking = &text
		}
		assistant.ToolCalls = toolCalls
		messages = append(messages, assistant)
		assistantEvent := AssistantMessageEvent{Type: "assistant_message", Message: assistant}
		events <- assistantEvent
		a.extensions.DispatchEvent(assistantEvent)

		executed := 0
		for _, call := range toolCalls {
			tool, ok := tools[call.Name]
			if !ok {
				msg := ai.NewToolResult("unknown tool: "+call.Name, call.ID, true)
				messages = append(messages, msg)
				events <- ToolResultEvent{Type: "tool_result", Message: msg}
				a.extensions.DispatchEvent(ToolResultEvent{Type: "tool_result", Message: msg})
				executed++
				continue
			}
			result, err := tool.Run(ctx, call.Arguments)
			var msg ai.ToolResult
			if err != nil {
				msg = ai.NewToolResult(err.Error(), call.ID, true)
			} else {
				if a.compaction != nil && a.compaction.MaxToolOutput > 0 && len(result) > a.compaction.MaxToolOutput {
					result = result[:a.compaction.MaxToolOutput] + fmt.Sprintf("\n... [truncated, %d bytes total]", len(result))
				}
				msg = ai.NewToolResult(result, call.ID, false)
			}
			messages = append(messages, msg)
			toolResultEvent := ToolResultEvent{Type: "tool_result", Message: msg}
			events <- toolResultEvent
			a.extensions.DispatchEvent(toolResultEvent)
			executed++
		}

		turnEnd := TurnEnd{Type: "turn_end", Turn: turns, ToolCalls: executed}
		events <- turnEnd
		a.extensions.DispatchEvent(turnEnd)

		if len(toolCalls) == 0 {
			end := AgentEnd{Type: "agent_end", Turns: turns, FinishReason: finishReason, Message: assistant}
			events <- end
			a.extensions.DispatchEvent(end)
			return
		}
	}
}

// compact wraps CompactWithFocus with an extension customization hook.
// If an extension provides a custom summary, it is used instead of the
// provider-based summarization.
func (a *Agent) compact(ctx context.Context, messages []ai.Message, focus string) ([]ai.Message, error) {
	if a.compaction.KeepFirst+a.compaction.KeepLast >= len(messages) {
		return messages, nil
	}
	if summary, ok := a.extensions.CustomizeCompaction(ctx, messages, focus); ok {
		return BuildCompactedMessages(messages, summary, a.compaction.KeepFirst, a.compaction.KeepLast), nil
	}
	return CompactWithFocus(ctx, a.provider, messages, a.compaction.KeepFirst, a.compaction.KeepLast, focus)
}

// isOverflowError reports whether a provider error indicates the context
// window was exceeded. ponytail: substring match on common provider
// wording. Upgrade path: structured error codes per provider.
func isOverflowError(err string) bool {
	lower := strings.ToLower(err)
	for _, phrase := range []string{"context length", "context_length", "too long", "token limit", "maximum context"} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
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
