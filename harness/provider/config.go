package provider

// Config configures the LLM provider extension. The struct carries the
// DEFAULT instance's fields plus the named instances map (card
// t_7dcd4280): the host routes the config.yaml provider: and
// providers: sections into this one shape, so the capsule reads one
// section ("ctx.Configs[\"provider\"]") and offers one resolver.
type Config struct {
	Type      string `yaml:"type"`
	ModelName string `yaml:"model_name"`
	Host      string `yaml:"host"`
	APIKey    string `yaml:"api_key"`
	// Instances are the named provider instances (config.yaml
	// providers:). The map key IS the instance id. Built lazily by
	// the resolver; a removed instance answers not-found (the shell
	// fails the run loudly - never a silent fallback to the default).
	Instances map[string]Config `yaml:"instances,omitempty"`
}

// Default returns the default provider configuration.
func Default() Config {
	return Config{Host: "http://localhost:11434"}
}
