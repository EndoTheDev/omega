package mcp

import (
	"context"
	"testing"

	"github.com/EndoTheDev/omega/agent"
)

// fakeConn is a test MCPConn that returns canned tools and call results.
type fakeConn struct {
	tools []mcpTool
}

func (f fakeConn) listTools() ([]mcpTool, error) { return f.tools, nil }
func (f fakeConn) callTool(name string, args map[string]any) (string, bool, error) {
	return "called " + name, false, nil
}
func (f fakeConn) close() {}

// TestPluginInterface verifies Plugin satisfies agent.Plugin at compile time.
func TestPluginInterface(t *testing.T) {
	var _ agent.Plugin = (*Plugin)(nil)
	var _ agent.ToolProvider = bridgeToolProvider{}
}

// TestEmptyBridge verifies an empty Bridge (no servers) produces zero tools
// and a no-op plugin that mounts without error.
func TestEmptyBridge(t *testing.T) {
	b, err := NewBridge(nil)
	if err != nil {
		t.Fatalf("NewBridge(nil): %v", err)
	}
	if len(b.Tools()) != 0 {
		t.Fatalf("expected 0 tools, got %d", len(b.Tools()))
	}

	p := NewPlugin(b)
	ctx := &agent.Context{}
	if err := p.Mount(ctx); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	if len(ctx.ToolProviders) != 1 {
		t.Fatalf("expected 1 tool provider, got %d", len(ctx.ToolProviders))
	}
	if len(ctx.ToolProviders[0].Tools()) != 0 {
		t.Fatalf("expected 0 tools from provider, got %d", len(ctx.ToolProviders[0].Tools()))
	}
}

// TestPluginMetadata verifies Name/Provides/Requires return expected values.
func TestPluginMetadata(t *testing.T) {
	p := NewPlugin(nil)
	if p.Name() != "mcp" {
		t.Fatalf("Name: got %q, want %q", p.Name(), "mcp")
	}
	if len(p.Provides()) != 1 || p.Provides()[0] != "tools" {
		t.Fatalf("Provides: got %v, want [tools]", p.Provides())
	}
	if len(p.Requires()) != 0 {
		t.Fatalf("Requires: got %v, want []", p.Requires())
	}
}

// TestNilBridgePlugin verifies a nil-bridge plugin is a no-op (mounts zero providers).
func TestNilBridgePlugin(t *testing.T) {
	p := NewPlugin(nil)
	ctx := &agent.Context{}
	if err := p.Mount(ctx); err != nil {
		t.Fatalf("Mount with nil bridge: %v", err)
	}
	if len(ctx.ToolProviders) != 0 {
		t.Fatalf("nil bridge should mount 0 providers, got %d", len(ctx.ToolProviders))
	}
}

// TestBridgeWithFakeServer verifies tool discovery and call routing work
// through a fake MCP server connection.
func TestBridgeWithFakeServer(t *testing.T) {
	b := &Bridge{
		servers: make(map[string]MCPConn),
		toolMap: make(map[string]toolMapping),
		toolDefs: make(map[string]mcpTool),
	}

	// Manually register a fake server with one tool.
	fake := fakeConn{
		tools: []mcpTool{
			{Name: "echo", Description: "Echoes input", InputSchema: map[string]any{"type": "object"}},
		},
	}
	b.servers["test"] = fake
	for _, tool := range fake.tools {
		toolName := "test." + tool.Name
		b.toolMap[toolName] = toolMapping{server: "test", mcp: tool.Name}
		b.toolDefs[toolName] = tool
	}

	tools := b.Tools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	tool, ok := tools["test.echo"]
	if !ok {
		t.Fatal("test.echo tool not found")
	}
	if tool.description != "Echoes input" {
		t.Fatalf("description: got %q, want %q", tool.description, "Echoes input")
	}

	// Execute the tool — should route to the fake server.
	result, err := tool.run(context.Background(), map[string]any{"msg": "hello"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result != "called echo" {
		t.Fatalf("result: got %q, want %q", result, "called echo")
	}
}

// TestConfigParsing verifies YAML and JSON config parsing both work.
func TestConfigParsing(t *testing.T) {
	// JSON config
	jsonData := []byte(`{"servers":[{"name":"test","command":"echo"}]}`)
	cfg, err := parseConfig(jsonData)
	if err != nil {
		t.Fatalf("parseConfig JSON: %v", err)
	}
	if len(cfg.Servers) != 1 || cfg.Servers[0].Name != "test" {
		t.Fatalf("JSON config: %+v", cfg)
	}

	// YAML config
	yamlData := []byte("servers:\n  - name: test2\n    url: http://localhost:8080\n")
	cfg2, err := parseConfig(yamlData)
	if err != nil {
		t.Fatalf("parseConfig YAML: %v", err)
	}
	if len(cfg2.Servers) != 1 || cfg2.Servers[0].Name != "test2" || cfg2.Servers[0].URL != "http://localhost:8080" {
		t.Fatalf("YAML config: %+v", cfg2)
	}
}

// TestMountAllIntegration verifies the plugin integrates with MountAll.
func TestMountAllIntegration(t *testing.T) {
	p := NewPlugin(nil)
	ctx := &agent.Context{}
	if err := agent.MountAll([]agent.Plugin{p}, ctx); err != nil {
		t.Fatalf("MountAll: %v", err)
	}
	if len(ctx.Infos) != 1 || ctx.Infos[0].Name != "mcp" {
		t.Fatalf("Infos: got %+v", ctx.Infos)
	}
}