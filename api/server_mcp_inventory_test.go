// The served inventory follows an mcp reload: the reload route re-wires the live channel (the
// host's own re-wire), the serving agent re-stamps the mcp entry from the bridge it just
// installed, and GET /extensions names the new server set - never the mount-time snapshot.
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/mcp"
	"github.com/EndoTheDev/omega/harness/provider"
)

// mcpEchoServer is a minimal MCP-over-HTTP peer serving one tool, "echo".
func mcpEchoServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		var result any
		switch req.Method {
		case "initialize":
			w.Header().Set("mcp-session-id", "api-session")
			result = map[string]any{"protocolVersion": "2025-11-25", "serverInfo": map[string]any{"name": "mock", "version": "1.0"}}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{
				"name": "echo", "description": "Echoes input", "inputSchema": map[string]any{"type": "object"},
			}}}
		case "tools/call":
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": "echo-result"}}}
		default:
			w.WriteHeader(http.StatusOK)
			return
		}
		out, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
		_, _ = w.Write(out)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestExtensionsFollowAnMCPReload(t *testing.T) {
	alpha := mcpEchoServer(t)
	beta := mcpEchoServer(t)
	t.Setenv("OMEGA_HOME", "")
	t.Setenv("MCP_SERVERS",
		`{"servers":[{"name":"alpha","url":"`+alpha.URL+`"},{"name":"beta","url":"`+beta.URL+`"}]}`)

	plugin, err := mcp.NewPluginFromEnv()
	if err != nil {
		t.Fatalf("mcp plugin: %v", err)
	}
	ctx := &core.Context{}
	if _, err := agent.MountAllAPI([]agent.Plugin{&provider.Plugin{}, plugin}, ctx, nil, nil); err != nil {
		t.Fatalf("mount: %v", err)
	}
	a := agent.NewFromContext(ctx, agent.AgentOptions{})
	s := NewServer(a, nil, nil, nil, nil, nil)
	// The shell's own LiveSeams face: the module route's re-wire lands here, exactly as it does
	// on the booted channel.
	ctx.Channels = append(ctx.Channels, &HTTPChannel{srv: s})
	if err := ctx.RunGroup().StartAll(context.Background(), core.StartDeps{Ctx: ctx, Agent: a}, ""); err != nil {
		t.Fatalf("fence start: %v", err)
	}

	mcpToolNames := func() []string {
		t.Helper()
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/extensions", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /extensions: %d %s", rec.Code, rec.Body)
		}
		var rows []extensionListing
		if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
			t.Fatalf("decode: %v", err)
		}
		for _, row := range rows {
			if row.Name == "mcp" {
				return row.ToolNames
			}
		}
		t.Fatalf("no mcp row on /extensions: %+v", rows)
		return nil
	}

	if got := mcpToolNames(); !reflect.DeepEqual(got, []string{"alpha.echo", "beta.echo"}) {
		t.Fatalf("boot: /extensions names %v, want both servers", got)
	}

	t.Setenv("MCP_SERVERS", `{"servers":[{"name":"alpha","url":"`+alpha.URL+`"}]}`)
	reloaded := false
	for _, rt := range ctx.ModuleRoutes {
		if rt.Pattern != "POST /mcp/reload" {
			continue
		}
		rec := httptest.NewRecorder()
		rt.Handler(rec, httptest.NewRequest(http.MethodPost, "/mcp/reload", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("POST /mcp/reload: %d %s", rec.Code, rec.Body)
		}
		reloaded = true
	}
	if !reloaded {
		t.Fatal("POST /mcp/reload is not mounted")
	}
	if got := mcpToolNames(); !reflect.DeepEqual(got, []string{"alpha.echo"}) {
		t.Fatalf("after the reload /extensions still names the removed server: %v", got)
	}

	t.Setenv("MCP_SERVERS", `{"servers":[]}`)
	for _, rt := range ctx.ModuleRoutes {
		if rt.Pattern != "POST /mcp/reload" {
			continue
		}
		rec := httptest.NewRecorder()
		rt.Handler(rec, httptest.NewRequest(http.MethodPost, "/mcp/reload", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("POST /mcp/reload (zero servers): %d %s", rec.Code, rec.Body)
		}
	}
	if got := mcpToolNames(); len(got) != 0 {
		t.Fatalf("a zero-server reload left tools on the served inventory: %v", got)
	}
}
