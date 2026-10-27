// Package core holds the engine's shared contracts: the agent event
// stream, the wire/session message shapes, the message vocabulary, the
// provider stream events, and the provider contract. Zero behavior, no
// dependencies outside stdlib (factory-flow ADR, v0.11 arc 1b2).
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
	Type      string `json:"type"`
	Turn      int    `json:"turn"`
	ToolCalls int    `json:"tool_calls"`
}

func (TurnEnd) isEvent() {}

// AgentEnd is emitted when Run finishes. Message carries the final
// assistant response (with thinking) so the TUI can persist it without
// reconstructing from the stream buffer.
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

// ToolResultEvent is emitted after a tool executes, carrying the result
// message so the TUI can persist it to the session store.
type ToolResultEvent struct {
	Type    string     `json:"type"`
	Message ToolResult `json:"message"`
}

func (ToolResultEvent) isEvent() {}

// AssistantMessageEvent is emitted after the provider finishes a turn,
// carrying the assistant message (with thinking and tool calls) so the
// TUI can persist it to the session store.
type AssistantMessageEvent struct {
	Type    string    `json:"type"`
	Message Assistant `json:"message"`
}

func (AssistantMessageEvent) isEvent() {}

// EventObserver is a module subscription on the run event spine (card
// t_72ecca67, M3): mounted by the continuous-agents ledger, runnable by
// any spine consumer the future needs. Observers block ONE goroutine of
// the live run - the tee forwards events to the host consumer only
// after every observer returned - so an observer must be cheap (one
// local sqlite insert, a stat, never network). Observers see the full
// spine: ToolCallEvent (before execution) and ToolResultEvent (after),
// in encounter order.
type EventObserver func(Event)

// TickEvent is a module-emitted spine payload (card t_72ecca67, M4):
// cron owns WHEN and fires a wake tick; continuous owns WHAT and
// consumes it. The v0.12 payload law holds: the emitter namespace is a
// namespaced string, the payload travels as raw JSON, and no consumer
// ever parses another module's Go types.
type TickEvent struct {
	Module  string          `json:"module"`  // the emitting capsule ("cron")
	Kind    string          `json:"kind"`    // the tick vocabulary ("wake")
	Payload json.RawMessage `json:"payload"` // the raw JSON payload
}

func (TickEvent) isEvent() {}

// Tick returns the event's tick data as a TickEvent, or zero when ev is
// not one (single type-assertion site for spine consumers).
func Tick(ev Event) (TickEvent, bool) {
	t, ok := ev.(TickEvent)
	return t, ok
}
