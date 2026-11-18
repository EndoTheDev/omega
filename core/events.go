// Package core holds the host's shared contracts: the agent event stream, the
// wire/session message shapes, the message vocabulary, the provider stream events, and
// the provider contract.
package core

import "encoding/json"

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
	Type         string `json:"type"`
	Turn         int    `json:"turn"`
	ToolCalls    int    `json:"tool_calls"`
	PendingCalls int    `json:"pending_calls,omitempty"`
}

func (TurnEnd) isEvent() {}

// AgentEnd is emitted when Run finishes.
type AgentEnd struct {
	Type         string    `json:"type"`
	Turns        int       `json:"turns"`
	FinishReason string    `json:"finish_reason"`
	Error        string    `json:"error,omitempty"`
	Message      Assistant `json:"message,omitempty"`
}

func (AgentEnd) isEvent() {}

// StreamEvent wraps an ai stream event forwarded from the provider.
type StreamEvent struct {
	Event ProviderStreamEvent `json:"-"`
}

func (StreamEvent) isEvent() {}

// ToolResultEvent is emitted after a tool executes, carrying the result message.
type ToolResultEvent struct {
	Type    string     `json:"type"`
	Message ToolResult `json:"message"`
}

func (ToolResultEvent) isEvent() {}

// AssistantMessageEvent is emitted after the provider finishes a turn, carrying.
type AssistantMessageEvent struct {
	Type    string    `json:"type"`
	Message Assistant `json:"message"`
}

func (AssistantMessageEvent) isEvent() {}

// EventObserver is a module subscription on the run event spine. Observers block ONE
// goroutine of the live run - the tee forwards events to the host consumer only after
// every observer returned - so an observer must be cheap (one local sqlite insert, a
// stat, never network).
type EventObserver func(Event)

// SessionEventObserver is an EventObserver that also receives the id of the session.
// The plain observer shape cannot attribute an event to a conversation - the session
// rides the RUN CONTEXT (agent.SessionIDFromContext), never the event payload, because
// the events are the loop's transport and a session is the host's label. The forward
// goroutine in Agent.RunWithProvider holds that context, so it is the one place that
// can hand the id to a spine consumer; a consumer that must scope by session subscribes
// through this shape instead. Same contract as EventObserver: cheap, blocking, one
// goroutine of the live run.
type SessionEventObserver func(sessionID string, ev Event)

// TickEvent is a module-emitted spine payload.
type TickEvent struct {
	Module  string          `json:"module"`  // the emitting capsule ("cron")
	Kind    string          `json:"kind"`    // the tick vocabulary ("wake")
	Payload json.RawMessage `json:"payload"` // the raw JSON payload
}

func (TickEvent) isEvent() {}

// Tick returns the event's tick data as a TickEvent, or zero when ev is not one.
func Tick(ev Event) (TickEvent, bool) {
	t, ok := ev.(TickEvent)
	return t, ok
}
