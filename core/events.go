// Package core holds the host's shared contracts: the agent event stream, the
// wire/session message shapes, the message vocabulary, the provider stream events, and
// the provider contract.
package core

import "encoding/json"

// Event is the sealed interface for all agent events.
type Event interface {
	isEvent()
}

type AgentStart struct {
	Type      string `json:"type"`
	ModelName string `json:"model_name"`
}

func (AgentStart) isEvent() {}

type TurnStart struct {
	Type string `json:"type"`
	Turn int    `json:"turn"`
}

func (TurnStart) isEvent() {}

type TurnEnd struct {
	Type         string `json:"type"`
	Turn         int    `json:"turn"`
	ToolCalls    int    `json:"tool_calls"`
	PendingCalls int    `json:"pending_calls,omitempty"`
}

func (TurnEnd) isEvent() {}

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

type ToolResultEvent struct {
	Type    string     `json:"type"`
	Message ToolResult `json:"message"`
}

func (ToolResultEvent) isEvent() {}

type AssistantMessageEvent struct {
	Type    string    `json:"type"`
	Message Assistant `json:"message"`
}

func (AssistantMessageEvent) isEvent() {}

// SteerDeliveredEvent reports one queued input a live turn took at a batch boundary: a
// steer the human typed mid-turn, or a result that settled while the turn ran. ID names
// the source queue row (the surface retires it); the message rides the transcript as
// ordinary content, with no delivery badge.
type SteerDeliveredEvent struct {
	Type    string  `json:"type"`
	ID      string  `json:"id,omitempty"`
	Message Message `json:"message"`
}

func (SteerDeliveredEvent) isEvent() {}

// EventObserver is a module subscription on the run event spine. Observers block ONE
// goroutine of the live run - the tee forwards events to the host consumer only after
// every observer returned - so an observer must be cheap (one local sqlite insert, a
// stat, never network).
type EventObserver func(Event)

// SessionEventObserver is an EventObserver that also receives the session id, which rides
// the RUN CONTEXT (agent.SessionIDFromContext) rather than the event: the events are the
// loop's transport and a session is the host's label. The forward goroutine in
// Agent.RunWithProvider holds that context, so it is the one place that can hand the id to
// a spine consumer. Same contract: cheap, blocking, one goroutine of the live run.
type SessionEventObserver func(sessionID string, ev Event)

// TickEvent is a module-emitted spine payload.
type TickEvent struct {
	Module  string          `json:"module"`  // the emitting capsule ("cron")
	Kind    string          `json:"kind"`    // the tick vocabulary ("wake")
	Payload json.RawMessage `json:"payload"` // the raw JSON payload
}

func (TickEvent) isEvent() {}

func Tick(ev Event) (TickEvent, bool) {
	t, ok := ev.(TickEvent)
	return t, ok
}
