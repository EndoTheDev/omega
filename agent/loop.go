package agent

import (
	"context"

	"github.com/EndoTheDev/omega/ai"
)

// AgentLoop drives the multi-turn conversation. The default
// implementation (DefaultAgentLoop) runs the standard turn loop:
// stream provider responses, execute tool calls, feed results back.
// A custom implementation can replace the entire loop logic.
//
// This is a Go-level seam, not an extension JSON-RPC seam. Only Go
// code importing omega as a library can swap the loop.
type AgentLoop interface {
	Run(ctx context.Context, opts LoopOptions) error
}

// LoopOptions carries everything the loop needs to run one agent
// session. The Agent struct builds this from its configured fields.
type LoopOptions struct {
	Provider      ai.Provider
	Messages      []ai.Message
	Tools         map[string]Tool
	ToolProvider  ToolProvider
	PromptBuilder PromptBuilder
	Compactor     Compactor
	Extensions    ExtensionManager
	MaxTurns      int
	MaxToolOutput int
	CWD           string
	Events        chan<- Event
}