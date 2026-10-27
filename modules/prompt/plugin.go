package prompt

import (

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
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

func (p *Plugin) Provides() []string { return []string{"prompt_builder"} }

// Requires lists seams that must be mounted before this plugin. The
// assembler renders sections contributed by other modules at build
// time, so it has no mount-order dependencies.
func (p *Plugin) Requires() []string { return nil }

// MountWithAPI implements agent.APIPlugin: the prompt_builder seam,
// one registration. The builder reads prompt section contributors
// from the Context at build time.
func (p *Plugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	return api.RegisterPromptBuilder(NewPromptBuilder(ctx))
}

// Compile-time check: Plugin implements agent.Plugin.
var _ agent.Plugin = (*Plugin)(nil)