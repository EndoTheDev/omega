package mcp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigEnvVar(t *testing.T) {
	t.Setenv("MCP_SERVERS", `{"servers":[{"name":"envserver","command":"envcmd"}]}`)
	cfg, _, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if len(cfg.Servers) != 1 || cfg.Servers[0].Name != "envserver" {
		t.Fatalf("cfg: %+v", cfg)
	}
}

func TestLoadConfigYAMLFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "mcp.yaml"),
		[]byte("servers:\n  - name: yamlserver\n    url: http://localhost:9999\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if len(cfg.Servers) != 1 || cfg.Servers[0].Name != "yamlserver" {
		t.Fatalf("cfg: %+v", cfg)
	}
}

func TestLoadConfigJSONFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "mcp.json"),
		[]byte(`{"servers":[{"name":"jsonserver","command":"jsoncmd"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if len(cfg.Servers) != 1 || cfg.Servers[0].Name != "jsonserver" {
		t.Fatalf("cfg: %+v", cfg)
	}
}

func TestLoadConfigEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	t.Setenv("MCP_SERVERS", "")
	cfg, _, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if len(cfg.Servers) != 0 {
		t.Fatalf("expected 0 servers, got %+v", cfg)
	}
}

func TestLoadConfigYAMLInConfigSubdir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "config", "mcp.yaml"),
		[]byte("servers:\n  - name: subserver\n    url: http://localhost:9998\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if len(cfg.Servers) != 1 || cfg.Servers[0].Name != "subserver" {
		t.Fatalf("cfg: %+v", cfg)
	}
}

func TestLoadConfigRootWinsOverSubdir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "config", "mcp.yaml"),
		[]byte("servers:\n  - name: subdir\n    url: http://localhost:1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "mcp.yaml"),
		[]byte("servers:\n  - name: rootwins\n    url: http://localhost:2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.Servers[0].Name != "rootwins" {
		t.Fatalf("root mcp.yaml should win, got %v", cfg.Servers)
	}
}

func TestLoadConfigInvalidYAML(t *testing.T) {
	t.Setenv("MCP_SERVERS", "{not yaml or json")
	if _, _, err := loadConfig(); err == nil {
		t.Fatal("expected error for invalid config, got nil")
	}
}

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

func TestNewBridgeFromEnvToolDiscovery(t *testing.T) {
	// connect has no seam for test connections, so this drives the real HTTP
	// connect against a closed port. The failure is recorded per server (the
	// Status report carries it) and the bridge still boots usable with zero tools.
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
