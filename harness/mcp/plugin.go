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

// NewPlugin creates an MCP plugin from an existing Bridge. A nil bridge makes the plugin
// a no-op that provides zero tools.
func NewPlugin(b *Bridge) *Plugin {
	return &Plugin{bridge: b}
}

func NewPluginFromEnv() (*Plugin, error) {
	b, err := NewBridgeFromEnv()
	if err != nil {
		return nil, err
	}
	return NewPlugin(b), nil
}

func (p *Plugin) activeBridge() *Bridge {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.bridge
}

func (p *Plugin) swapBridge(b *Bridge) *Bridge {
	p.mu.Lock()
	defer p.mu.Unlock()
	old := p.bridge
	p.bridge = b
	return old
}

func (p *Plugin) Name() string { return "mcp" }

// Description implements agent.Describer.
func (p *Plugin) Description() string {
	return "MCP bridge: tools from configured Model Context Protocol servers"
}

func (p *Plugin) ConfigKey() string { return "" }

func (p *Plugin) Provides() []string { return []string{"tools"} }

func (p *Plugin) Requires() []string { return nil }

// MountWithAPI implements agent.APIPlugin. Server-qualified external tool names
// ("proof.echo") live beneath the "mcp." namespace guard claimed ONCE here, so the
// one-dot law stays unweakened while they surface under their natural names.
func (p *Plugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	// The listing route mounts even on a nil bridge: an empty config answers an empty list
	// (the empty state is the webui contract), not a 404.
	if err := api.RegisterRoute("GET /mcp/servers", p.handleStatus); err != nil {
		return err
	}
	if err := api.RegisterRoute("POST /mcp/reload", p.handleReload); err != nil {
		return err
	}
	// Nil-bridge mount keeps the route surface stable: the empty tab's toggle is refused at
	// the wire, never a module-rooted miss.
	if err := api.RegisterRoute("POST /mcp/toggle", p.handleToggle); err != nil {
		return err
	}
	// POST /mcp/save upserts one full server entry into mcp.yaml. Both entry routes answer
	// the fresh StatusReport (the one-response contract the toggle carries) and mount even
	// on a nil bridge: the first add wakes an idle module, like the reload route.
	if err := api.RegisterRoute("POST /mcp/save", p.handleSaveServer); err != nil {
		return err
	}
	if err := api.RegisterRoute("POST /mcp/delete", p.handleDeleteServer); err != nil {
		return err
	}
	// The mcp subcommand is the terminal face of the same entry CRUD: every write rides
	// saveServerAndReload/deleteServerAndReload, so `omega mcp add` and a wire save land
	// byte-identically.
	if err := api.RegisterSubcommand("mcp", mcpSummary, cmdMCP(p)); err != nil {
		return err
	}
	// The tool provider registers even on a nil bridge: Tools follows the live bridge per
	// call, so the provider never changes after mount and only the bridge swaps beneath it.
	return api.RegisterRuntimeTools(bridgeToolProvider{p: p})
}

type bridgeToolProvider struct {
	p *Plugin
}

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
			// the origin stamp is the whole filter contract: the webui Tools tab keys on this
			// exact value, never on dotted-name shape.
			Origin: core.ToolOriginMCP,
		}
	}
	return tools
}

func (p *Plugin) Toggleable() bool { return true }

var _ agent.Plugin = (*Plugin)(nil)
