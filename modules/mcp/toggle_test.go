// Per-server enable/disable toggle tests (card t_83f37525): the
// config flag is honored at connect (a disabled server is never
// connected, serves no tools, stays listed), the wire carries it,
// and POST /mcp/toggle persists it into mcp.yaml (comments and key
// order survive the node write) and reloads through the shared swap.
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

// writeHome writes a temp OMEGA_HOME with an mcp.yaml at the config/
// subdir (the Endo 2026-10-18 layout the loader reads second) and
// points the env at it. Returns the yaml path.
func writeHome(t *testing.T, yaml string) string {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "config", "mcp.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OMEGA_HOME", home)
	t.Setenv("MCP_SERVERS", "")
	return path
}

// TestServerConfigEnabledFlag: the yaml flag parses, an absent key
// reads as on (pre-flag configs keep working).
func TestServerConfigEnabledFlag(t *testing.T) {
	cfg, err := parseConfig([]byte(`
servers:
  - name: on
    command: cat
  - name: off
    command: cat
    enabled: false
  - name: explicit
    command: cat
    enabled: true
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Servers) != 3 {
		t.Fatalf("servers: got %d, want 3", len(cfg.Servers))
	}
	if !cfg.Servers[0].isEnabled() {
		t.Error("absent enabled key must read as enabled")
	}
	if cfg.Servers[1].isEnabled() {
		t.Error("enabled: false must read as disabled")
	}
	if !cfg.Servers[2].isEnabled() {
		t.Error("enabled: true must read as enabled")
	}
}

// TestDisabledServerNotConnectedNotServed: the connect loop skips the
// disabled entry - no connection attempt (the mock never sees it),
// no discovered tools in the served map, and the status row stays
// listed with enabled:false / not connected / no error.
func TestDisabledServerNotConnectedNotServed(t *testing.T) {
	up := mockMCPServer(t, "s1")
	defer up.Close()
	down := mockMCPServer(t, "s2")
	defer down.Close()

	writeHome(t, `
servers:
  - name: live
    url: `+up.URL+`
  - name: muted
    url: `+down.URL+`
    enabled: false
`)
	b, err := NewBridgeFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	st := b.Status()
	if len(st.Servers) != 2 {
		t.Fatalf("servers listed: got %d, want 2 (disabled rows stay visible)", len(st.Servers))
	}
	byName := map[string]ServerStatus{}
	for _, s := range st.Servers {
		byName[s.Name] = s
	}
	if !byName["live"].Enabled || !byName["live"].Connected || len(byName["live"].Tools) != 1 {
		t.Fatalf("live: %+v", byName["live"])
	}
	m := byName["muted"]
	if m.Enabled {
		t.Error("muted: the wire must carry enabled:false")
	}
	if m.Connected {
		t.Error("muted: a disabled server is never connected")
	}
	if m.Error != "" {
		t.Errorf("muted: the skip is intentional, not a failure - error %q", m.Error)
	}
	if len(m.Tools) != 0 {
		t.Errorf("muted: no tools discovered - got %v", m.Tools)
	}
	// the served tool map carries only the live server's names
	served := b.Tools()
	if len(served) != 1 {
		t.Fatalf("served tools: got %d, want 1", len(served))
	}
	if _, ok := served["muted.echo"]; ok {
		t.Error("a disabled server's tools must never enter the tool map")
	}
	if _, ok := served["live.echo"]; !ok {
		t.Error("the live server's tools stay served")
	}
}

// serveToggleRoute finds the module's POST /mcp/toggle route on the
// context and serves one POST through it. Returns the response.
func serveToggleRoute(t *testing.T, ctx *core.Context, name string, enabled bool) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(toggleRequest{Name: name, Enabled: enabled})
	if err != nil {
		t.Fatal(err)
	}
	for _, rt := range ctx.ModuleRoutes {
		if rt.Pattern == "POST /mcp/toggle" {
			w := httptest.NewRecorder()
			r := httptest.NewRequest("POST", "/mcp/toggle", strings.NewReader(string(body)))
			rt.Handler(w, r)
			return w
		}
	}
	t.Fatal("POST /mcp/toggle not registered on the mount")
	return nil
}

// TestToggleRoutePersistsAndReloads: POST /mcp/toggle writes the
// flag into mcp.yaml (comments + key order survive the node write),
// answers the fresh StatusReport, and the reload honored it - the
// disabled server's tools leave the served map. Toggle back on for
// the return-trip half.
func TestToggleRoutePersistsAndReloads(t *testing.T) {
	up := mockMCPServer(t, "s1")
	defer up.Close()

	path := writeHome(t, `
# the operator's file: comments survive the toggle write
servers:
  - name: echo
    url: `+up.URL+`
  - name: other
    command: never-run
`)
	p, err := NewPluginFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	ctx := mountPlugin(t, p)
	// route registration happens in Mount - assert through the mounted
	// routes the toggle exists
	found := false
	for _, rt := range ctx.ModuleRoutes {
		if rt.Pattern == "POST /mcp/toggle" {
			found = true
		}
	}
	if !found {
		t.Fatal("POST /mcp/toggle not registered on the mount")
	}

	// ---- disable echo ----
	w := serveToggleRoute(t, ctx, "echo", false)
	if w.Code != 200 {
		t.Fatalf("toggle off: %d %s", w.Code, w.Body.String())
	}
	var rep StatusReport
	if err := json.Unmarshal(w.Body.Bytes(), &rep); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// the response IS the fresh status (the reload's body ruling)
	var disabled *ServerStatus
	for i := range rep.Servers {
		if rep.Servers[i].Name == "echo" {
			disabled = &rep.Servers[i]
		}
	}
	if disabled == nil || disabled.Enabled || disabled.Connected || len(disabled.Tools) != 0 {
		t.Fatalf("response: echo not disabled: %+v", disabled)
	}
	// the file carries the flag; comments and key order survive
	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	y := string(out)
	if !strings.Contains(y, "enabled: false") {
		t.Fatalf("file missing the flag:\n%s", y)
	}
	if !strings.Contains(y, "# the operator's file") {
		t.Errorf("the comment did not survive the write:\n%s", y)
	}
	if strings.Index(y, "name: echo") > strings.Index(y, "enabled: false") {
		t.Errorf("flag must ride its own entry, not a new tail key:\n%s", y)
	}
	if !strings.Contains(y, "name: other") {
		t.Errorf("the untouched entry must survive:\n%s", y)
	}
	// the served tools drop the disabled server's names
	if _, ok := p.activeBridge().Tools()["echo.echo"]; ok {
		t.Error("the disabled server's tools stay served - the reload did not honor the write")
	}

	// ---- re-enable echo (the return trip) ----
	w2 := serveToggleRoute(t, ctx, "echo", true)
	if w2.Code != 200 {
		t.Fatalf("toggle on: %d %s", w2.Code, w2.Body.String())
	}
	var rep2 StatusReport
	if err := json.Unmarshal(w2.Body.Bytes(), &rep2); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var enabled *ServerStatus
	for i := range rep2.Servers {
		if rep2.Servers[i].Name == "echo" {
			enabled = &rep2.Servers[i]
		}
	}
	if enabled == nil || !enabled.Enabled || !enabled.Connected || len(enabled.Tools) != 1 {
		t.Fatalf("response: echo not re-enabled: %+v", enabled)
	}
	if _, ok := p.activeBridge().Tools()["echo.echo"]; !ok {
		t.Error("the re-enabled server's tools did not come back")
	}
	// reload persists the state: a fresh status read (no further write)
	// still reports it
	st := p.activeBridge().Status()
	for _, s := range st.Servers {
		if s.Name == "echo" && !s.Enabled {
			t.Error("status does not match the persisted file")
		}
	}
}

// TestToggleRouteRefusals: the sources with nowhere (or no right way)
// to persist are refused at the wire, not silently dropped.
func TestToggleRouteRefusals(t *testing.T) {
	// env source: the env var is not a file
	t.Setenv("MCP_SERVERS", `{"servers":[{"name":"x","command":"cat"}]}`)
	p, err := NewPluginFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	ctx := mountPlugin(t, p)
	w := serveToggleRoute(t, ctx, "x", false)
	if w.Code != 400 {
		t.Errorf("env source: got %d, want 400 (%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "MCP_SERVERS") {
		t.Errorf("the refusal must name the source: %s", w.Body.String())
	}

	// no config file anywhere: nothing to toggle
	p2, err := NewPluginFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	t.Setenv("MCP_SERVERS", "")
	ctx2 := mountPlugin(t, p2)
	w2 := serveToggleRoute(t, ctx2, "x", false)
	if w2.Code != 400 {
		t.Errorf("no config: got %d, want 400 (%s)", w2.Code, w2.Body.String())
	}
	if !strings.Contains(w2.Body.String(), "no mcp config file") {
		t.Errorf("the refusal must name the empty source: %s", w2.Body.String())
	}

	// mcp.json: the flag lives on the mcp.yaml entry, never a JSON write
	jsonHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(jsonHome, "mcp.json"),
		[]byte(`{"servers":[{"name":"x","command":"cat"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OMEGA_HOME", jsonHome)
	p3, err := NewPluginFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	ctx3 := mountPlugin(t, p3)
	w3 := serveToggleRoute(t, ctx3, "x", false)
	if w3.Code != 400 {
		t.Errorf("json source: got %d, want 400 (%s)", w3.Code, w3.Body.String())
	}
	if !strings.Contains(w3.Body.String(), "mcp.json") {
		t.Errorf("the refusal must name the file kind: %s", w3.Body.String())
	}

	// unknown server in a valid file: refuse, never write garbage
	yamlHome := t.TempDir()
	path := filepath.Join(yamlHome, "mcp.yaml")
	if err := os.WriteFile(path, []byte("servers:\n  - name: real\n    command: cat\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OMEGA_HOME", yamlHome)
	p4, err := NewPluginFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	ctx4 := mountPlugin(t, p4)
	w4 := serveToggleRoute(t, ctx4, "ghost", false)
	if w4.Code != 400 {
		t.Errorf("unknown name: got %d, want 400 (%s)", w4.Code, w4.Body.String())
	}
	out, _ := os.ReadFile(path)
	if strings.Contains(string(out), "enabled") {
		t.Errorf("a refused toggle must not touch the file:\n%s", out)
	}
}