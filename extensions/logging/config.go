package logging

// Config configures the operational logging extension.
type Config struct {
	Enabled bool   `yaml:"enabled"`
	File    string `yaml:"file"`
}

// Default returns the default logging configuration.
func Default() Config {
	return Config{Enabled: true, File: "omega.log"}
}
