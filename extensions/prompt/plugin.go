package prompt

import (
	"github.com/EndoTheDev/omega/agent"
)

// Plugin is the agent.Plugin adapter for the prompt builder.
// Seam: prompt_builder (exclusive).
type Plugin struct{}

// NewPlugin creates a prompt-builder plugin.
func NewPlugin() *Plugin { return &Plugin{} }

// Name returns the plugin name shown in /extensions.
func (p *Plugin) Name() string { return "prompt" }

// Provides lists the seams this plugin mounts.
func (p *Plugin) Provides() []string { return []string{"prompt_builder"} }

// Requires lists seams that must be mounted before this plugin.
// The prompt builder needs memory for snapshot injection and skills
// for the Available Skills section.
func (p *Plugin) Requires() []string { return []string{"memory", "skills"} }

// Mount sets ctx.PromptBuilder. The memory provider is passed for
// snapshot injection; the skills provider is passed for skill listing.
func (p *Plugin) Mount(ctx *agent.Context) error {
	ctx.PromptBuilder = NewPromptBuilder(ctx.Skills, ctx.Memory)
	return nil
}

// Compile-time check: Plugin implements agent.Plugin.
var _ agent.Plugin = (*Plugin)(nil)
