package mcp

import (
	"sync"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/engine/agent"
)

// Plugin adapts the MCP bridge into omega's plugin system. It provides
// the "tools" seam (additive) — discovered MCP server tools are merged
// with other tool providers by the agent loop. It mounts through the
// ExtensionAPI exclusively (v0.12 phase F).
//
// The bridge is a swappable slot, not a fixed field (the reload cycle,
// card t_d6e94e79): POST /mcp/reload re-reads the config and swaps in a
// fresh Bridge. mu guards that swap - every reader (Tools(), the
// status route) takes it per call; no lock is held across the whole
// reload sequence. reloadMu serializes reloads against each other and
// is never taken by readers.
type Plugin struct {
	mu       sync.RWMutex
	reloadMu sync.Mutex
	bridge   *Bridge
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

// activeBridge returns the live bridge under the read lock (nil still
// possible - the no-op shape).
func (p *Plugin) activeBridge() *Bridge {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.bridge
}

// swapBridge installs the fresh bridge and returns the replaced one
// (the caller disconnects its servers outside the lock).
func (p *Plugin) swapBridge(b *Bridge) *Bridge {
	p.mu.Lock()
	defer p.mu.Unlock()
	old := p.bridge
	p.bridge = b
	return old
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
// provider - every discovered MCP server tool, server-qualified - ships
// from here, plus the module's two routes: the read-only inventory the
// webui MCP tab lists (GET /mcp/servers, card t_c7136c50) and the
// reload the tab's refresh button drives (POST /mcp/reload, card
// t_d6e94e79 - mcp.yaml edits go live on refresh, no server restart).
// No command, no skills, no canvas panel. The env/mcp.yaml config load
// lives in the module too; the host's construction falls back to a
// no-op bridge on config failure. Removal proof: TestMCPRemovalProof
// (cmd/omega/removal_test.go).
//
// The names are runtime data (the mcp ruling, card t_6c0375b5):
// server-qualified external names ("proof.echo") live beneath the
// "mcp." namespace guard claimed ONCE here - they never pass the
// per-name namespace claim, so the one-dot law stays unweakened while
// externally-sourced server tools surface under their natural names.
// Binary purity: removing the module removes the guard and all of
// them.
func (p *Plugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	// The listing route mounts even on a nil bridge: an empty config
	// answers an empty list (the empty state is the webui contract),
	// not a 404. The route is the module's own namespace ("/mcp"),
	// tenant-checked by the registry.
	if err := api.RegisterRoute("GET /mcp/servers", p.handleStatus); err != nil {
		return err
	}
	// The reload route (card t_d6e94e79) mounts even on a nil bridge
	// too: a module idle at boot (no config found) wakes up on the
	// first reload - the refresh button is the whole trigger.
	if err := api.RegisterRoute("POST /mcp/reload", p.handleReload); err != nil {
		return err
	}
	// The tool provider registers even on a nil bridge (the reload
	// cycle): Tools() follows the live bridge per call - the provider
	// itself never changes after mount, only the bridge swaps beneath
	// it, so an empty bridge and a no-op stay interchangeable.
	return api.RegisterRuntimeTools(bridgeToolProvider{p: p})
}

// bridgeToolProvider adapts Plugin to the core.ToolMap shape. The map
// is assembled at registration; the provider it lands in re-reads the
// LIVE bridge's discovered tools on every Tools() call - a reload
// swaps the bridge, the next read serves the new tool set.
type bridgeToolProvider struct {
	p *Plugin
}

// Tools returns the MCP-discovered tools as core.Tool values.
func (b bridgeToolProvider) Tools() map[string]core.Tool {
	active := b.p.activeBridge()
	if active == nil {
		return map[string]core.Tool{}
	}
	mcpTools := active.Tools()
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
