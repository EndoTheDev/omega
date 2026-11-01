
package mcp

import (
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// card t_c2da3379: every bridge-discovered tool stamp carries the
// core.ToolOriginMCP origin - the wire field the webui Tools tab
// filters on (the MCP tab owns MCP display, one surface per concern).
// The stamp is the only key: no name-shape logic anywhere.
func TestBridgeToolProviderStampsMCPOrigin(t *testing.T) {
	b := &Bridge{
		servers:  make(map[string]MCPConn),
		toolMap:  make(map[string]toolMapping),
		toolDefs: make(map[string]mcpTool),
	}
	b.toolDefs["test.echo"] = mcpTool{
		Description: "Echoes input",
		InputSchema: map[string]any{"type": "object"},
	}
	b.toolMap["test.echo"] = toolMapping{server: "test", mcp: "echo"}
	p := NewPlugin(b)
	tools := bridgeToolProvider{p: p}.Tools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools["test.echo"].Origin != core.ToolOriginMCP {
		t.Fatalf("origin: got %q, want %q", tools["test.echo"].Origin, core.ToolOriginMCP)
	}
}
