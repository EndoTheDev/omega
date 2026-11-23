// Inventory tests: a reload follows the bridge into the ExtensionInfo snapshot - the list the
// Tools section and the extensions listing read - instead of leaving the mount-time one.
package mcp

import (
	"context"
	"reflect"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// probeChannel is the serving surface these tests drive: its re-wire does what the http
// channel does after a toggle - re-read the mounted Context into the agent's inventory.
type probeChannel struct{ ag *agent.Agent }

func (c *probeChannel) Start(context.Context, core.ChannelDeps) error { return nil }
func (c *probeChannel) Stop() error                                   { return nil }

func (c *probeChannel) RewireSeams(ctx *core.Context) error {
	c.ag.RefreshExtensionInfos(ctx)
	return nil
}

// mountInventoryPlugin mounts the plugin the way the host does: the API mount, then the fence
// start that binds the live Context a reload re-wires through.
func mountInventoryPlugin(t *testing.T, p *Plugin) (*core.Context, *agent.Agent) {
	t.Helper()
	ctx := &core.Context{}
	if _, err := agent.MountAllAPI([]agent.Plugin{p}, ctx, nil, nil); err != nil {
		t.Fatalf("api mount: %v", err)
	}
	ag := agent.NewFromContext(ctx, agent.AgentOptions{})
	ctx.Channels = append(ctx.Channels, &probeChannel{ag: ag})
	if err := ctx.RunGroup().StartAll(context.Background(), core.StartDeps{Ctx: ctx, Agent: ag}, ""); err != nil {
		t.Fatalf("fence start: %v", err)
	}
	return ctx, ag
}

func names(t *testing.T, infos []core.ExtensionInfo) []string {
	t.Helper()
	for _, info := range infos {
		if info.Name != "mcp" {
			continue
		}
		out := make([]string, 0, len(info.ToolList))
		for _, ti := range info.ToolList {
			out = append(out, ti.Name)
		}
		return out
	}
	t.Fatalf("no mcp inventory entry in %+v", infos)
	return nil
}

func TestReloadRebuildsTheToolSnapshot(t *testing.T) {
	alpha := mockMCPServer(t, "alpha-session")
	beta := mockMCPServer(t, "beta-session")
	t.Setenv("OMEGA_HOME", "")
	t.Setenv("MCP_SERVERS",
		`{"servers":[{"name":"alpha","url":"`+alpha.URL+`"},{"name":"beta","url":"`+beta.URL+`"}]}`)

	p, err := NewPluginFromEnv()
	if err != nil {
		t.Fatalf("NewPluginFromEnv: %v", err)
	}
	ctx, ag := mountInventoryPlugin(t, p)
	both := []string{"alpha.echo", "beta.echo"}
	if got := names(t, ctx.Inventory()); !reflect.DeepEqual(got, both) {
		t.Fatalf("mount inventory: got %v, want %v", got, both)
	}
	if got := names(t, ag.ExtensionInfos()); !reflect.DeepEqual(got, both) {
		t.Fatalf("mount snapshot: got %v, want %v", got, both)
	}

	t.Setenv("MCP_SERVERS", `{"servers":[{"name":"alpha","url":"`+alpha.URL+`"}]}`)
	if w := serveRoute(t, ctx, "POST /mcp/reload", "POST", "/mcp/reload"); w.Code != 200 {
		t.Fatalf("POST /mcp/reload: %d %s", w.Code, w.Body.String())
	}
	only := []string{"alpha.echo"}
	if got := names(t, ctx.Inventory()); !reflect.DeepEqual(got, only) {
		t.Fatalf("the inventory the section reads still names the removed server: got %v, want %v", got, only)
	}
	if got := names(t, ag.ExtensionInfos()); !reflect.DeepEqual(got, only) {
		t.Fatalf("the serving agent's snapshot still names the removed server: got %v, want %v", got, only)
	}
}

func TestReloadToZeroToolsLeavesNoBlock(t *testing.T) {
	alpha := mockMCPServer(t, "zero-session")
	t.Setenv("OMEGA_HOME", "")
	t.Setenv("MCP_SERVERS", `{"servers":[{"name":"alpha","url":"`+alpha.URL+`"}]}`)

	p, err := NewPluginFromEnv()
	if err != nil {
		t.Fatalf("NewPluginFromEnv: %v", err)
	}
	ctx, ag := mountInventoryPlugin(t, p)

	t.Setenv("MCP_SERVERS", `{"servers":[]}`)
	if w := serveRoute(t, ctx, "POST /mcp/reload", "POST", "/mcp/reload"); w.Code != 200 {
		t.Fatalf("POST /mcp/reload: %d %s", w.Code, w.Body.String())
	}
	// The module stays mounted - only its tool set is gone, so the entry keeps an empty
	// ToolList and the section renders no mcp block for it.
	entry := false
	for _, info := range ctx.Inventory() {
		if info.Name != "mcp" {
			continue
		}
		entry = true
		if info.Tools != 0 || len(info.ToolList) != 0 {
			t.Fatalf("zero-server entry still carries tools: %+v", info)
		}
	}
	if !entry {
		t.Fatal("the mounted module lost its inventory entry")
	}
	if got := names(t, ag.ExtensionInfos()); len(got) != 0 {
		t.Fatalf("the serving agent still advertises %v", got)
	}
}

// TestReloadWithoutFenceStillSwaps pins the degradation: a host that arms no fence has no bound
// Context to re-wire, and the swap is still the whole reload.
func TestReloadWithoutFenceStillSwaps(t *testing.T) {
	alpha := mockMCPServer(t, "bare-session")
	t.Setenv("OMEGA_HOME", "")
	t.Setenv("MCP_SERVERS", `{"servers":[{"name":"alpha","url":"`+alpha.URL+`"}]}`)

	ctx := &core.Context{}
	if _, err := agent.MountAllAPI([]agent.Plugin{NewPlugin(nil)}, ctx, nil, nil); err != nil {
		t.Fatalf("api mount: %v", err)
	}
	if w := serveRoute(t, ctx, "POST /mcp/reload", "POST", "/mcp/reload"); w.Code != 200 {
		t.Fatalf("POST /mcp/reload: %d %s", w.Code, w.Body.String())
	}
	if _, ok := ctx.ToolProviders[0].Tools()["alpha.echo"]; !ok {
		t.Fatal("the reload did not install the live bridge's tools")
	}
}
