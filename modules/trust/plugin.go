package trust

import (
	"github.com/EndoTheDev/omega/core"
)

// Plugin adapts the trust Provider to the agent.Plugin interface.
type Plugin struct{}

func NewPlugin() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string       { return "trust" }

// Description implements agent.Describer.
func (p *Plugin) Description() string {
	return "trust: gates project context (AGENTS.md) and shell approval"
}
// ConfigKey names the config section this plugin reads from
// ctx.Configs (see agent.Describer). Empty = reads no section.
func (p *Plugin) ConfigKey() string { return "trust" }

func (p *Plugin) Provides() []string { return []string{"trust"} }
func (p *Plugin) Requires() []string { return nil }

func (p *Plugin) Mount(ctx *core.Context) error {
	cfg, ok := ctx.Configs["trust"].(Config)
	if !ok {
		cfg = Default()
	}
	ctx.Trust = NewProvider(cfg.Home)
	return nil
}
