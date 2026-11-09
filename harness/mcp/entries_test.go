package mcp

// entries_test.go: the entry CRUD write law (card t_2a649d87) at the
// node level - bootstrap, in-place upsert, document-comment survival,
// delete returning the file to its seed bytes, duplicate refusal and
// the one schema law every writer shares.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const entriesSeed = `# the human-authored file
note: keep me
servers:
  # the docs server, hand-tuned
  - name: docs
    url: http://127.0.0.1:9/mcp
    note: keep this key
  - name: local
    command: /bin/echo
    args:
      - hi
`

// seedHome writes the named config file into a temp home and returns
// it (with OMEGA_HOME pointed there).
func seedHome(t *testing.T, name, content string) (home, path string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	t.Setenv("MCP_SERVERS", "")
	if content != "" {
		path = filepath.Join(home, "config", name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return home, path
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestUpsertBootstrapsFreshHome verifies the first add against a home
// with no mcp.yaml: the file is created at config/mcp.yaml (the crons
// jobsDocument bootstrap) and the loader parses it back.
func TestUpsertBootstrapsFreshHome(t *testing.T) {
	home, _ := seedHome(t, "", "")
	path, err := writableConfigFile()
	if err != nil {
		t.Fatalf("writableConfigFile: %v", err)
	}
	want := filepath.Join(home, "config", "mcp.yaml")
	if path != want {
		t.Fatalf("bootstrap path: got %q, want %q", path, want)
	}
	enabled := true
	err = upsertServerEntry(path, serverConfig{Name: "docs", URL: "http://127.0.0.1:8080/mcp", Enabled: &enabled})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	cfg, source, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if source != "mcp.yaml" || len(cfg.Servers) != 1 {
		t.Fatalf("landed config: source=%q servers=%+v", source, cfg.Servers)
	}
	if got := cfg.Servers[0]; got.Name != "docs" || got.URL != "http://127.0.0.1:8080/mcp" || !got.isEnabled() {
		t.Fatalf("landed entry: %+v", got)
	}
}

// TestUpsertPreservesDocumentComments verifies the fold-write law: the
// document's comments, key order, unknown keys and untouched entries
// survive a write that replaces one entry.
func TestUpsertPreservesDocumentComments(t *testing.T) {
	_, path := seedHome(t, "mcp.yaml", entriesSeed)
	if err := upsertServerEntry(path, serverConfig{Name: "docs", URL: "http://127.0.0.1:9/mcp2"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	text := readFileString(t, path)
	for _, want := range []string{
		"# the human-authored file",
		"note: keep me",
		"name: local", // the untouched entry
		"command: /bin/echo",
		"- hi",
		"url: http://127.0.0.1:9/mcp2",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("after upsert, mcp.yaml missing %q:\n%s", want, text)
		}
	}
	// the replaced entry's own stale scalar is gone, and the document
	// still carries exactly two servers
	if strings.Contains(text, "127.0.0.1:9/mcp\n") {
		t.Fatalf("stale url survived the replace:\n%s", text)
	}
	cfg, _, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if len(cfg.Servers) != 2 {
		t.Fatalf("servers: got %d, want 2 (%+v)", len(cfg.Servers), cfg.Servers)
	}
	if cfg.Servers[0].Name != "docs" || cfg.Servers[0].URL != "http://127.0.0.1:9/mcp2" {
		t.Fatalf("entry order or content changed: %+v", cfg.Servers[0])
	}
}

// TestRemoveLandsTheSeedState verifies the residue law: after add then
// delete, the loader reads exactly the seed's servers and the file
// carries no trace of the added entry. (Byte identity is not the
// contract - the node marshal normalizes indentation, the cron
// writeJobsYAML law - the landed config is.)
func TestRemoveLandsTheSeedState(t *testing.T) {
	_, path := seedHome(t, "mcp.yaml", entriesSeed)
	if err := upsertServerEntry(path, serverConfig{Name: "scratch", Command: "python3", Args: []string{"server.py"}}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if cfg, _, err := loadConfig(); err != nil || len(cfg.Servers) != 3 {
		t.Fatalf("after add: cfg=%+v err=%v", cfg, err)
	}
	if err := removeServerEntry(path, "scratch"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if got := readFileString(t, path); strings.Contains(got, "scratch") {
		t.Fatalf("deleted entry left a trace in the file:\n%s", got)
	}
	cfg, _, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if len(cfg.Servers) != 2 || cfg.Servers[0].Name != "docs" || cfg.Servers[1].Name != "local" {
		t.Fatalf("servers not back to the seed state: %+v", cfg.Servers)
	}
	if !strings.Contains(readFileString(t, path), "# the human-authored file") {
		t.Fatal("the document comment did not survive the add+delete round trip")
	}
}

// TestRemoveUnknownServer verifies the 404-lane error and that nothing
// is written when the name is unknown.
func TestRemoveUnknownServer(t *testing.T) {
	_, path := seedHome(t, "mcp.yaml", entriesSeed)
	seed := readFileString(t, path)
	err := removeServerEntry(path, "ghost")
	var unknown UnknownServerError
	if !errors.As(err, &unknown) {
		t.Fatalf("got %v, want UnknownServerError", err)
	}
	if got := readFileString(t, path); got != seed {
		t.Fatalf("a refused delete touched the file:\n%s", got)
	}
}

// TestUpsertRefusesDuplicateNames verifies the ambiguity guard: a file
// whose hand-written content carries one name twice refuses the write
// instead of silently updating the first twin.
func TestUpsertRefusesDuplicateNames(t *testing.T) {
	dup := "servers:\n  - name: docs\n    url: http://a/mcp\n  - name: docs\n    url: http://b/mcp\n"
	_, path := seedHome(t, "mcp.yaml", dup)
	err := upsertServerEntry(path, serverConfig{Name: "docs", URL: "http://c/mcp"})
	if err == nil || !strings.Contains(err.Error(), "more than one server named docs") {
		t.Fatalf("got %v, want the duplicate-name refusal", err)
	}
	if got := readFileString(t, path); got != dup {
		t.Fatalf("a refused upsert touched the file:\n%s", got)
	}
}

// TestUpsertKeepsForeignKeys: a file whose top-level document carries
// other keys keeps them, and a servers: key missing entirely is
// appended rather than dropped.
func TestUpsertAppendsServersKey(t *testing.T) {
	seed := "# foreign doc\nother: 1\n"
	_, path := seedHome(t, "mcp.yaml", seed)
	if err := upsertServerEntry(path, serverConfig{Name: "docs", URL: "http://127.0.0.1/mcp"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	text := readFileString(t, path)
	if !strings.Contains(text, "other: 1") || !strings.Contains(text, "name: docs") {
		t.Fatalf("servers key not appended beside the foreign one:\n%s", text)
	}
}

// TestValidateServerEntry pins the one schema law: names are bare
// words without dots, transports are exclusive, args/env are stdio-only
// and headers are http-only.
func TestValidateServerEntry(t *testing.T) {
	enabled := true
	valid := []serverConfig{
		{Name: "docs", URL: "https://example.com/mcp", Headers: map[string]string{"X-Key": "v"}},
		{Name: "local", Command: "server", Args: []string{"--flag"}, Env: map[string]string{"K": "v"}, Enabled: &enabled},
	}
	for _, sc := range valid {
		if err := validateServerEntry(sc); err != nil {
			t.Fatalf("valid entry %+v refused: %v", sc, err)
		}
	}
	bad := map[string]serverConfig{
		"missing name":     {URL: "https://example.com/mcp"},
		"whitespace name":  {Name: " docs ", URL: "https://example.com/mcp"},
		"dotted name":      {Name: "a.b", URL: "https://example.com/mcp"},
		"leading dash":     {Name: "-docs", URL: "https://example.com/mcp"},
		"no transport":     {Name: "docs"},
		"both transports":  {Name: "docs", Command: "x", URL: "https://example.com/mcp"},
		"bad url":          {Name: "docs", URL: "ftp://example.com/mcp"},
		"args on http":     {Name: "docs", URL: "https://example.com/mcp", Args: []string{"x"}},
		"env on http":      {Name: "docs", URL: "https://example.com/mcp", Env: map[string]string{"K": "v"}},
		"headers on stdio": {Name: "docs", Command: "x", Headers: map[string]string{"X-Key": "v"}},
		"padded command":   {Name: "docs", Command: " x "},
	}
	for label, sc := range bad {
		if err := validateServerEntry(sc); err == nil {
			t.Fatalf("%s: accepted %+v, want a refusal", label, sc)
		}
	}
}

// TestWritableConfigFileRefusals pins the legacy-source refusals - the
// toggle route's exact law, shared by every writer.
func TestWritableConfigFileRefusals(t *testing.T) {
	home, _ := seedHome(t, "", "")
	t.Setenv("MCP_SERVERS", `{"servers":[{"name":"x","url":"http://x/mcp"}]}`)
	if _, err := writableConfigFile(); err == nil || !strings.Contains(err.Error(), "MCP_SERVERS") {
		t.Fatalf("env source: got %v, want the MCP_SERVERS refusal", err)
	}

	t.Setenv("MCP_SERVERS", "")
	jsonPath := filepath.Join(home, "mcp.json")
	if err := os.WriteFile(jsonPath, []byte(`{"servers":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := writableConfigFile(); err == nil || !strings.Contains(err.Error(), "mcp.json") {
		t.Fatalf("json source: got %v, want the mcp.json refusal", err)
	}
}
