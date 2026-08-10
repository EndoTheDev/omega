package gateway

import (
	"fmt"
	"os"
	"strconv"

	"github.com/EndoTheDev/omega-dev/internal/agent"
	"gopkg.in/yaml.v3"
)

// Config holds the gateway's runtime configuration. It is loaded from
// config.yaml, then overridden by environment variables, then defaults
// are applied for anything still unset.
type Config struct {
	Provider   ProviderConfig          `yaml:"provider"`
	Server     ServerConfig            `yaml:"server"`
	Store      StoreConfig             `yaml:"store"`
	Compaction agent.CompactionConfig  `yaml:"compaction"`
}

// ProviderConfig configures the LLM provider connection.
type ProviderConfig struct {
	ModelName string `yaml:"model_name"`
	Host      string `yaml:"host"`
}

// ServerConfig configures the HTTP listener.
type ServerConfig struct {
	Port int `yaml:"port"`
}

// StoreConfig configures the SQLite session store.
type StoreConfig struct {
	DBPath string `yaml:"db_path"`
}

// DefaultConfig returns the configuration used when neither YAML nor
// environment variables provide a value.
func DefaultConfig() Config {
	return Config{
		Provider: ProviderConfig{
			Host: "http://localhost:11434",
		},
		Server: ServerConfig{
			Port: 8099,
		},
		Store: StoreConfig{
			DBPath: "omega.db",
		},
		Compaction: agent.CompactionConfig{
			Enabled:   true,
			Threshold: 0.8,
			KeepFirst: 2,
			KeepLast:  10,
		},
	}
}

// LoadConfig reads configuration from path (a config.yaml file), applies
// environment variable overrides, fills defaults, and validates the result.
// An empty path skips YAML loading entirely.
func LoadConfig(path string) (Config, error) {
	cfg := DefaultConfig()

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return Config{}, fmt.Errorf("read config: %w", err)
		}
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return Config{}, fmt.Errorf("parse config: %w", err)
		}
	}

	applyEnv(&cfg)

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// applyEnv overrides config fields from OMEGA_* environment variables.
func applyEnv(cfg *Config) {
	if v := os.Getenv("OMEGA_MODEL"); v != "" {
		cfg.Provider.ModelName = v
	}
	if v := os.Getenv("OMEGA_HOST"); v != "" {
		cfg.Provider.Host = v
	}
	if v := os.Getenv("OMEGA_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			cfg.Server.Port = port
		}
	}
	if v := os.Getenv("OMEGA_DB_PATH"); v != "" {
		cfg.Store.DBPath = v
	}
	if v := os.Getenv("OMEGA_COMPACTION_THRESHOLD"); v != "" {
		if threshold, err := strconv.ParseFloat(v, 64); err == nil && threshold > 0 && threshold <= 1 {
			cfg.Compaction.Threshold = threshold
		}
	}
}

// Validate checks that required fields are present and values are sane.
func (c Config) Validate() error {
	if c.Provider.ModelName == "" {
		return fmt.Errorf("config: provider.model_name is required")
	}
	if c.Server.Port <= 0 {
		return fmt.Errorf("config: server.port must be > 0, got %d", c.Server.Port)
	}
	return nil
}
