package prompt

import (
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/agent"
)

// Plugin is the agent.Plugin adapter for the prompt builder.
// Seam: prompt_builder (exclusive). It mounts through the ExtensionAPI
// exclusively (v0.12 phase F).
type Plugin struct{}

// NewPlugin creates a prompt-builder plugin.
func NewPlugin() *Plugin { return &Plugin{} }

// Name returns the plugin name shown in /extensions.
func (p *Plugin) Name() string { return "prompt" }

// Description implements agent.Describer.
func (p *Plugin) Description() string {
	return "system prompt builder: base prompt plus module prompt sections and user overrides"
}

// Provides lists the seams this plugin mounts.
// ConfigKey names the config section this plugin reads from
// ctx.Configs (see agent.Describer). Empty = reads no section.
func (p *Plugin) ConfigKey() string { return "" }

func (p *Plugin) Provides() []string { return []string{"prompt"} }

// Requires lists seams that must be mounted before this plugin. The
// assembler renders sections contributed by other modules at build
// time, so it has no mount-order dependencies.
func (p *Plugin) Requires() []string { return nil }

// Toggleable opts the prompt builder into the toggleable tier (0.17.3
// tier re-law, card t_ee3a968b: "always on: agent-loop, provider and
// logging; toggleable: the rest"). A disabled builder leaves the loop on
// the documented floor prompt (the provider's own default), which is a
// legitimate configuration.
func (p *Plugin) Toggleable() bool { return true }

// MountWithAPI implements agent.APIPlugin: the prompt_builder seam,
// one registration. The builder reads prompt section contributors
// from the Context at build time.
func (p *Plugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	return api.Offer("prompt", NewPromptBuilder(ctx))
}

// Compile-time check: Plugin implements agent.Plugin.
var _ agent.Plugin = (*Plugin)(nil)
