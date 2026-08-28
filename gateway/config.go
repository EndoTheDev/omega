package gateway

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/EndoTheDev/omega/agent"
	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v3"
)

// Config holds the gateway's runtime configuration. It is loaded from
// config.yaml, then overridden by environment variables, then defaults
// are applied for anything still unset.
type Config struct {
	Provider      ProviderConfig          `yaml:"provider"`
	Server        ServerConfig            `yaml:"server"`
	Store         StoreConfig             `yaml:"store"`
	Compaction    agent.CompactionConfig  `yaml:"compaction"`
	SystemPrompt  string                  `yaml:"system_prompt"`
	HTTPTimeout   int                     `yaml:"http_timeout"`
	MaxTurns      int                     `yaml:"max_turns"`
	Theme         string                  `yaml:"theme"`
	Notifications string                  `yaml:"notifications"`
	Skills        SkillsConfig            `yaml:"skills"`
	Memory        MemoryConfig            `yaml:"memory"`
	Logging       LoggingConfig           `yaml:"logging"`
}

// SkillsConfig controls where skills are loaded from.
type SkillsConfig struct {
	Dir string `yaml:"dir"`
}

// MemoryConfig controls the persistent memory system.
type MemoryConfig struct {
	Enabled              bool   `yaml:"enabled"`
	UserProfileEnabled   bool   `yaml:"user_profile_enabled"`
	CharLimit            int    `yaml:"char_limit"`
	UserProfileCharLimit int    `yaml:"user_char_limit"`
	File                 string `yaml:"file"`
	UserProfileFile      string `yaml:"user_file"`
}

// LoggingConfig controls the operational logging system.
type LoggingConfig struct {
	Enabled bool   `yaml:"enabled"`
	File    string `yaml:"file"`
}

// ProviderConfig configures the LLM provider connection.
type ProviderConfig struct {
	Type      string `yaml:"type"`
	ModelName string `yaml:"model_name"`
	Host      string `yaml:"host"`
	APIKey    string `yaml:"api_key"`
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
			Enabled:       true,
			Threshold:     0.6,
			ContextWindow: 32768,
			KeepFirst:     2,
			KeepLast:      10,
			ReserveTokens: 16384,
			MaxToolOutput: 32768,
		},
		Skills: SkillsConfig{
			Dir: "skills",
		},
		Memory: MemoryConfig{
			Enabled:              true,
			UserProfileEnabled:   true,
			CharLimit:            2200,
			UserProfileCharLimit: 1375,
			File:                 "memory.md",
			UserProfileFile:      "user.md",
		},
		Logging: LoggingConfig{
			Enabled: true,
			File:    "omega.log",
		},
		HTTPTimeout:  300,
		MaxTurns:     100,
		Notifications: "bell",
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
	if v := os.Getenv("OMEGA_PROVIDER"); v != "" {
		cfg.Provider.Type = v
	}
	if v := os.Getenv("OMEGA_API_KEY"); v != "" {
		cfg.Provider.APIKey = v
	}
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
	if v := os.Getenv("OMEGA_COMPACTION_ENABLED"); v != "" {
		cfg.Compaction.Enabled = v == "1" || strings.EqualFold(v, "true")
	}
	if v := os.Getenv("OMEGA_COMPACTION_THRESHOLD"); v != "" {
		if threshold, err := strconv.ParseFloat(v, 64); err == nil && threshold > 0 && threshold <= 1 {
			cfg.Compaction.Threshold = threshold
		}
	}
	if v := os.Getenv("OMEGA_COMPACTION_CONTEXT_WINDOW"); v != "" {
		if window, err := strconv.Atoi(v); err == nil && window > 0 {
			cfg.Compaction.ContextWindow = window
		}
	}
	if v := os.Getenv("OMEGA_COMPACTION_KEEP_FIRST"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			cfg.Compaction.KeepFirst = n
		}
	}
	if v := os.Getenv("OMEGA_COMPACTION_KEEP_LAST"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			cfg.Compaction.KeepLast = n
		}
	}
	if v := os.Getenv("OMEGA_COMPACTION_RESERVE_TOKENS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.Compaction.ReserveTokens = n
		}
	}
	if v := os.Getenv("OMEGA_COMPACTION_MAX_TOOL_OUTPUT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.Compaction.MaxToolOutput = n
		}
	}
	if v := os.Getenv("OMEGA_SKILLS_DIR"); v != "" {
		cfg.Skills.Dir = v
	}
	if v := os.Getenv("OMEGA_HTTP_TIMEOUT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.HTTPTimeout = n
		}
	}
	if v := os.Getenv("OMEGA_MAX_TURNS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.MaxTurns = n
		}
	}
	if v := os.Getenv("OMEGA_THEME"); v != "" {
		cfg.Theme = v
	}
	if v := os.Getenv("OMEGA_NOTIFICATIONS"); v != "" {
		cfg.Notifications = v
	}
	if v := os.Getenv("OMEGA_MEMORY_ENABLED"); v != "" {
		cfg.Memory.Enabled = v == "1" || strings.EqualFold(v, "true")
	}
	if v := os.Getenv("OMEGA_USER_PROFILE_ENABLED"); v != "" {
		cfg.Memory.UserProfileEnabled = v == "1" || strings.EqualFold(v, "true")
	}
	if v := os.Getenv("OMEGA_MEMORY_CHAR_LIMIT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.Memory.CharLimit = n
		}
	}
	if v := os.Getenv("OMEGA_USER_CHAR_LIMIT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.Memory.UserProfileCharLimit = n
		}
	}
	if v := os.Getenv("OMEGA_LOGGING_ENABLED"); v != "" {
		cfg.Logging.Enabled = v == "1" || strings.EqualFold(v, "true")
	}
	if v := os.Getenv("OMEGA_LOGGING_FILE"); v != "" {
		cfg.Logging.File = v
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

// WatchConfig watches the config file at path and calls onChange when
// it changes. The callback receives the freshly loaded config. If the
// reload fails (e.g. file is temporarily empty during an atomic save),
// the error is ignored and the old config stays in effect. Runs until
// the watcher is closed by the caller.
func WatchConfig(path string, onChange func(Config)) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return
	}
	go func() {
		defer watcher.Close()
		watcher.Add(path)
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
					cfg, err := LoadConfig(path)
					if err == nil {
						onChange(cfg)
					}
				}
			case _, ok := <-watcher.Errors:
				if !ok {
					return
				}
			}
		}
	}()
}
