// Status inventory tests: the read-only GET /mcp/servers surface the webui MCP dock.
package mcp

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

func mountPlugin(t *testing.T, p *Plugin) *core.Context {
	t.Helper()
	ctx := &core.Context{}
	if _, err := agent.MountAllAPI([]agent.Plugin{p}, ctx, nil, nil); err != nil {
		t.Fatalf("api mount: %v", err)
	}
	return ctx
}

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
	// the tool seam stays claimed even on a nil bridge.
	if len(ctx.ToolProviders) != 1 {
		t.Fatalf("tool providers: got %d, want 1", len(ctx.ToolProviders))
	}
	if got := ctx.ToolProviders[0].Tools(); len(got) != 0 {
		t.Fatalf("nil bridge tools: got %d, want 0", len(got))
	}
}

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
	if len(ctx.ToolProviders) != 1 {
		t.Fatalf("tool providers: got %d, want 1", len(ctx.ToolProviders))
	}
}

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

// The wire carries the whole entry so the panel can edit it: args plain (the command
// line), env and header VALUES masked (a token never rides the read wire).
func TestStatusWireCarriesEntryAndMasksSecrets(t *testing.T) {
	disabled := false
	cfg := &mcpConfig{Servers: []serverConfig{
		{
			Name:    "stdio",
			Command: "/bin/echo",
			Args:    []string{"--flag", "value"},
			Env:     map[string]string{"TOKEN": "s3cret", "EMPTY": ""},
			Enabled: &disabled,
		},
		{
			Name:    "http",
			URL:     "http://127.0.0.1:9/mcp",
			Headers: map[string]string{"Authorization": "Bearer s3cret"},
			Enabled: &disabled,
		},
	}}
	b, err := NewBridge(cfg)
	if err != nil {
		t.Fatalf("NewBridge: %v", err)
	}
	rep := b.Status()
	if len(rep.Servers) != 2 {
		t.Fatalf("servers: got %d, want 2", len(rep.Servers))
	}
	stdio := rep.Servers[0]
	if len(stdio.Args) != 2 || stdio.Args[0] != "--flag" || stdio.Args[1] != "value" {
		t.Fatalf("args: %v, want [--flag value] plain", stdio.Args)
	}
	if got := stdio.Env["TOKEN"]; got != maskedValue {
		t.Fatalf("env TOKEN: %q, want the mask", got)
	}
	if got, ok := stdio.Env["EMPTY"]; !ok || got != "" {
		t.Fatalf("env EMPTY: %q (present=%v), want the empty value as itself", got, ok)
	}
	if got := rep.Servers[1].Headers["Authorization"]; got != maskedValue {
		t.Fatalf("header Authorization: %q, want the mask", got)
	}
	raw, err := json.Marshal(rep)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), "s3cret") {
		t.Fatalf("the report carries a secret value:\n%s", raw)
	}
}
