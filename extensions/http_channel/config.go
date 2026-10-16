package http_channel

// Config configures the HTTP channel extension.
type Config struct {
	Port int      `yaml:"port"`
	// Bind: addresses to listen on, one channel per entry.
	// Default: loopback only - the API is unauthenticated and exposes
	// shell.run, so anything beyond loopback is an explicit opt-in
	// (e.g. the Tailscale IP so Traefik can route to omega).
	Bind []string `yaml:"bind"`
}

// Default returns the default HTTP channel configuration.
func Default() Config {
	return Config{Port: 8099, Bind: []string{"127.0.0.1"}}
}
