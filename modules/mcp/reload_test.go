// Reload tests (card t_d6e94e79): POST /mcp/reload makes mcp.yaml
// edits live - a fresh bridge (new tools, removed servers, partial
// failures) swaps in with no restart, a broken config keeps the old
// bridge serving, and readers race the swap.
package mcp

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/engine/agent"
)

// serveRoute serves one request through the named route pattern on the
// context (both routes register on the ModuleRoutes ledger).
func serveRoute(t *testing.T, ctx *core.Context, pattern, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	for _, rt := range ctx.ModuleRoutes {
		if rt.Pattern == pattern {
			w := httptest.NewRecorder()
			rt.Handler(w, httptest.NewRequest(method, path, nil))
			return w
		}
	}
	t.Fatalf("%s not registered on the mount", pattern)
	return nil
}

// mountPlugin mounts a plugin the api way and returns the live Context.
func mountReloadPlugin(t *testing.T, p *Plugin) *core.Context {
	t.Helper()
	ctx := &core.Context{}
	if _, err := agent.MountAllAPI([]agent.Plugin{p}, ctx, nil, nil); err != nil {
		t.Fatalf("api mount: %v", err)
	}
	return ctx
}

// decodeReport parses a StatusReport body.
func decodeReport(t *testing.T, w *httptest.ResponseRecorder) StatusReport {
	t.Helper()
	var rep StatusReport
	if err := json.Unmarshal(w.Body.Bytes(), &rep); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return rep
}

// TestReloadRouteEmptyToLive verifies the exact Endo scenario: no
// config at boot (nil bridge, idle module), mcp.yaml lands, POST
// /mcp/reload - the server appears and the tool seam serves it. No
// restart anywhere.
func TestReloadRouteEmptyToLive(t *testing.T) {
	srv := mockMCPServer(t, "reload-session")
	t.Setenv("MCP_SERVERS", `{"servers":[{"name":"mock","url":"`+srv.URL+`"}]}`)
	t.Setenv("OMEGA_HOME", "")

	ctx := mountReloadPlugin(t, NewPlugin(nil))

	// before the reload: the empty state, zero tools
	before := decodeReport(t, serveRoute(t, ctx, "GET /mcp/servers", "GET", "/mcp/servers"))
	if len(before.Servers) != 0 {
		t.Fatalf("before reload: got %d servers, want 0", len(before.Servers))
	}
	if got := ctx.ToolProviders[0].Tools(); len(got) != 0 {
		t.Fatalf("before reload: got %d tools, want 0", len(got))
	}

	w := serveRoute(t, ctx, "POST /mcp/reload", "POST", "/mcp/reload")
	if w.Code != 200 {
		t.Fatalf("POST /mcp/reload: %d", w.Code)
	}
	rep := decodeReport(t, w)
	if rep.Source != "env" {
		t.Fatalf("source: got %q, want %q", rep.Source, "env")
	}
	if len(rep.Servers) != 1 || rep.Servers[0].Name != "mock" || !rep.Servers[0].Connected {
		t.Fatalf("after reload: %+v", rep.Servers)
	}
	if len(rep.Servers[0].Tools) != 1 || rep.Servers[0].Tools[0] != "mock.echo" {
		t.Fatalf("tools: %v, want [mock.echo]", rep.Servers[0].Tools)
	}
	// the same provider now serves the fresh tool set - no remount
	if got := ctx.ToolProviders[0].Tools(); len(got) != 1 {
		t.Fatalf("after reload, provider tools: got %d, want 1", len(got))
	}
	if _, ok := ctx.ToolProviders[0].Tools()["mock.echo"]; !ok {
		t.Fatal("after reload, mock.echo missing from the provider")
	}
}

