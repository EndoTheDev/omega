package tools

import "github.com/EndoTheDev/omega/agent"

// Plugin adapts ToolProvider into the agent.Plugin interface.
// It registers shell and file tools as an additive tools-seam provider.
type Plugin struct{}

// Name returns the extension name shown in /extensions.
func (p *Plugin) Name() string { return "tools" }

// Provides lists seam names this plugin mounts. "tools" is additive,
// so multiple plugins can contribute tool providers without conflict.
func (p *Plugin) Provides() []string { return []string{"tools"} }

// Requires lists seam names that must be mounted first. tools
// has no dependencies — it only provides tools, which need nothing.
func (p *Plugin) Requires() []string { return nil }

// Mount appends the ToolProvider to ctx.ToolProviders.
func (p *Plugin) Mount(ctx *agent.Context) error {
	ctx.ToolProviders = append(ctx.ToolProviders, &ToolProvider{})
	return nil
}

// NewPlugin creates a new tools plugin.
func NewPlugin() *Plugin { return &Plugin{} }