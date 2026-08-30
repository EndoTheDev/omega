package main

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
	if cfg.Compaction.Threshold != 0.6 {
		t.Errorf("compaction.threshold default = %v, want 0.6", cfg.Compaction.Threshold)
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

func TestApplyEnvCompaction(t *testing.T) {
	path := writeTempConfig(t, `
provider:
  model_name: llama3
`)
	t.Setenv("OMEGA_COMPACTION_ENABLED", "false")
	t.Setenv("OMEGA_COMPACTION_CONTEXT_WINDOW", "65536")
	t.Setenv("OMEGA_COMPACTION_KEEP_FIRST", "5")
	t.Setenv("OMEGA_COMPACTION_KEEP_LAST", "20")
	t.Setenv("OMEGA_COMPACTION_RESERVE_TOKENS", "8192")
	t.Setenv("OMEGA_COMPACTION_MAX_TOOL_OUTPUT", "16384")

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Compaction.Enabled {
		t.Errorf("compaction.enabled = true, want false (env override)")
	}
	if cfg.Compaction.ContextWindow != 65536 {
		t.Errorf("compaction.context_window = %d, want 65536", cfg.Compaction.ContextWindow)
	}
	if cfg.Compaction.KeepFirst != 5 {
		t.Errorf("compaction.keep_first = %d, want 5", cfg.Compaction.KeepFirst)
	}
	if cfg.Compaction.KeepLast != 20 {
		t.Errorf("compaction.keep_last = %d, want 20", cfg.Compaction.KeepLast)
	}
	if cfg.Compaction.ReserveTokens != 8192 {
		t.Errorf("compaction.reserve_tokens = %d, want 8192", cfg.Compaction.ReserveTokens)
	}
	if cfg.Compaction.MaxToolOutput != 16384 {
		t.Errorf("compaction.max_tool_output = %d, want 16384", cfg.Compaction.MaxToolOutput)
	}
}

func TestApplyEnvCompactionEnabledTrue(t *testing.T) {
	path := writeTempConfig(t, `
provider:
  model_name: llama3
compaction:
  enabled: false
`)
	t.Setenv("OMEGA_COMPACTION_ENABLED", "1")
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !cfg.Compaction.Enabled {
		t.Errorf("compaction.enabled = false, want true (env override with '1')")
	}
}

func TestApplyEnvMisc(t *testing.T) {
	path := writeTempConfig(t, `
provider:
  model_name: llama3
`)
	t.Setenv("OMEGA_SKILLS_DIR", "/custom/skills")
	t.Setenv("OMEGA_HTTP_TIMEOUT", "600")
	t.Setenv("OMEGA_MAX_TURNS", "200")
	t.Setenv("OMEGA_THEME", "dark")
	t.Setenv("OMEGA_NOTIFICATIONS", "none")

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Skills.Dir != "/custom/skills" {
		t.Errorf("skills.dir = %q, want /custom/skills", cfg.Skills.Dir)
	}
	if cfg.HTTPTimeout != 600 {
		t.Errorf("http_timeout = %d, want 600", cfg.HTTPTimeout)
	}
	if cfg.MaxTurns != 200 {
		t.Errorf("max_turns = %d, want 200", cfg.MaxTurns)
	}
	if cfg.Theme != "dark" {
		t.Errorf("theme = %q, want dark", cfg.Theme)
	}
	if cfg.Notifications != "none" {
		t.Errorf("notifications = %q, want none", cfg.Notifications)
	}
}

func TestApplyEnvMemory(t *testing.T) {
	path := writeTempConfig(t, `
provider:
  model_name: llama3
`)
	t.Setenv("OMEGA_MEMORY_ENABLED", "false")
	t.Setenv("OMEGA_USER_PROFILE_ENABLED", "0")
	t.Setenv("OMEGA_MEMORY_CHAR_LIMIT", "5000")
	t.Setenv("OMEGA_USER_CHAR_LIMIT", "3000")

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Memory.Enabled {
		t.Errorf("memory.enabled = true, want false (env override)")
	}
	if cfg.Memory.UserProfileEnabled {
		t.Errorf("memory.user_profile_enabled = true, want false (env override)")
	}
	if cfg.Memory.CharLimit != 5000 {
		t.Errorf("memory.char_limit = %d, want 5000", cfg.Memory.CharLimit)
	}
	if cfg.Memory.UserProfileCharLimit != 3000 {
		t.Errorf("memory.user_char_limit = %d, want 3000", cfg.Memory.UserProfileCharLimit)
	}
}

func TestApplyEnvMemoryEnabledTrue(t *testing.T) {
	path := writeTempConfig(t, `
provider:
  model_name: llama3
memory:
  enabled: false
  user_profile_enabled: false
`)
	t.Setenv("OMEGA_MEMORY_ENABLED", "true")
	t.Setenv("OMEGA_USER_PROFILE_ENABLED", "true")
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !cfg.Memory.Enabled {
		t.Errorf("memory.enabled = false, want true (env override)")
	}
	if !cfg.Memory.UserProfileEnabled {
		t.Errorf("memory.user_profile_enabled = false, want true (env override)")
	}
}

func TestApplyEnvLogging(t *testing.T) {
	path := writeTempConfig(t, `
provider:
  model_name: llama3
`)
	t.Setenv("OMEGA_LOGGING_ENABLED", "false")
	t.Setenv("OMEGA_LOGGING_FILE", "/var/log/omega.log")

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Logging.Enabled {
		t.Errorf("logging.enabled = true, want false (env override)")
	}
	if cfg.Logging.File != "/var/log/omega.log" {
		t.Errorf("logging.file = %q, want /var/log/omega.log", cfg.Logging.File)
	}
}

func TestApplyEnvLoggingEnabledTrue(t *testing.T) {
	path := writeTempConfig(t, `
provider:
  model_name: llama3
logging:
  enabled: false
`)
	t.Setenv("OMEGA_LOGGING_ENABLED", "1")
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !cfg.Logging.Enabled {
		t.Errorf("logging.enabled = false, want true (env override with '1')")
	}
}

func TestApplyEnvInvalidValues(t *testing.T) {
	path := writeTempConfig(t, `
provider:
  model_name: llama3
`)
	// Invalid port: non-numeric — should be skipped, default preserved.
	t.Setenv("OMEGA_PORT", "not-a-number")
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Server.Port != 8099 {
		t.Errorf("port = %d, want default 8099 (invalid env skipped)", cfg.Server.Port)
	}
}

func TestApplyEnvInvalidCompactionThreshold(t *testing.T) {
	path := writeTempConfig(t, `
provider:
  model_name: llama3
`)
	// Out-of-range threshold (negative) — should be skipped, default preserved.
	t.Setenv("OMEGA_COMPACTION_THRESHOLD", "-0.5")
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Compaction.Threshold != 0.6 {
		t.Errorf("threshold = %v, want default 0.6 (invalid env skipped)", cfg.Compaction.Threshold)
	}
}

func TestApplyEnvInvalidCompactionThresholdOverOne(t *testing.T) {
	path := writeTempConfig(t, `
provider:
  model_name: llama3
`)
	// Out-of-range threshold (>1) — should be skipped, default preserved.
	t.Setenv("OMEGA_COMPACTION_THRESHOLD", "1.5")
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Compaction.Threshold != 0.6 {
		t.Errorf("threshold = %v, want default 0.6 (invalid env skipped)", cfg.Compaction.Threshold)
	}
}

func TestApplyEnvInvalidCompactionContextWindow(t *testing.T) {
	path := writeTempConfig(t, `
provider:
  model_name: llama3
`)
	// Zero context window — should be skipped, default preserved.
	t.Setenv("OMEGA_COMPACTION_CONTEXT_WINDOW", "0")
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Compaction.ContextWindow != 32768 {
		t.Errorf("context_window = %d, want default 32768 (invalid env skipped)", cfg.Compaction.ContextWindow)
	}
}

func TestApplyEnvNegativeCompactionContextWindow(t *testing.T) {
	path := writeTempConfig(t, `
provider:
  model_name: llama3
`)
	// Negative context window — should be skipped, default preserved.
	t.Setenv("OMEGA_COMPACTION_CONTEXT_WINDOW", "-100")
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Compaction.ContextWindow != 32768 {
		t.Errorf("context_window = %d, want default 32768 (invalid env skipped)", cfg.Compaction.ContextWindow)
	}
}
