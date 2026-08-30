package trust

import (
	"github.com/EndoTheDev/omega/agent"
)

// Plugin adapts the trust Provider to the agent.Plugin interface.
type Plugin struct{}

func NewPlugin() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string       { return "trust" }
func (p *Plugin) Provides() []string { return []string{"trust"} }
func (p *Plugin) Requires() []string { return nil }

func (p *Plugin) Mount(ctx *agent.Context) error {
	cfg, ok := ctx.Configs["trust"].(Config)
	if !ok {
		cfg = Default()
	}
	ctx.Trust = NewProvider(cfg.Home)
	return nil
}
