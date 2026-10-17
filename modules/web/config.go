package web

// Config configures the web search/fetch extension.
type Config struct {
	APIKey string `yaml:"api_key"`
}

// Default returns the default web configuration.
func Default() Config {
	return Config{}
}
