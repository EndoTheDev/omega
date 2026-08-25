package compactor

import (
	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/gateway"
)

// Plugin adapts the compactor to the agent.Plugin interface.
type Plugin struct{}

func (p *Plugin) Name() string       { return "compactor" }
func (p *Plugin) Provides() []string { return []string{"compactor"} }
func (p *Plugin) Requires() []string { return []string{"provider"} }

// Mount reads CompactionConfig from ctx.Config (type-asserted to
// gateway.Config), wires the provider from the already-mounted
// provider slot, and sets ctx.Compactor.
func (p *Plugin) Mount(ctx *agent.Context) error {
	cfg, ok := ctx.Config.(gateway.Config)
	if ok {
		ctx.Compactor = &Compactor{
			provider: ctx.Provider,
			config:   cfg.Compaction,
		}
	} else {
		// No config: compaction disabled.
		ctx.Compactor = &Compactor{
			provider: ctx.Provider,
		}
	}
	return nil
}

// NewPlugin creates a compactor plugin instance.
func NewPlugin() *Plugin { return &Plugin{} }