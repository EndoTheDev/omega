package agent_loop

import (
	"fmt"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// Plugin adapts the loop into omega's plugin system. It provides
// the "loop" seam — the agent's turn-based conversation loop. It
// mounts through the ExtensionAPI exclusively (v0.12 phase F - the
// universal registration surface is the only mount path).
type Plugin struct{}

// NewPlugin creates an agent-loop plugin instance.
func NewPlugin() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string { return "agent-loop" }

// Description implements agent.Describer.
func (p *Plugin) Description() string {
	return "the agent loop: model turns, tool dispatch, retries, and streaming"
}

// ConfigKey names the config section this plugin reads from
// ctx.Configs (see agent.Describer). Empty = reads no section.
func (p *Plugin) ConfigKey() string { return "" }

func (p *Plugin) Provides() []string { return []string{"loop"} }
func (p *Plugin) Requires() []string { return nil }

// Mount is the retired classic body: the capsule mounts through the
// ExtensionAPI only (agent.APIPlugin wins the strangler dispatch).
// Reaching it means the registration surface was bypassed - fail loud.
func (p *Plugin) Mount(ctx *core.Context) error {
	return fmt.Errorf("agent-loop: classic mount retired - the capsule mounts through the ExtensionAPI (v0.12 phase F)")
}

// MountWithAPI implements agent.APIPlugin: the loop seam, one
// registration.
func (p *Plugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	return api.RegisterLoop(Loop{})
}