package provider

// Config configures the LLM provider extension.
type Config struct {
	Type      string `yaml:"type"`
	ModelName string `yaml:"model_name"`
	Host      string `yaml:"host"`
	APIKey    string `yaml:"api_key"`
}

// Default returns the default provider configuration.
func Default() Config {
	return Config{Host: "http://localhost:11434"}
}
