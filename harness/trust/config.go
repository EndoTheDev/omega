package trust

// Config holds the trust extension's configuration.
type Config struct {
	Home string
}

// Default returns the default trust configuration.
func Default() Config {
	return Config{}
}
