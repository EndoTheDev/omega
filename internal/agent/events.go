package agent

import "github.com/EndoTheDev/omega-dev/internal/ai"

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

// AgentEnd is emitted when Run finishes.
type AgentEnd struct {
	Type         string `json:"type"`
	Turns        int    `json:"turns"`
	FinishReason string `json:"finish_reason"`
	Error        string `json:"error,omitempty"`
}

func (AgentEnd) isEvent() {}

// StreamEvent wraps an ai stream event forwarded from the provider.
type StreamEvent struct {
	Event ai.StreamEvent `json:"-"`
}

func (StreamEvent) isEvent() {}
