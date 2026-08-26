package agent

import "github.com/EndoTheDev/omega/ai"

// Event is the sealed interface for all agent events.
type Event interface {
	isEvent()
}

// AgentStart is emitted when Run begins.
type AgentStart struct {
	Type      string `json:"type"`
	ModelName string `json:"model_name"`
}

func (AgentStart) isEvent() {}

// TurnStart is emitted before each provider call.
type TurnStart struct {
	Type string `json:"type"`
	Turn int    `json:"turn"`
}

func (TurnStart) isEvent() {}

// TurnEnd is emitted after a turn completes, including tool execution.
type TurnEnd struct {
	Type      string `json:"type"`
	Turn      int    `json:"turn"`
	ToolCalls int    `json:"tool_calls"`
}

func (TurnEnd) isEvent() {}

// AgentEnd is emitted when Run finishes. Message carries the final
// assistant response (with thinking) so the TUI can persist it without
// reconstructing from the stream buffer.
type AgentEnd struct {
	Type         string        `json:"type"`
	Turns        int           `json:"turns"`
	FinishReason string        `json:"finish_reason"`
	Error        string        `json:"error,omitempty"`
	Message      ai.Assistant  `json:"message,omitempty"`
}

func (AgentEnd) isEvent() {}

// StreamEvent wraps an ai stream event forwarded from the provider.
type StreamEvent struct {
	Event ai.StreamEvent `json:"-"`
}

func (StreamEvent) isEvent() {}

// ToolResultEvent is emitted after a tool executes, carrying the result
// message so the TUI can persist it to the session store.
type ToolResultEvent struct {
	Type    string        `json:"type"`
	Message ai.ToolResult `json:"message"`
}

func (ToolResultEvent) isEvent() {}

// AssistantMessageEvent is emitted after the provider finishes a turn,
// carrying the assistant message (with thinking and tool calls) so the
// TUI can persist it to the session store.
type AssistantMessageEvent struct {
	Type    string       `json:"type"`
	Message ai.Assistant `json:"message"`
}

func (AssistantMessageEvent) isEvent() {}
