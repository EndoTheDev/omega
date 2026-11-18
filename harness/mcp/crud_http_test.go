package mcp


import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// serveJSON serves one request with a JSON body through the named route pattern on.
func serveJSON(t *testing.T, ctx *core.Context, pattern, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	for _, rt := range ctx.ModuleRoutes {
		if rt.Pattern != pattern {
			continue
		}
		req := httptest.NewRequest("POST", path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		rt.Handler(w, req)
		return w
	}
	t.Fatalf("%s not registered on the mount", pattern)
	return nil
}

// crudHome points OMEGA_HOME at a temp home with no mcp config yet.
func crudHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	t.Setenv("MCP_SERVERS", "")
	return home
}

func TestCRUDRoundTrip(t *testing.T) {
	home := crudHome(t)
	srv := mockMCPServer(t, "crud-session")

	ctx := mountReloadPlugin(t, NewPlugin(nil))
	baseline := decodeReport(t, serveRoute(t, ctx, "GET /mcp/servers", "GET", "/mcp/servers"))
	if len(baseline.Servers) != 0 {
		t.Fatalf("baseline: got %+v, want empty", baseline.Servers)
	}

	w := serveJSON(t, ctx, "POST /mcp/save", "/mcp/save",
		`{"name":"mock","url":"`+srv.URL+`","enabled":true}`)
	if w.Code != 200 {
		t.Fatalf("POST /mcp/save: %d %s", w.Code, w.Body.String())
	}
	saved := decodeReport(t, w)
	if len(saved.Servers) != 1 || saved.Servers[0].Name != "mock" || !saved.Servers[0].Connected || !saved.Servers[0].Enabled {
		t.Fatalf("after save: %+v", saved.Servers)
	}
	if len(saved.Servers[0].Tools) != 1 || saved.Servers[0].Tools[0] != "mock.echo" {
		t.Fatalf("after save, tools: %v", saved.Servers[0].Tools)
	}
	if _, ok := ctx.ToolProviders[0].Tools()["mock.echo"]; !ok {
		t.Fatal("after save, the tool provider does not serve mock.echo")
	}
	landed := filepath.Join(home, "config", "mcp.yaml")
	if text := readFileString(t, landed); !strings.Contains(text, "name: mock") || !strings.Contains(text, "url: "+srv.URL) {
		t.Fatalf("landed file missing the entry:\n%s", text)
	}

	got := decodeReport(t, serveRoute(t, ctx, "GET /mcp/servers", "GET", "/mcp/servers"))
	if len(got.Servers) != 1 || got.Servers[0].Name != "mock" || !got.Servers[0].Connected {
		t.Fatalf("GET after save: %+v", got.Servers)
	}

	// delete: the listing is back to the starting state, the tool seam is empty again.
	w = serveJSON(t, ctx, "POST /mcp/delete", "/mcp/delete", `{"name":"mock"}`)
	if w.Code != 200 {
		t.Fatalf("POST /mcp/delete: %d %s", w.Code, w.Body.String())
	}
	after := decodeReport(t, w)
	if len(after.Servers) != 0 {
		t.Fatalf("after delete: %+v", after.Servers)
	}
	if tools := ctx.ToolProviders[0].Tools(); len(tools) != 0 {
		t.Fatalf("after delete, the tool provider still serves %d tool(s)", len(tools))
	}
	final := decodeReport(t, serveRoute(t, ctx, "GET /mcp/servers", "GET", "/mcp/servers"))
	baselineJSON, _ := json.Marshal(baseline)
	finalJSON, _ := json.Marshal(final)
	if string(baselineJSON) != string(finalJSON) {
		t.Fatalf("listing not back to the starting state:\nbaseline: %s\nfinal:    %s", baselineJSON, finalJSON)
	}
	// the add/delete round trip on a home that started with no file leaves no file.
	if _, err := os.Stat(landed); !os.IsNotExist(err) {
		t.Fatalf("empty mcp.yaml left behind after the last delete (stat err: %v)", err)
	}
}

// TestCRUDSaveValidation pins the refusals the wire carries.
func TestCRUDSaveValidation(t *testing.T) {
	crudHome(t)
	ctx := mountReloadPlugin(t, NewPlugin(nil))
	for _, body := range []string{
		`{}`,
		`{"name":"docs"}`,
		`{"name":"docs","command":"x","url":"http://y/mcp"}`,
		`{"name":"a.b","url":"http://y/mcp"}`,
		`{"name":"docs","url":"ftp://y/mcp"}`,
	} {
		w := serveJSON(t, ctx, "POST /mcp/save", "/mcp/save", body)
		if w.Code != 400 {
			t.Fatalf("save %s: got %d, want 400 (%s)", body, w.Code, w.Body.String())
		}
	}
}

// TestCRUDDeleteUnknown404 pins the no-silent-noop law on the wire.
func TestCRUDDeleteUnknown404(t *testing.T) {
	crudHome(t)
	ctx := mountReloadPlugin(t, NewPlugin(nil))
	w := serveJSON(t, ctx, "POST /mcp/delete", "/mcp/delete", `{"name":"ghost"}`)
	if w.Code != 404 {
		t.Fatalf("delete ghost: got %d, want 404 (%s)", w.Code, w.Body.String())
	}
	w = serveJSON(t, ctx, "POST /mcp/delete", "/mcp/delete", `{}`)
	if w.Code != 400 {
		t.Fatalf("delete without a name: got %d, want 400", w.Code)
	}
}

func TestCRUDDeleteUnmountsStdioCleanly(t *testing.T) {
	home := crudHome(t)
	script := writeFakeServer(t)
	seed := "servers:\n  - name: fake\n    command: " + pythonBin() + "\n    args:\n      - " + script + "\n"
	if err := os.MkdirAll(filepath.Join(home, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "config", "mcp.yaml"), []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := NewPluginFromEnv()
	if err != nil {
		t.Fatalf("NewPluginFromEnv: %v", err)
	}
	ctx := mountReloadPlugin(t, p)
	if _, ok := ctx.ToolProviders[0].Tools()["fake.echo"]; !ok {
		t.Fatal("precondition: fake.echo not served before the delete")
	}
	old := p.activeBridge()
	conn, ok := old.servers["fake"].(*stdioServer)
	if !ok {
		t.Fatalf("precondition: fake is not a stdio connection (%T)", old.servers["fake"])
	}

	w := serveJSON(t, ctx, "POST /mcp/delete", "/mcp/delete", `{"name":"fake"}`)
	if w.Code != 200 {
		t.Fatalf("POST /mcp/delete: %d %s", w.Code, w.Body.String())
	}

	if !old.isStopped() {
		t.Fatal("replaced bridge not marked stopped")
	}
	if conn.cmd.ProcessState == nil {
		t.Fatal("stdio child not reaped after the delete (ProcessState nil)")
	}
	if tools := ctx.ToolProviders[0].Tools(); len(tools) != 0 {
		t.Fatalf("after delete, the tool provider still serves %v", tools)
	}
	rep := decodeReport(t, serveRoute(t, ctx, "GET /mcp/servers", "GET", "/mcp/servers"))
	if len(rep.Servers) != 0 {
		t.Fatalf("after delete: %+v", rep.Servers)
	}
}
