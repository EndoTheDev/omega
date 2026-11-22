package prompt

import (
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/agent"
)

// Plugin is the agent.Plugin adapter for the prompt builder.
type Plugin struct{}

// NewPlugin creates a prompt-builder plugin.
func NewPlugin() *Plugin { return &Plugin{} }

// Name returns the plugin name shown in /extensions.
func (p *Plugin) Name() string { return "prompt" }

// Description implements agent.Describer.
func (p *Plugin) Description() string {
	return "system prompt builder: base prompt plus module prompt sections and user overrides"
}

func (p *Plugin) ConfigKey() string { return "" }

// Provides lists the seams this plugin mounts.
func (p *Plugin) Provides() []string { return []string{"prompt"} }

func (p *Plugin) Requires() []string { return nil }

func (p *Plugin) Toggleable() bool { return true }

// MountWithAPI implements agent.APIPlugin: the prompt_builder seam, one registration.
func (p *Plugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	return api.Offer("prompt", NewPromptBuilder(ctx))
}

var _ agent.Plugin = (*Plugin)(nil)
