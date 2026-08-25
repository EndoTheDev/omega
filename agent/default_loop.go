package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/EndoTheDev/omega/ai"
)

// DefaultLoopProvider is the standard turn-based conversation loop.
// It streams provider responses, executes tool calls, and feeds
// results back into the message history until the provider stops
// calling tools or the turn cap is reached.
type DefaultLoopProvider struct{}

// Run executes the conversation loop. It reads all inputs from opts
// and writes events to opts.Events. The caller is responsible for
// closing the events channel.
func (DefaultLoopProvider) Run(ctx context.Context, opts LoopOptions) error {
	tools := opts.Tools
	if tools == nil {
		tools = map[string]Tool{}
	}

	// Merge tool provider tools. Existing tools take precedence.
	if opts.ToolProvider != nil {
		if providerTools := opts.ToolProvider.Tools(); len(providerTools) > 0 {
			merged := make(map[string]Tool, len(tools)+len(providerTools))
			for name, t := range providerTools {
				merged[name] = t
			}
			for name, t := range tools {
				merged[name] = t
			}
			tools = merged
		}
	}

	// Merge extension tool providers. Existing tools take precedence.
	for _, tp := range opts.ToolProviders {
		if tp == nil {
			continue
		}
		extTools := tp.Tools()
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

	maxTurns := opts.MaxTurns
	if maxTurns <= 0 {
		maxTurns = defaultMaxTurns
	}

	messages := opts.Messages

	// Build the system prompt. The prompt builder can fully replace it.
	// Guidelines are appended to any non-empty prompt.
	prompt := ""
	if opts.PromptBuilder != nil {
		if extPrompt, ok := opts.PromptBuilder.BuildPrompt(ctx, PromptBuildOptions{
			CWD:            opts.CWD,
			Messages:       messages,
			Extensions:     opts.ExtensionInfos,
			ProjectContext: opts.PromptContext,
			Custom:         opts.PromptCustom,
			Append:         opts.PromptAppend,
		}); ok {
			prompt = extPrompt
		}
		if prompt != "" {
			if guidelines := opts.PromptBuilder.Guidelines(); len(guidelines) > 0 {
				prompt += "\n## Extension Guidelines\n"
				for _, g := range guidelines {
					prompt += "- " + g + "\n"
				}
			}
		}
	}
	if prompt != "" {
		messages = append([]ai.Message{ai.NewSystem(prompt)}, messages...)
	}

	start := AgentStart{Type: "agent_start", ModelName: ""}
	if opts.Provider != nil {
		start.ModelName = opts.Provider.ModelName()
	}
	opts.Events <- start

	turns := 0
	overflowRetries := 0
	for {
		if ctx.Err() != nil {
			end := AgentEnd{Type: "agent_end", Turns: turns, FinishReason: "cancelled", Error: ctx.Err().Error()}
			opts.Events <- end
			return nil
		}
		if turns >= maxTurns {
			end := AgentEnd{Type: "agent_end", Turns: turns, FinishReason: "max_turns"}
			opts.Events <- end
			return nil
		}

		if opts.CompactionProvider != nil {
			compacted, err := opts.CompactionProvider.Compact(ctx, messages)
			if err != nil {
				end := AgentEnd{Type: "agent_end", Turns: turns, FinishReason: "error", Error: err.Error()}
				opts.Events <- end
				return nil
			}
			messages = compacted
		}

		turns++
		turnStart := TurnStart{Type: "turn_start", Turn: turns}
		opts.Events <- turnStart

		var content, thinking strings.Builder
		var toolCalls []ai.ToolCall
		finishReason := "stop"
		streamErr := ""

		if opts.Provider == nil {
			end := AgentEnd{Type: "agent_end", Turns: turns, FinishReason: "error", Error: "no provider configured"}
			opts.Events <- end
			return nil
		}

		for event := range opts.Provider.Stream(ctx, messages, toolSchemas(tools)) {
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
			opts.Events <- StreamEvent{Event: event}
		}

		if streamErr != "" {
			// A context overflow error triggers one auto-compaction and
			// retry of the turn. The failed attempt counts as a turn and
			// emits TurnStart without TurnEnd - acceptable asymmetry, the
			// retried turn reports its own TurnEnd. Skip the retry when
			// response content was already streamed: the user saw it, and
			// retrying would duplicate it.
			if isOverflowError(streamErr) && opts.CompactionProvider != nil && overflowRetries < maxOverflowRetries && content.Len() == 0 {
				overflowRetries++
				compacted, err := opts.CompactionProvider.Compact(ctx, messages)
				if err != nil {
					end := AgentEnd{Type: "agent_end", Turns: turns, FinishReason: "error", Error: err.Error()}
					opts.Events <- end
					return nil
				}
				messages = compacted
				continue
			}
			// No compactor loaded: surface a friendly message instead
			// of the raw provider error.
			errMsg := streamErr
			if isOverflowError(streamErr) && opts.CompactionProvider == nil {
				errMsg = "context full — start a new session (/new)"
			}
			end := AgentEnd{Type: "agent_end", Turns: turns, FinishReason: "error", Error: errMsg}
			opts.Events <- end
			return nil
		}

		assistant := ai.NewAssistant(content.String())
		if thinking.Len() > 0 {
			text := thinking.String()
			assistant.Thinking = &text
		}
		assistant.ToolCalls = toolCalls
		messages = append(messages, assistant)
		assistantEvent := AssistantMessageEvent{Type: "assistant_message", Message: assistant}
		opts.Events <- assistantEvent

		// Execute tool calls concurrently. Results are collected in
		// order so the message history stays deterministic.
		type toolResult struct {
			msg ai.ToolResult
		}
		results := make([]toolResult, len(toolCalls))
		var wg sync.WaitGroup
		for i, call := range toolCalls {
			tool, ok := tools[call.Name]
			if !ok {
				results[i] = toolResult{msg: ai.NewToolResult("unknown tool: "+call.Name, call.ID, true)}
				continue
			}
			wg.Add(1)
			go func(idx int, c ai.ToolCall, t Tool) {
				defer wg.Done()
				result, err := t.Run(ctx, c.Arguments)
				var msg ai.ToolResult
				if err != nil {
					msg = ai.NewToolResult(err.Error(), c.ID, true)
				} else {
					if opts.MaxToolOutput > 0 && len(result) > opts.MaxToolOutput {
						result = result[:opts.MaxToolOutput] + fmt.Sprintf("\n... [truncated, %d bytes total]", len(result))
					}
					msg = ai.NewToolResult(result, c.ID, false)
				}
				results[idx] = toolResult{msg: msg}
			}(i, call, tool)
		}
		wg.Wait()

		executed := 0
		for _, r := range results {
			messages = append(messages, r.msg)
			toolResultEvent := ToolResultEvent{Type: "tool_result", Message: r.msg}
			opts.Events <- toolResultEvent
			executed++
		}

		turnEnd := TurnEnd{Type: "turn_end", Turn: turns, ToolCalls: executed}
		opts.Events <- turnEnd

		// Non-blocking: drain all buffered subagent results and
		// batch them into one user message. Runs after every turn
		// (not just when there are no tool calls) so that results
		// are picked up even while the agent polls with delegate.status.
		if opts.InjectedMessages != nil {
			var combined string
			draining := true
			for draining {
				select {
				case msg, ok := <-opts.InjectedMessages:
					if ok {
						if combined != "" {
							combined += "\n\n---\n\n"
						}
						combined += msg.Text
					}
				default:
					draining = false
				}
			}
			if combined != "" {
				messages = append(messages, ai.NewUser(combined))
				continue
			}
		}

		if len(toolCalls) == 0 {
			// One-shot mode (UserInput == nil): block if subagents are
			// still running. TUI mode (UserInput != nil) never blocks —
			// the TUI goroutine handles re-injection.
			if opts.InjectedMessages != nil && opts.UserInput == nil && opts.PendingDelegations != nil && opts.PendingDelegations() > 0 {
				select {
				case msg, ok := <-opts.InjectedMessages:
					if ok {
						messages = append(messages, ai.NewUser(msg.Text))
						continue
					}
				case <-ctx.Done():
					end := AgentEnd{Type: "agent_end", Turns: turns, FinishReason: "cancelled", Error: ctx.Err().Error()}
					opts.Events <- end
					return nil
				}
			}
			end := AgentEnd{Type: "agent_end", Turns: turns, FinishReason: finishReason, Message: assistant}
			opts.Events <- end
			return nil
		}
	}
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