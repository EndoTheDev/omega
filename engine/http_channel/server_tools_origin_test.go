package http_channel

// GET /tools origin provenance (card t_c2da3379): the wire carries each
// tool's origin field - "mcp" for MCP-bridge tools, omitted (empty) for
// native ones. The Tools TAB's filter (harness/tools/ui/toolRows.ts)
// reads this field; the wire itself stays the agent's full executable
// registry, truthful about what the agent can call. The test feeds one
// native dotted tool (sessions.search) and one MCP-stamped tool
// (sequential-thinking.sequentialthinking) through the real handler and
// asserts both rows carry their origin - NO name-shape logic anywhere:
// both are dotted, the origin field is the only distinction.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

func TestToolsListingCarriesOrigin(t *testing.T) {
	tools := map[string]core.Tool{
		// native dotted tools: origin empty, must ride the wire unstamped
		"sessions.search": {Description: "search past sessions"},
		"shell.run":       {Description: "run a shell command"},
		// the MCP-stamped shape: exactly what bridgeToolProvider.Tools()
		// produces (harness/mcp/plugin.go stamps core.ToolOriginMCP)
		"sequential-thinking.sequentialthinking": {
			Description: "sequential thinking",
			Origin:      core.ToolOriginMCP,
		},
	}
	if core.ToolOriginMCP != "mcp" {
		t.Fatalf("ToolOriginMCP drift: %q", core.ToolOriginMCP)
	}

	a := core.NewAgent(nil, tools, 0)
	s := NewServer(a, nil, nil, nil, nil, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/tools", nil)
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /tools: %d %s", rec.Code, rec.Body)
	}

	var got []toolListing
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v (%s)", err, rec.Body)
	}
	byName := map[string]toolListing{}
	for _, row := range got {
		byName[row.Name] = row
	}
	for _, name := range []string{"sessions.search", "shell.run"} {
		row, ok := byName[name]
		if !ok {
			t.Fatalf("%s absent from the listing: %s", name, rec.Body)
		}
		if row.Origin != "" {
			t.Fatalf("%s: native tool carries origin %q, want empty", name, row.Origin)
		}
	}
	row, ok := byName["sequential-thinking.sequentialthinking"]
	if !ok {
		t.Fatalf("mcp tool absent from the registry listing (the wire stays truthful): %s", rec.Body)
	}
	if row.Origin != core.ToolOriginMCP {
		t.Fatalf("mcp tool origin: got %q, want %q", row.Origin, core.ToolOriginMCP)
	}
}