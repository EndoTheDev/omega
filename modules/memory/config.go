package memory

// Config configures the persistent memory extension.
type Config struct {
	Enabled              bool   `yaml:"enabled"`
	UserProfileEnabled   bool   `yaml:"user_profile_enabled"`
	CharLimit            int    `yaml:"char_limit"`
	UserProfileCharLimit int    `yaml:"user_char_limit"`
	File                 string `yaml:"file"`
	UserProfileFile      string `yaml:"user_file"`
}

// Default returns the default memory configuration.
func Default() Config {
	return Config{
		Enabled:              true,
		UserProfileEnabled:   true,
		CharLimit:            2200,
		UserProfileCharLimit: 1375,
		File:                 "memory.md",
		UserProfileFile:      "user.md",
	}
}
