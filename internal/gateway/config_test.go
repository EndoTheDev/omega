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
  type: openai
  model_name: gpt-4o
  host: http://openai-proxy:8080
  api_key: sk-test
server:
  port: 9000
store:
  db_path: /tmp/omega.db
`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Provider.Type != "openai" {
		t.Errorf("type = %q, want openai", cfg.Provider.Type)
	}
	if cfg.Provider.ModelName != "gpt-4o" {
		t.Errorf("model_name = %q, want gpt-4o", cfg.Provider.ModelName)
	}
	if cfg.Provider.Host != "http://openai-proxy:8080" {
		t.Errorf("host = %q, want http://openai-proxy:8080", cfg.Provider.Host)
	}
	if cfg.Provider.APIKey != "sk-test" {
		t.Errorf("api_key = %q, want sk-test", cfg.Provider.APIKey)
	}
	if cfg.Server.Port != 9000 {
		t.Errorf("port = %d, want 9000", cfg.Server.Port)
	}
	if cfg.Store.DBPath != "/tmp/omega.db" {
		t.Errorf("db_path = %q, want /tmp/omega.db", cfg.Store.DBPath)
	}
}

func TestLoadConfigProviderTypeEnv(t *testing.T) {
	path := writeTempConfig(t, `
provider:
  model_name: llama3
`)
	t.Setenv("OMEGA_PROVIDER", "anthropic")
	t.Setenv("OMEGA_API_KEY", "sk-ant-test")
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Provider.Type != "anthropic" {
		t.Errorf("type = %q, want anthropic (env override)", cfg.Provider.Type)
	}
	if cfg.Provider.APIKey != "sk-ant-test" {
		t.Errorf("api_key = %q, want sk-ant-test (env override)", cfg.Provider.APIKey)
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

func TestCompactionDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if !cfg.Compaction.Enabled {
		t.Error("compaction.enabled default = false, want true")
	}
	if cfg.Compaction.Threshold != 0.8 {
		t.Errorf("compaction.threshold default = %v, want 0.8", cfg.Compaction.Threshold)
	}
	if cfg.Compaction.KeepFirst != 2 || cfg.Compaction.KeepLast != 10 {
		t.Errorf("compaction keep defaults = %d/%d, want 2/10", cfg.Compaction.KeepFirst, cfg.Compaction.KeepLast)
	}
}

func TestCompactionEnvOverride(t *testing.T) {
	path := writeTempConfig(t, `
provider:
  model_name: llama3
`)
	t.Setenv("OMEGA_COMPACTION_THRESHOLD", "0.5")
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Compaction.Threshold != 0.5 {
		t.Errorf("compaction.threshold = %v, want 0.5 (env override)", cfg.Compaction.Threshold)
	}
}
