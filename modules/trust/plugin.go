package trust

import (

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// Plugin adapts the trust Provider to the agent.Plugin interface.
type Plugin struct{}

func NewPlugin() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string { return "trust" }

// Description implements agent.Describer.
func (p *Plugin) Description() string {
	return "trust: gates project context (AGENTS.md) and shell approval"
}

// ConfigKey names the config section this plugin reads from
// ctx.Configs (see agent.Describer). Empty = reads no section.
func (p *Plugin) ConfigKey() string { return "trust" }

func (p *Plugin) Provides() []string { return []string{"trust"} }
func (p *Plugin) Requires() []string { return nil }

// MountWithAPI registers the trust seam - the module's whole surface
// (arc 6, card t_e1efb979): ResolveContext gates the AGENTS.md project
// context behind trust.yaml and State feeds the frontend status bar,
// both called by the host (serve, run, runChat) through nil-guarded
// wrappers (cmd/omega/trust.go) before the frontends launch. No tool,
// no slash command, no REST route, no skills, no canvas panel and no
// module-qualified actions exist, and none did before the arc: the
// frontends consume two strings the host computes from this seam
// (PromptContext + TrustState on FrontendOptions). The CLI flags
// (--approve/--no-approve) parse in the host and ride the same seam;
// the boot-time store migration (trust.Bootstrap, fired by the host's
// home bootstrap) is host wiring around a module-owned function - no
// mounted surface rides it. Take trust.NewPlugin() out of
// buildPlugins() and every face above vanishes; the proof is
// TestTrustRemovalProof (cmd/omega/removal_test.go, eighth).
func (p *Plugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	cfg, ok := ctx.Configs["trust"].(Config)
	if !ok {
		cfg = Default()
	}
	return api.RegisterTrust(NewProvider(cfg.Home))
}

// Toggleable marks this capsule as a module capsule (v0.12 E1, the
// tier law): the omega.json disable list may name it.
func (p *Plugin) Toggleable() bool { return true }
