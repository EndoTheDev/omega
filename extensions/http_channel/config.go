package http_channel

// Config configures the HTTP channel extension.
type Config struct {
	Port int `yaml:"port"`
}

// Default returns the default HTTP channel configuration.
func Default() Config {
	return Config{Port: 8099}
}
