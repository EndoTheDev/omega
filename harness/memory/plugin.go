package memory

import (
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/agent"
)

// Plugin implements agent.Plugin for the memory seam.
type Plugin struct{}

// Compile-time assertion that *Plugin implements agent.Plugin.
var _ agent.Plugin = (*Plugin)(nil)

func (p *Plugin) Name() string { return "memory" }

// Description implements agent.Describer.
func (p *Plugin) Description() string {
	return "durable memory: facts saved and recalled across sessions by the agent"
}

// ConfigKey names the config section this plugin reads from ctx.Configs.
func (p *Plugin) ConfigKey() string { return "memory" }

func (p *Plugin) Provides() []string { return []string{"memory", "tools", "prompt_sections"} }
func (p *Plugin) Requires() []string { return nil }

// MountWithAPI implements agent.APIPlugin.
func (p *Plugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	cfg := Default()
	if c, ok := ctx.Configs["memory"].(Config); ok {
		cfg = c
	}
	fm := NewFileMemory(cfg)
	if err := api.Offer("memory", fm); err != nil {
		return err
	}
	for name, tool := range (&memoryToolProvider{mem: fm}).Tools() {
		if err := api.RegisterTool(name, tool); err != nil {
			return err
		}
	}
	return api.RegisterPromptSectioner(fm)
}

// NewPlugin creates a memory plugin instance.
func NewPlugin() *Plugin { return &Plugin{} }

// Toggleable marks this capsule as a module capsule (v0.12 E1, the tier law).
func (p *Plugin) Toggleable() bool { return true }