// TestReloadToolsChangeAfterReload verifies the live-bridge swap: a
// configured server that leaves the config drops off the tool seam on
// reload, and the fresh list (source included) comes back on the
// reload response itself.
func TestReloadToolsChangeAfterReload(t *testing.T) {
	srv := mockMCPServer(t, "swap-session")
	t.Setenv("MCP_SERVERS", `{"servers":[{"name":"mock","url":"`+srv.URL+`"}]}`)
	t.Setenv("OMEGA_HOME", "")

	p, err := NewPluginFromEnv()
	if err != nil {
		t.Fatalf("NewPluginFromEnv: %v", err)
	}
	ctx := mountReloadPlugin(t, p)
	if _, ok := ctx.ToolProviders[0].Tools()["mock.echo"]; !ok {
		t.Fatal("precondition: mock.echo served before the reload")
	}

	// the config loses the server; no restart anywhere
	t.Setenv("MCP_SERVERS", `{"servers":[]}`)
	w := serveRoute(t, ctx, "POST /mcp/reload", "POST", "/mcp/reload")
	if w.Code != 200 {
		t.Fatalf("POST /mcp/reload: %d", w.Code)
	}
	rep := decodeReport(t, w)
	if len(rep.Servers) != 0 {
		t.Fatalf("after removal: got %+v, want empty", rep.Servers)
	}
	if got := ctx.ToolProviders[0].Tools(); len(got) != 0 {
		t.Fatalf("after removal, provider tools: got %d, want 0", len(got))
	}
	// GET /mcp/servers stays read-only over the live (now empty) bridge
	rep2 := decodeReport(t, serveRoute(t, ctx, "GET /mcp/servers", "GET", "/mcp/servers"))
	if rep2.Source != "env" || len(rep2.Servers) != 0 {
		t.Fatalf("GET after removal: %+v", rep2)
	}
}

// TestReloadRemovesServerInFlight verifies the removal contract: a
// tool run reaching the removed bridge answers a structured IsError
// tool result (an error return the loop turns into IsError), never a
// panic and never a lost response - while the fresh bridge serves.
func TestReloadRemovesServerInFlight(t *testing.T) {
	srv := mockMCPServer(t, "inflight-session")
	t.Setenv("MCP_SERVERS", `{"servers":[{"name":"mock","url":"`+srv.URL+`"}]}`)
	t.Setenv("OMEGA_HOME", "")

	p, err := NewPluginFromEnv()
	if err != nil {
		t.Fatalf("NewPluginFromEnv: %v", err)
	}
	ctx := mountReloadPlugin(t, p)
	// the tool closure the loop would hold when the call went in
	// flight - the pre-reload bridge's run
	oldTools := p.bridge.Tools()["mock.echo"]
	if oldTools.run == nil {
		t.Fatal("precondition: mock.echo's run missing")
	}

	// the config loses the server; the reload lands beneath the call
	t.Setenv("MCP_SERVERS", `{"servers":[]}`)
	w := serveRoute(t, ctx, "POST /mcp/reload", "POST", "/mcp/reload")
	if w.Code != 200 {
		t.Fatalf("POST /mcp/reload: %d", w.Code)
	}

	// the removed server's call: a structured IsError tool result
	_, runErr := oldTools.run(context.Background(), map[string]any{"msg": "hi"})
	if runErr == nil {
		t.Fatal("expected an IsError tool result from the removed server, got none")
	}
	if !strings.Contains(runErr.Error(), "removed on reload") {
		t.Fatalf("error should mention the reload removal, got %v", runErr)
	}
}

// TestReloadKeepsBridgeOnBadConfig verifies a broken config fails the
// reload (non-200, the panel's error line) but keeps the previous
// bridge serving - the refresh never drops a working tool set it
// cannot replace.
func TestReloadKeepsBridgeOnBadConfig(t *testing.T) {
	srv := mockMCPServer(t, "badcfg-session")
	t.Setenv("MCP_SERVERS", `{"servers":[{"name":"mock","url":"`+srv.URL+`"}]}`)

	p, err := NewPluginFromEnv()
	if err != nil {
		t.Fatalf("NewPluginFromEnv: %v", err)
	}
	ctx := mountReloadPlugin(t, p)
	if _, ok := ctx.ToolProviders[0].Tools()["mock.echo"]; !ok {
		t.Fatal("precondition: mock.echo served before the reload")
	}

	// the file config is now unreadable; the env var vacates
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	t.Setenv("MCP_SERVERS", "")
	if err := os.WriteFile(filepath.Join(home, "mcp.yaml"), []byte("servers: [oops"), 0o644); err != nil {
		t.Fatalf("write bad mcp.yaml: %v", err)
	}

	w := serveRoute(t, ctx, "POST /mcp/reload", "POST", "/mcp/reload")
	if w.Code != 500 {
		t.Fatalf("POST /mcp/reload with a broken config: got %d, want 500 body %q", w.Code, w.Body.String())
	}
	// the old bridge keeps serving, untouched
	if _, ok := ctx.ToolProviders[0].Tools()["mock.echo"]; !ok {
		t.Fatal("the broken reload must not drop the working tool set")
	}
}

