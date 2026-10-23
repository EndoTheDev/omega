package ai

import "github.com/EndoTheDev/omega/core"

// Compatibility aliases: the provider stream event contract now lives
// in core (arc 1b2, t_d16468fd). Every provider lane and module import
// of ai.* keeps compiling; the aliases disappear when modules rewire
// in phase 2. The sealed interface is named ProviderStreamEvent in
// core because the agent-event contract already carries core.StreamEvent.
//
// DO NOT add new types here - new stream events belong in core.
type (
	// StreamEvent is the sealed interface for all provider stream event
	// types.
	StreamEvent = core.ProviderStreamEvent
	// ThinkingChunk is a chunk of the model's thinking content.
	ThinkingChunk = core.ThinkingChunk
	// ResponseChunk is a chunk of the model's response content.
	ResponseChunk = core.ResponseChunk
	// ToolCallEvent carries a tool call parsed from the stream.
	ToolCallEvent = core.ToolCallEvent
	// StreamEnd is emitted at the end of a stream.
	StreamEnd = core.StreamEnd
)
