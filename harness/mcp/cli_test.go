package mcp

// cli_test.go: the mcp subcommand (card t_2a649d87). The CLI rides
// saveServerAndReload / deleteServerAndReload - the very write law
// POST /mcp/save and POST /mcp/delete use - so these tests pin the
// CLI's own contract (verbs, flag application, refusals) and one
// end-to-end round trip against a real home, asserting the landed file
// and the listing each step.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const cliSeedMCP = `# the human-authored file
servers:
  - name: docs   # the owner's note
    url: http://127.0.0.1:9/mcp
`

// newCLIPlugin mounts the entry set a CLI dispatch would get: a plugin
// loaded from a real mcp.yaml in a temp home.
func newCLIPlugin(t *testing.T, seed string) (*Plugin, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	t.Setenv("MCP_SERVERS", "")
	if seed != "" {
		path := filepath.Join(home, "config", "mcp.yaml")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	p, err := NewPluginFromEnv()
	if err != nil {
		t.Fatalf("NewPluginFromEnv: %v", err)
	}
	return p, home
}

func mcpConfigPath(home string) string { return filepath.Join(home, "config", "mcp.yaml") }

// TestMCPCLIRoundTrip drives add -> list -> edit -> delete through the
// handler the capsule registers, asserting the landed file and the
// listing each step.
func TestMCPCLIRoundTrip(t *testing.T) {
	p, home := newCLIPlugin(t, cliSeedMCP)
	cmd := cmdMCP(p)
	srv := mockMCPServer(t, "cli-session")

	// add: a connected http entry, and it lands in the operator's file
	if err := cmd([]string{"add", "mock", "--url", srv.URL, "--header", "X-Key:v"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	text := readFileString(t, mcpConfigPath(home))
	for _, want := range []string{"name: mock", "url: " + srv.URL, "X-Key: v", "# the owner's note"} {
		if !strings.Contains(text, want) {
			t.Fatalf("after add, mcp.yaml missing %q:\n%s", want, text)
		}
	}

	// list: the table carries both entries and the discovered tool
	var buf bytes.Buffer
	if err := mcpList(p, &buf); err != nil {
		t.Fatalf("list: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "mock") || !strings.Contains(out, "mock.echo") || !strings.Contains(out, "docs") {
		t.Fatalf("list output missing rows:\n%s", out)
	}

	// edit: only the passed flag is applied - the url survives
	if err := cmd([]string{"edit", "mock", "--enabled=false"}); err != nil {
		t.Fatalf("edit: %v", err)
	}
	entry, ok := p.ServerEntry("mock")
	if !ok {
		t.Fatal("after edit, mock missing from the live config")
	}
	if entry.URL != srv.URL || entry.isEnabled() {
		t.Fatalf("edit clobbered unpassed fields: %+v", entry)
	}
	if text := readFileString(t, mcpConfigPath(home)); !strings.Contains(text, "enabled: false") {
		t.Fatalf("edit did not land enabled: false:\n%s", text)
	}

	// edit with no flags is a loud refusal, not a rewrite
	if err := cmd([]string{"edit", "mock"}); err == nil || !strings.Contains(err.Error(), "nothing to change") {
		t.Fatalf("edit with no flags: got %v", err)
	}

	// delete: back to the seed state, byte for byte
	if err := cmd([]string{"delete", "mock"}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if got := readFileString(t, mcpConfigPath(home)); strings.Contains(got, "mock") || !strings.Contains(got, "# the human-authored file") {
		t.Fatalf("file not back to the seed state:\n%s", got)
	}
	cfg, _, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig after delete: %v", err)
	}
	if len(cfg.Servers) != 1 || cfg.Servers[0].Name != "docs" {
		t.Fatalf("servers after delete: %+v", cfg.Servers)
	}
	buf.Reset()
	if err := mcpList(p, &buf); err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if strings.Contains(buf.String(), "mock") || !strings.Contains(buf.String(), "docs") {
		t.Fatalf("listing not back to the starting state:\n%s", buf.String())
	}
}

// TestMCPCLIAddBootstraps: add on a home with no mcp.yaml creates the
// file (the first add is the bootstrap).
func TestMCPCLIAddBootstraps(t *testing.T) {
	p, home := newCLIPlugin(t, "")
	cmd := cmdMCP(p)
	if err := cmd([]string{"add", "local", "--command", "cat"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if text := readFileString(t, mcpConfigPath(home)); !strings.Contains(text, "command: cat") {
		t.Fatalf("bootstrap file:\n%s", text)
	}
}

// TestMCPCLIRefusals pins the grammar's refusals: unknown verb, the
// name-first law, a bad env pair, unknown entry, trailing arguments.
func TestMCPCLIRefusals(t *testing.T) {
	p, _ := newCLIPlugin(t, cliSeedMCP)
	cmd := cmdMCP(p)
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"frobnicate"}, "unknown mcp subcommand"},
		{[]string{"add", "--url", "http://x/mcp"}, "the server name comes first"},
		{[]string{"add", "local", "--command", "cat", "--env", "oops"}, "expected KEY=VALUE"},
		{[]string{"add", "local", "--command", "cat", "extra"}, "only the name is positional"},
		{[]string{"edit", "ghost", "--enabled=false"}, "no server"},
		{[]string{"delete", "ghost"}, "no mcp server named"},
		{[]string{"delete", "docs", "extra"}, "only the name is positional"},
		{[]string{"list", "extra"}, "unexpected argument"},
	}
	for _, c := range cases {
		err := cmd(c.args)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%v: got %v, want error containing %q", c.args, err, c.want)
		}
	}
}

// TestMCPCLIHelp prints the module's own help and registers the verb
// summary the global help listing shows.
func TestMCPCLIHelp(t *testing.T) {
	p, _ := newCLIPlugin(t, "")
	if err := cmdMCP(p)([]string{"--help"}); err != nil {
		t.Fatalf("--help: %v", err)
	}
	if !strings.Contains(mcpHelp, "omega mcp add <name> [flags]") {
		t.Fatal("help text missing the add grammar")
	}
	if !strings.Contains(mcpSummary, "list, add, edit, delete") {
		t.Fatalf("summary: %q", mcpSummary)
	}
}
