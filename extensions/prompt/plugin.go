package prompt

import (
	"github.com/EndoTheDev/omega/agent"
)

// Plugin is the agent.Plugin adapter for the prompt builder.
// Seam: prompt_builder (exclusive).
type Plugin struct {
	skillsDir string
}

// NewPlugin creates a prompt-builder plugin. skillsDir overrides
// OMEGA_SKILLS_DIR; pass "" to use the env var.
func NewPlugin(skillsDir string) *Plugin {
	return &Plugin{skillsDir: skillsDir}
}

// Name returns the plugin name shown in /extensions.
func (p *Plugin) Name() string { return "prompt" }

// Provides lists the seams this plugin mounts.
func (p *Plugin) Provides() []string { return []string{"prompt_builder"} }

// Requires lists seams that must be mounted before this plugin.
// The prompt builder has no dependencies.
func (p *Plugin) Requires() []string { return nil }

// Mount sets ctx.PromptBuilder to a new PromptBuilder.
func (p *Plugin) Mount(ctx *agent.Context) error {
	ctx.PromptBuilder = NewPromptBuilder(p.skillsDir)
	return nil
}

// Compile-time check: Plugin implements agent.Plugin.
var _ agent.Plugin = (*Plugin)(nil)