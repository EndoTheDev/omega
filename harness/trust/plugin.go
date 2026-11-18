package trust

import (
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/agent"
)

// Plugin adapts the trust Provider to the agent.Plugin interface.
type Plugin struct{}

func NewPlugin() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string { return "trust" }

// Description implements agent.Describer.
func (p *Plugin) Description() string {
	return "trust: gates project context (AGENTS.md) and shell approval"
}

// ConfigKey names the config section this plugin reads from ctx.Configs (see.
func (p *Plugin) ConfigKey() string { return "trust" }

func (p *Plugin) Provides() []string { return []string{"trust"} }
func (p *Plugin) Requires() []string { return nil }

// MountWithAPI registers the trust seam - the module's whole surface: ResolveContext
// gates the AGENTS.md project context behind trust.yaml and State feeds the webui
// status bar, both called by the host (serve, run, runChat) through nil-guarded
// wrappers (cmd/omega/trust.go) before the frontends launch.
func (p *Plugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	cfg, ok := ctx.Configs["trust"].(Config)
	if !ok {
		cfg = Default()
	}
	return api.Offer("trust", NewProvider(cfg.Home))
}

// Toggleable marks this capsule as a module capsule (v0.12 E1, the tier law).
func (p *Plugin) Toggleable() bool { return true }
