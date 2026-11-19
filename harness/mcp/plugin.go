package mcp

import (
	"sync"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/agent"
)

// Plugin adapts the MCP bridge into omega's plugin system. reloadMu serializes reloads
// against each other and is never taken by readers.
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

// NewPluginFromEnv loads MCP config from the environment and creates a plugin.
func NewPluginFromEnv() (*Plugin, error) {
	b, err := NewBridgeFromEnv()
	if err != nil {
		return nil, err
	}
	return NewPlugin(b), nil
}

// activeBridge returns the live bridge under the read lock.
func (p *Plugin) activeBridge() *Bridge {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.bridge
}

// swapBridge installs the fresh bridge and returns the replaced one.
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

// Provides lists the seams this plugin mounts.
func (p *Plugin) ConfigKey() string { return "" }

func (p *Plugin) Provides() []string { return []string{"tools"} }

// Requires lists seams that must be mounted before this plugin.
func (p *Plugin) Requires() []string { return nil }

// MountWithAPI implements agent.APIPlugin. The names are runtime data: server-qualified
// external names ("proof.echo") live beneath the "mcp." namespace guard claimed ONCE
// here - they never pass the per-name namespace claim, so the one-dot law stays
// unweakened while externally-sourced server tools surface under their natural names.
func (p *Plugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	// The listing route mounts even on a nil bridge: an empty config answers an empty list
	// (the empty state is the webui contract), not a 404.
	if err := api.RegisterRoute("GET /mcp/servers", p.handleStatus); err != nil {
		return err
	}
	// The reload route mounts even on a nil bridge too.
	if err := api.RegisterRoute("POST /mcp/reload", p.handleReload); err != nil {
		return err
	}
	// The per-server toggle route rides the same reasoning. Nil-bridge mount keeps the
	// route surface stable (the empty tab's toggle is refused at the wire, never a
	// module-rooted miss).
	if err := api.RegisterRoute("POST /mcp/toggle", p.handleToggle); err != nil {
		return err
	}
	// The entry CRUD routes: POST /mcp/save upserts one full server entry into mcp.yaml.
	// Both answer the fresh StatusReport - the one- response contract the toggle carries.
	// They mount even on a nil bridge: the first add wakes an idle module, exactly like
	// the reload route.
	if err := api.RegisterRoute("POST /mcp/save", p.handleSaveServer); err != nil {
		return err
	}
	if err := api.RegisterRoute("POST /mcp/delete", p.handleDeleteServer); err != nil {
		return err
	}
	// The mcp subcommand: the terminal face of the same entry CRUD. The module owns its
	// argv (v0.12 phase D Q6); every write rides
	// saveServerAndReload/deleteServerAndReload, so `omega mcp add` and a wire save land
	// byte-identically.
	if err := api.RegisterSubcommand("mcp", mcpSummary, cmdMCP(p)); err != nil {
		return err
	}
	// The tool provider registers even on a nil bridge (the reload cycle): Tools follows
	// the live bridge per call - the provider itself never changes after mount, only the
	// bridge swaps beneath it, so an empty bridge and a no-op stay interchangeable.
	return api.RegisterRuntimeTools(bridgeToolProvider{p: p})
}

// bridgeToolProvider adapts Plugin to the core.ToolMap shape.
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
			// the origin stamp is the whole filter contract - the webui Tools tab keys on this
			// exact value, never on dotted-name shape.
			Origin: core.ToolOriginMCP,
		}
	}
	return tools
}

// Toggleable marks the MCP bridge as a module capsule (v0.12 E1).
func (p *Plugin) Toggleable() bool { return true }

// Compile-time assertion that the plugin is a Plugin.
var _ agent.Plugin = (*Plugin)(nil)
