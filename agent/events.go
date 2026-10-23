package agent

// Compatibility aliases: these types now live in core (arc 1a,
// t_8c9841ac). Every module import of agent.* keeps compiling; the
// aliases disappear when modules rewire in phase 2.
//
// DO NOT add new types here - new event types belong in core.

import "github.com/EndoTheDev/omega/core"

type (
	// Event is the sealed interface for all agent events.
	Event = core.Event
	// AgentStart is emitted when Run begins.
	AgentStart = core.AgentStart
	// TurnStart is emitted before each provider call.
	TurnStart = core.TurnStart
	// TurnEnd is emitted after a turn completes, including tool execution.
	TurnEnd = core.TurnEnd
	// AgentEnd is emitted when Run finishes.
	AgentEnd = core.AgentEnd
	// StreamEvent wraps an ai stream event forwarded from the provider.
	StreamEvent = core.StreamEvent
	// ToolResultEvent is emitted after a tool executes.
	ToolResultEvent = core.ToolResultEvent
	// AssistantMessageEvent is emitted after a provider turn completes.
	AssistantMessageEvent = core.AssistantMessageEvent
)