// TestReloadPartialFailure verifies one failing server does not fail
// the reload: the live server serves, the dead one stays listed with
// its Error field (the existing StatusReport shape).
func TestReloadPartialFailure(t *testing.T) {
	srv := mockMCPServer(t, "partial-session")
	t.Setenv("MCP_SERVERS",
		`{"servers":[{"name":"live","url":"`+srv.URL+`"},{"name":"dead","url":"http://127.0.0.1:1"}]}`)
	t.Setenv("OMEGA_HOME", "")

	ctx := mountReloadPlugin(t, NewPlugin(nil))
	w := serveRoute(t, ctx, "POST /mcp/reload", "POST", "/mcp/reload")
	if w.Code != 200 {
		t.Fatalf("POST /mcp/reload: %d (a failing server must not fail the reload)", w.Code)
	}
	rep := decodeReport(t, w)
	if len(rep.Servers) != 2 {
		t.Fatalf("servers: got %d, want 2 (the dead one stays visible)", len(rep.Servers))
	}
	live, dead := rep.Servers[0], rep.Servers[1]
	if live.Name != "live" || !live.Connected || live.Error != "" {
		t.Fatalf("live: %+v", live)
	}
	if dead.Name != "dead" || dead.Connected || dead.Error == "" {
		t.Fatalf("dead: %+v", dead)
	}
	if _, ok := ctx.ToolProviders[0].Tools()["live.echo"]; !ok {
		t.Fatal("the live server's tools serve after a partial-failure reload")
	}
}

// TestReloadConcurrentReads races the swap against reader goroutines
// (Tools() and the status route): every read lands wholly on one
// bridge, nothing trips. Run with -race in the verify pass.
func TestReloadConcurrentReads(t *testing.T) {
	srv := mockMCPServer(t, "race-session")
	srvCfg := func() string {
		return `{"servers":[{"name":"mock","url":"` + srv.URL + `"}]}`
	}
	t.Setenv("MCP_SERVERS", srvCfg())
	t.Setenv("OMEGA_HOME", "")

	p, err := NewPluginFromEnv()
	if err != nil {
		t.Fatalf("NewPluginFromEnv: %v", err)
	}
	ctx := mountReloadPlugin(t, p)
	provider := ctx.ToolProviders[0]

	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = provider.Tools()
					serveRoute(t, ctx, "GET /mcp/servers", "GET", "/mcp/servers")
				}
			}
		}()
	}
	for i := 0; i < 3; i++ {
		w := serveRoute(t, ctx, "POST /mcp/reload", "POST", "/mcp/reload")
		if w.Code != 200 {
			t.Fatalf("reload %d: %d", i, w.Code)
		}
	}
	// a reload to empty and back - the swap direction changes too
	t.Setenv("MCP_SERVERS", `{"servers":[]}`)
	if w := serveRoute(t, ctx, "POST /mcp/reload", "POST", "/mcp/reload"); w.Code != 200 {
		t.Fatalf("empty reload: %d", w.Code)
	}
	t.Setenv("MCP_SERVERS", srvCfg())
	if w := serveRoute(t, ctx, "POST /mcp/reload", "POST", "/mcp/reload"); w.Code != 200 {
		t.Fatalf("back reload: %d", w.Code)
	}
	close(stop)
	wg.Wait()
}
