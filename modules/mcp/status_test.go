// Status inventory tests (card t_c7136c50): the read-only GET
// /mcp/servers surface the webui MCP dock tab lists — shape, empty
// state, and per-server connection truth.
package mcp

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/engine/agent"
)

// mountPlugin mounts the plugin the api way and returns the live
// Context (the ModuleRoutes ledger rides it).
func mountPlugin(t *testing.T, p *Plugin) *core.Context {
	t.Helper()
	ctx := &core.Context{}
	if _, err := agent.MountAllAPI([]agent.Plugin{p}, ctx, nil, nil); err != nil {
		t.Fatalf("api mount: %v", err)
	}
	return ctx
}

// serveStatusRoute finds the module's GET /mcp/servers route on the
// context and serves one GET through it.
func serveStatusRoute(t *testing.T, ctx *core.Context) *httptest.ResponseRecorder {
	t.Helper()
	for _, rt := range ctx.ModuleRoutes {
		if rt.Pattern == "GET /mcp/servers" {
			w := httptest.NewRecorder()
			rt.Handler(w, httptest.NewRequest("GET", "/mcp/servers", nil))
			return w
		}
	}
	t.Fatal("GET /mcp/servers not registered on the mount")
	return nil
}

// TestStatusRouteNilBridge verifies the empty-state contract: a nil
// bridge (host fallback on config failure) still registers the route
// and answers an empty list, never a module-rooted miss.
func TestStatusRouteNilBridge(t *testing.T) {
	ctx := mountPlugin(t, NewPlugin(nil))
	w := serveStatusRoute(t, ctx)
	if w.Code != 200 {
		t.Fatalf("GET /mcp/servers: %d", w.Code)
	}
	var rep StatusReport
	if err := json.Unmarshal(w.Body.Bytes(), &rep); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if rep.Source != "none" {
		t.Fatalf("source: got %q, want \"none\"", rep.Source)
	}
	if len(rep.Servers) != 0 {
		t.Fatalf("servers: got %d, want 0", len(rep.Servers))
	}
	// the tool seam stays claimed even on a nil bridge (the reload
	// cycle, card t_d6e94e79): one provider that follows the live
	// bridge per call - it serves the empty map now
	if len(ctx.ToolProviders) != 1 {
		t.Fatalf("tool providers: got %d, want 1", len(ctx.ToolProviders))
	}
	if got := ctx.ToolProviders[0].Tools(); len(got) != 0 {
		t.Fatalf("nil bridge tools: got %d, want 0", len(got))
	}
}

// TestStatusRouteEmptyBridge verifies an existing-but-empty bridge
// serves the empty report from its own Status, and the route rides
// the ModuleRoutes ledger.
func TestStatusRouteEmptyBridge(t *testing.T) {
	b, err := NewBridge(nil)
	if err != nil {
		t.Fatalf("NewBridge(nil): %v", err)
	}
	ctx := mountPlugin(t, NewPlugin(b))
	w := serveStatusRoute(t, ctx)
	if w.Code != 200 {
		t.Fatalf("GET /mcp/servers: %d", w.Code)
	}
	var rep StatusReport
	if err := json.Unmarshal(w.Body.Bytes(), &rep); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(rep.Servers) != 0 {
		t.Fatalf("servers: got %v, want empty", rep.Servers)
	}
	// the additive tools seam stays claimed the classic way
	if len(ctx.ToolProviders) != 1 {
		t.Fatalf("tool providers: got %d, want 1", len(ctx.ToolProviders))
	}
}

// TestStatusReportConnectionTruth builds a bridge over three shapes —
// a live HTTP MCP server, an entry with neither command nor url, and a
// refusing URL — and verifies the report names transport, connection,
// failure, and the server-qualified tools.
func TestStatusReportConnectionTruth(t *testing.T) {
	srv := mockMCPServer(t, "test-session")
	cfg := &mcpConfig{Servers: []serverConfig{
		{Name: "obsidian", URL: srv.URL},
		{Name: "void"}, // neither transport configured
		{Name: "dead", URL: "http://127.0.0.1:1"},
	}}
	b, err := NewBridge(cfg)
	if err != nil {
		t.Fatalf("NewBridge: %v", err)
	}
	rep := b.Status()
	if len(rep.Servers) != 3 {
		t.Fatalf("servers: got %d, want 3", len(rep.Servers))
	}
	obsidian := rep.Servers[0]
	if obsidian.Name != "obsidian" || obsidian.Transport != "http" {
		t.Fatalf("obsidian shape: %+v", obsidian)
	}
	if !obsidian.Connected {
		t.Fatal("obsidian: the live server must report connected")
	}
	if obsidian.Error != "" {
		t.Fatalf("obsidian error: %q", obsidian.Error)
	}
	if len(obsidian.Tools) == 0 || obsidian.Tools[0] != "obsidian.echo" {
		t.Fatalf("obsidian tools: %v, want [obsidian.echo]", obsidian.Tools)
	}
	void := rep.Servers[1]
	if void.Connected || void.Error == "" {
		t.Fatalf("void truth: connected=%v error=%q", void.Connected, void.Error)
	}
	if void.Transport != "" {
		t.Fatalf("void transport: %q, want empty", void.Transport)
	}
	dead := rep.Servers[2]
	if dead.Connected {
		t.Fatal("dead: a refusing URL must not report connected")
	}
	if dead.Error == "" {
		t.Fatal("dead: the connect failure must be recorded, not lost")
	}
}

// TestStatusRouteWithLiveServer verifies the full env path: config
// from MCP_SERVERS, a connected server, and the JSON body over the
// actual route handler.
func TestStatusRouteWithLiveServer(t *testing.T) {
	srv := mockMCPServer(t, "test-session")
	t.Setenv("MCP_SERVERS", `{"servers":[{"name":"proof","url":"`+srv.URL+`"}]}`)
	t.Setenv("OMEGA_HOME", "")
	p, err := NewPluginFromEnv()
	if err != nil {
		t.Fatalf("NewPluginFromEnv: %v", err)
	}
	ctx := mountPlugin(t, p)
	w := serveStatusRoute(t, ctx)
	if w.Code != 200 {
		t.Fatalf("GET /mcp/servers: %d", w.Code)
	}
	var rep StatusReport
	if err := json.Unmarshal(w.Body.Bytes(), &rep); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if rep.Source != "env" {
		t.Fatalf("source: got %q, want \"env\"", rep.Source)
	}
	if len(rep.Servers) != 1 {
		t.Fatalf("servers: got %d, want 1", len(rep.Servers))
	}
	if rep.Servers[0].Name != "proof" || rep.Servers[0].Transport != "http" || !rep.Servers[0].Connected {
		t.Fatalf("server shape: %+v", rep.Servers[0])
	}
	if len(rep.Servers[0].Tools) != 1 || rep.Servers[0].Tools[0] != "proof.echo" {
		t.Fatalf("tools: %v, want [proof.echo]", rep.Servers[0].Tools)
	}
}
