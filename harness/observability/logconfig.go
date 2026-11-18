package observability

// Config configures the outbound logging half of the capsule.
type Config struct {
	Enabled bool   `yaml:"enabled"`
	File    string `yaml:"file"`
}

// Default returns the default logging configuration.
func Default() Config {
	return Config{Enabled: true, File: ""}
}
