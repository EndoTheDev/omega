package mcp

import (
	"github.com/EndoTheDev/omega/core"
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

// Description implements agent.Describer.
func (p *Plugin) Description() string {
	return "MCP bridge: tools from configured Model Context Protocol servers"
}

// Provides lists the seams this plugin mounts. MCP provides tools
// (additive — multiple plugins can contribute tools).
// ConfigKey names the config section this plugin reads from
// ctx.Configs (see agent.Describer). Empty = reads no section.
func (p *Plugin) ConfigKey() string { return "" }

func (p *Plugin) Provides() []string { return []string{"tools"} }

// Requires lists seams that must be mounted before this plugin.
// MCP has no dependencies on other plugins.
func (p *Plugin) Requires() []string { return nil }

// Mount populates the Context. Appends a ToolProvider that wraps
// the bridge's discovered MCP tools as core.Tool values.
//
// This Mount is the module's whole surface (arc 6 capsule, card
// t_cf6b7795): the bridge tool provider - every discovered MCP server
// tool, server-qualified - ships from here, and the module ships
// nothing else - no command, no REST route, no skills, no canvas panel
// (nothing rendered mcp before the arc). The env/mcp.yaml config load
// lives in the module too; buildPlugins' construction falls back to a
// no-op bridge on config failure. Removal proof:
// TestMCPRemovalProof (cmd/omega/removal_test.go).
func (p *Plugin) Mount(ctx *core.Context) error {
	if p.bridge == nil {
		return nil
	}

	ctx.ToolProviders = append(ctx.ToolProviders, bridgeToolProvider{p.bridge})
	return nil
}

// bridgeToolProvider adapts Bridge to the core.ToolProvider interface.
type bridgeToolProvider struct {
	bridge *Bridge
}

// Tools returns the MCP-discovered tools as core.Tool values.
func (b bridgeToolProvider) Tools() map[string]core.Tool {
	mcpTools := b.bridge.Tools()
	tools := make(map[string]core.Tool, len(mcpTools))
	for name, t := range mcpTools {
		tools[name] = core.Tool{
			Description: t.description,
			Parameters:  t.parameters,
			Run:         t.run,
		}
	}
	return tools
}

// Toggleable marks the MCP bridge as a module capsule (v0.12 E1): the
// omega.json disable list may name it.
func (p *Plugin) Toggleable() bool { return true }

// Dismount unmounts the bridge tool provider (the E1 dismount proof).
// The bridge process is the caller's concern - the plugin that opened
// it owns closing it.
func (p *Plugin) Dismount(ctx *core.Context) error {
	tools := ctx.ToolProviders[:0]
	for _, tp := range ctx.ToolProviders {
		if _, is := tp.(bridgeToolProvider); is {
			continue
		}
		tools = append(tools, tp)
	}
	ctx.ToolProviders = tools
	return nil
}
