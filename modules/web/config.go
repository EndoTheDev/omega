package web

// Config configures the web search/fetch extension.
type Config struct {
	APIKey string `yaml:"api_key"`
	// BaseURL is the host serving the mirrored /api/web_search and
	// /api/web_fetch endpoints. Empty falls back to ollama.com.
	BaseURL string `yaml:"base_url"`
}

// Default returns the default web configuration.
func Default() Config {
	return Config{}
}
