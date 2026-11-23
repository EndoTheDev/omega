package mcp

// entries_test.go: the entry CRUD write law at the node level - bootstrap, in-place
// upsert, document-comment survival, delete returning the file to its seed bytes,
// duplicate refusal and the one schema law every writer shares.

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

// TestRemoveLandsTheSeedState verifies the residue law: after add then delete, the
// loader reads exactly the seed's servers and the file carries no trace of the added
// entry. (Byte identity is not the contract - the node marshal normalizes indentation -
// the landed config is.)
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

// A save that carries the read mask back keeps the stored secret: the panel edit that
// never touches an env value cannot erase it.
func TestUpsertKeepsMaskedSecrets(t *testing.T) {
	_, path := seedHome(t, "mcp.yaml", entriesSeed)
	if err := upsertServerEntry(path, serverConfig{
		Name:    "local",
		Command: "/bin/echo",
		Args:    []string{"hi"},
		Env:     map[string]string{"TOKEN": "s3cret", "EMPTY": ""},
	}); err != nil {
		t.Fatalf("seed entry: %v", err)
	}

	if err := upsertServerEntry(path, serverConfig{
		Name:    "local",
		Command: "/bin/echo2",
		Args:    []string{"hi"},
		Env:     map[string]string{"TOKEN": maskedValue, "EMPTY": ""},
	}); err != nil {
		t.Fatalf("masked save: %v", err)
	}

	cfg, _, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	var local *serverConfig
	for i := range cfg.Servers {
		if cfg.Servers[i].Name == "local" {
			local = &cfg.Servers[i]
		}
	}
	if local == nil {
		t.Fatalf("local entry lost: %+v", cfg.Servers)
	}
	if local.Command != "/bin/echo2" {
		t.Fatalf("command: %q, want the edited value", local.Command)
	}
	if got := local.Env["TOKEN"]; got != "s3cret" {
		t.Fatalf("env TOKEN: %q, want the stored secret kept", got)
	}
	if got, ok := local.Env["EMPTY"]; !ok || got != "" {
		t.Fatalf("env EMPTY: %q (present=%v), want the empty value kept", got, ok)
	}
}

func TestUpsertMaskWithoutStoredValueIsRefused(t *testing.T) {
	_, path := seedHome(t, "mcp.yaml", entriesSeed)
	before := readFileString(t, path)
	cases := []serverConfig{
		// a header mask on an entry whose file carries no headers
		{Name: "docs", URL: "http://127.0.0.1:9/mcp", Headers: map[string]string{"Authorization": maskedValue}},
		// an env mask on an entry that does not exist yet
		{Name: "fresh", Command: "/bin/echo", Env: map[string]string{"TOKEN": maskedValue}},
	}
	for _, sc := range cases {
		err := upsertServerEntry(path, sc)
		if err == nil || !strings.Contains(err.Error(), "no value to keep") {
			t.Fatalf("upsert %q with a mask: got %v, want the refusal", sc.Name, err)
		}
	}
	if after := readFileString(t, path); after != before {
		t.Fatalf("a refused masked save touched the file:\n%s", after)
	}
}

func TestUpsertMaskedHeaderResolvesAndReplaces(t *testing.T) {
	_, path := seedHome(t, "mcp.yaml", entriesSeed)
	if err := upsertServerEntry(path, serverConfig{
		Name:    "docs",
		URL:     "http://127.0.0.1:9/mcp",
		Headers: map[string]string{"Authorization": "Bearer s3cret"},
	}); err != nil {
		t.Fatalf("seed header: %v", err)
	}
	if err := upsertServerEntry(path, serverConfig{
		Name:    "docs",
		URL:     "http://127.0.0.1:9/mcp2",
		Headers: map[string]string{"Authorization": maskedValue, "X-Fresh": "v"},
	}); err != nil {
		t.Fatalf("masked header save: %v", err)
	}
	text := readFileString(t, path)
	if !strings.Contains(text, "Authorization: Bearer s3cret") {
		t.Fatalf("stored header not kept:\n%s", text)
	}
	if !strings.Contains(text, "X-Fresh: v") {
		t.Fatalf("new header not written:\n%s", text)
	}
}
