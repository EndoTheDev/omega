package agent_loop

import (
	"github.com/EndoTheDev/omega/agent"
)

// Plugin adapts the loop into omega's plugin system. It provides
// the "loop" seam — the agent's turn-based conversation loop.
type Plugin struct{}

// NewPlugin creates an agent-loop plugin instance.
func NewPlugin() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string       { return "agent-loop" }
func (p *Plugin) Provides() []string { return []string{"loop"} }
func (p *Plugin) Requires() []string { return nil }

// Mount sets ctx.Loop to the default loop implementation.
func (p *Plugin) Mount(ctx *agent.Context) error {
	ctx.Loop = Loop{}
	return nil
}