package gateway

import (
	"os"
	"path/filepath"
	"testing"
)

// writeTempConfig writes content to a temp file and returns its path.
func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}

func TestLoadConfigDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Provider.Host != "http://localhost:11434" {
		t.Errorf("default host = %q, want http://localhost:11434", cfg.Provider.Host)
	}
	if cfg.Server.Port != 8099 {
		t.Errorf("default port = %d, want 8099", cfg.Server.Port)
	}
	if cfg.Store.DBPath != "omega.db" {
		t.Errorf("default db_path = %q, want omega.db", cfg.Store.DBPath)
	}
}

func TestLoadConfigFromYAML(t *testing.T) {
	path := writeTempConfig(t, `
provider:
  model_name: llama3
  host: http://ollama:11434
server:
  port: 9000
store:
  db_path: /tmp/omega.db
`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Provider.ModelName != "llama3" {
		t.Errorf("model_name = %q, want llama3", cfg.Provider.ModelName)
	}
	if cfg.Provider.Host != "http://ollama:11434" {
		t.Errorf("host = %q, want http://ollama:11434", cfg.Provider.Host)
	}
	if cfg.Server.Port != 9000 {
		t.Errorf("port = %d, want 9000", cfg.Server.Port)
	}
	if cfg.Store.DBPath != "/tmp/omega.db" {
		t.Errorf("db_path = %q, want /tmp/omega.db", cfg.Store.DBPath)
	}
}

func TestLoadConfigEnvOverridesYAML(t *testing.T) {
	path := writeTempConfig(t, `
provider:
  model_name: llama3
  host: http://ollama:11434
server:
  port: 9000
store:
  db_path: /tmp/omega.db
`)
	t.Setenv("OMEGA_MODEL", "qwen2.5")
	t.Setenv("OMEGA_HOST", "http://env-host:11434")
	t.Setenv("OMEGA_PORT", "7777")
	t.Setenv("OMEGA_DB_PATH", "/env/omega.db")

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Provider.ModelName != "qwen2.5" {
		t.Errorf("model_name = %q, want qwen2.5 (env override)", cfg.Provider.ModelName)
	}
	if cfg.Provider.Host != "http://env-host:11434" {
		t.Errorf("host = %q, want env override", cfg.Provider.Host)
	}
	if cfg.Server.Port != 7777 {
		t.Errorf("port = %d, want 7777 (env override)", cfg.Server.Port)
	}
	if cfg.Store.DBPath != "/env/omega.db" {
		t.Errorf("db_path = %q, want /env/omega.db (env override)", cfg.Store.DBPath)
	}
}

func TestLoadConfigEnvFillsMissingYAML(t *testing.T) {
	// YAML provides only model_name; env fills the rest.
	path := writeTempConfig(t, `
provider:
  model_name: llama3
`)
	t.Setenv("OMEGA_HOST", "http://env-host:11434")
	t.Setenv("OMEGA_PORT", "7000")
	t.Setenv("OMEGA_DB_PATH", "/env/omega.db")

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Provider.Host != "http://env-host:11434" {
		t.Errorf("host = %q, want env value", cfg.Provider.Host)
	}
	if cfg.Server.Port != 7000 {
		t.Errorf("port = %d, want 7000", cfg.Server.Port)
	}
	if cfg.Store.DBPath != "/env/omega.db" {
		t.Errorf("db_path = %q, want /env/omega.db", cfg.Store.DBPath)
	}
}

func TestValidateRequiresModelName(t *testing.T) {
	path := writeTempConfig(t, `
server:
  port: 8099
`)
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("expected error for missing model_name, got nil")
	}
}

func TestValidateRejectsNonPositivePort(t *testing.T) {
	path := writeTempConfig(t, `
provider:
  model_name: llama3
server:
  port: 0
`)
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("expected error for port 0, got nil")
	}
}
