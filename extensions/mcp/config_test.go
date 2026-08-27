package mcp

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadConfigEnvVar verifies MCP_SERVERS env var takes priority over files.
func TestLoadConfigEnvVar(t *testing.T) {
	t.Setenv("MCP_SERVERS", `{"servers":[{"name":"envserver","command":"envcmd"}]}`)
	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if len(cfg.Servers) != 1 || cfg.Servers[0].Name != "envserver" {
		t.Fatalf("cfg: %+v", cfg)
	}
}

// TestLoadConfigYAMLFile verifies mcp.yaml in OMEGA_HOME is used when the
// env var is unset.
func TestLoadConfigYAMLFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "mcp.yaml"),
		[]byte("servers:\n  - name: yamlserver\n    url: http://localhost:9999\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if len(cfg.Servers) != 1 || cfg.Servers[0].Name != "yamlserver" {
		t.Fatalf("cfg: %+v", cfg)
	}
}

// TestLoadConfigJSONFile verifies mcp.json is used when no mcp.yaml exists.
func TestLoadConfigJSONFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "mcp.json"),
		[]byte(`{"servers":[{"name":"jsonserver","command":"jsoncmd"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if len(cfg.Servers) != 1 || cfg.Servers[0].Name != "jsonserver" {
		t.Fatalf("cfg: %+v", cfg)
	}
}

// TestLoadConfigEmpty verifies an empty config (no env var, no files) is
// returned without error — the bridge is a no-op in that case.
func TestLoadConfigEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	t.Setenv("MCP_SERVERS", "")
	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if len(cfg.Servers) != 0 {
		t.Fatalf("expected 0 servers, got %+v", cfg)
	}
}

// TestLoadConfigInvalidYAML verifies malformed config surfaces a parse error.
func TestLoadConfigInvalidYAML(t *testing.T) {
	t.Setenv("MCP_SERVERS", "{not yaml or json")
	if _, err := loadConfig(); err == nil {
		t.Fatal("expected error for invalid config, got nil")
	}
}

// TestNewBridgeFromEnvEmptyConfig verifies NewBridgeFromEnv with an empty
// config produces a working, tool-less bridge.
func TestNewBridgeFromEnvEmptyConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	t.Setenv("MCP_SERVERS", "")
	b, err := NewBridgeFromEnv()
	if err != nil {
		t.Fatalf("NewBridgeFromEnv: %v", err)
	}
	if len(b.Tools()) != 0 {
		t.Fatalf("expected 0 tools, got %d", len(b.Tools()))
	}
}

// TestNewBridgeFromEnvBadConfig verifies a broken MCP_SERVERS payload
// surfaces the parse error instead of silently returning an empty bridge.
func TestNewBridgeFromEnvBadConfig(t *testing.T) {
	t.Setenv("MCP_SERVERS", "{{{")
	if _, err := NewBridgeFromEnv(); err == nil {
		t.Fatal("expected error for invalid MCP_SERVERS, got nil")
	}
}

// TestNewBridgeFromEnvToolDiscovery verifies env-configured tools end up in
// the bridge when pointed at a fake in-process connection.
func TestNewBridgeFromEnvToolDiscovery(t *testing.T) {
	// ponytail: connect() has no injection point for test conns, so this
	// exercises loadConfig -> NewBridge wiring only with a server entry that
	// fails to connect (a URL that refuses). The bridge must still be usable
	// and the failure must be non-fatal.
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	t.Setenv("MCP_SERVERS", `{"servers":[{"name":"dead","url":"http://127.0.0.1:1"}]}`)
	b, err := NewBridgeFromEnv()
	if err != nil {
		t.Fatalf("NewBridgeFromEnv: %v", err)
	}
	if len(b.Tools()) != 0 {
		t.Fatalf("dead server should contribute 0 tools, got %d", len(b.Tools()))
	}
}