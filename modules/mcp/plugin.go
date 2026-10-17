package mcp

import (
	"github.com/EndoTheDev/omega/agent"
)

// Plugin adapts the MCP bridge into omega's plugin system. It provides
// the "tools" seam (additive) — discovered MCP server tools are merged
// with other tool providers by the agent loop.
type Plugin struct {
	bridge *Bridge
}

// NewPlugin creates an MCP plugin from an existing Bridge. The bridge
// may be nil if no MCP servers are configured — the plugin is a no-op
// in that case (provides zero tools).
func NewPlugin(b *Bridge) *Plugin {
	return &Plugin{bridge: b}
}

// NewPluginFromEnv loads MCP config from the environment and creates a
// plugin. If no config is found, the plugin is a no-op.
func NewPluginFromEnv() (*Plugin, error) {
	b, err := NewBridgeFromEnv()
	if err != nil {
		return nil, err
	}
	return NewPlugin(b), nil
}

// Name returns the plugin name shown in the /extensions listing.
func (p *Plugin) Name() string { return "mcp" }

// Provides lists the seams this plugin mounts. MCP provides tools
// (additive — multiple plugins can contribute tools).
func (p *Plugin) Provides() []string { return []string{"tools"} }

// Requires lists seams that must be mounted before this plugin.
// MCP has no dependencies on other plugins.
func (p *Plugin) Requires() []string { return nil }

// Mount populates the Context. Appends a ToolProvider that wraps
// the bridge's discovered MCP tools as agent.Tool values.
func (p *Plugin) Mount(ctx *agent.Context) error {
	if p.bridge == nil {
		return nil
	}

	ctx.ToolProviders = append(ctx.ToolProviders, bridgeToolProvider{p.bridge})
	return nil
}

// bridgeToolProvider adapts Bridge to the agent.ToolProvider interface.
type bridgeToolProvider struct {
	bridge *Bridge
}

// Tools returns the MCP-discovered tools as agent.Tool values.
func (b bridgeToolProvider) Tools() map[string]agent.Tool {
	mcpTools := b.bridge.Tools()
	tools := make(map[string]agent.Tool, len(mcpTools))
	for name, t := range mcpTools {
		tools[name] = agent.Tool{
			Description: t.description,
			Parameters:  t.parameters,
			Run:         t.run,
		}
	}
	return tools
}