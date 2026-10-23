package agent

import (
	"github.com/EndoTheDev/omega/core"
)

// Re-export shims (arc 1b, t_7bb4790d): the tool types and the Agent
// engine moved to core alongside the seam contracts (Context, CronDeps
// and LoopOptions reference them, and the loop engine's own struct is
// seam-adjacent). Every module import of agent.* keeps compiling; the
// aliases disappear when modules rewire in phase 2.
//
// DO NOT add new types here - new engine types belong in core.

type (
	// ToolAction is one named sub-action of a domain tool.
	ToolAction = core.ToolAction
	// Tool is a callable the model may invoke. The map key is the tool name.
	Tool = core.Tool
	// Agent runs the multi-turn conversation loop (the wiring container;
	// execution delegates to the LoopProvider seam).
	Agent = core.Agent
	// AgentOptions carries the host-level settings NewFromContext cannot
	// get from the Context itself.
	AgentOptions = core.AgentOptions
)

// NewAgent creates an Agent. A maxTurns <= 0 uses the default cap.
var NewAgent = core.NewAgent

// NewFromContext creates a fully-wired Agent from a mounted Context.
// Channels, newAgent, and the TUI use this.
var NewFromContext = core.NewFromContext