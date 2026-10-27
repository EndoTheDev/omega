package mcp

import (
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/engine/agent"
)

// Plugin adapts the MCP bridge into omega's plugin system. It provides
// the "tools" seam (additive) — discovered MCP server tools are merged
// with other tool providers by the agent loop. It mounts through the
// ExtensionAPI exclusively (v0.12 phase F).
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

// MountWithAPI implements agent.APIPlugin. This Mount is the module's
// whole surface (arc 6 capsule, card t_cf6b7795): the bridge tool
// provider - every discovered MCP server tool, server-qualified -
// ships from here, and the module ships nothing else - no command, no
// REST route, no skills, no canvas panel. The env/mcp.yaml config load
// lives in the module too; the host's construction falls back to a
// no-op bridge on config failure. Removal proof:
// TestMCPRemovalProof (cmd/omega/removal_test.go).
//
// The names are runtime data (the mcp ruling, card t_6c0375b5):
// server-qualified external names ("proof.echo") live beneath the
// "mcp." namespace guard claimed ONCE here - they never pass the
// per-name namespace claim, so the one-dot law stays unweakened while
// externally-sourced server tools surface under their natural names.
// Binary purity: removing the module removes the guard and all of
// them.
func (p *Plugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	if p.bridge == nil {
		// No servers configured: claim nothing, register nothing -
		// the no-op shape the classic nil bridge had.
		return nil
	}
	return api.RegisterRuntimeTools(bridgeToolProvider{p.bridge})
}

// bridgeToolProvider adapts Bridge to the core.ToolMap shape. The map
// is assembled at registration; the provider it lands in re-reads the
// bridge's discovered tools on every Tools() call.
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
// config.yaml disabled: list may name it.
func (p *Plugin) Toggleable() bool { return true }

// Compile-time assertion that the plugin is a Plugin.
var _ agent.Plugin = (*Plugin)(nil)
