package loop

import (
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/agent"
)

// Plugin adapts the loop into omega's plugin system.
type Plugin struct{}

// NewPlugin creates an agent-loop plugin instance.
func NewPlugin() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string { return "agent-loop" }

// Description implements agent.Describer.
func (p *Plugin) Description() string {
	return "the agent loop: model turns, tool dispatch, retries, and streaming"
}

// ConfigKey names the config section this plugin reads from ctx.Configs (see.
func (p *Plugin) ConfigKey() string { return "" }

func (p *Plugin) Provides() []string { return []string{"loop"} }
func (p *Plugin) Requires() []string { return nil }

// MountWithAPI implements agent.APIPlugin: the loop seam, one registration.
func (p *Plugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	return api.Offer("loop", Loop{})
}
