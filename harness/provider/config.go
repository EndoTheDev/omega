package provider

type Config struct {
	Type      string `yaml:"type"`
	ModelName string `yaml:"model_name"`
	Host      string `yaml:"host"`
	APIKey    string `yaml:"api_key"`
	// Instances are the named provider instances (config.yaml providers:). Built lazily by
	// the resolver; a removed instance answers not-found (the shell fails the run loudly -
	// never a silent fallback to the default).
	Instances map[string]Config `yaml:"instances,omitempty"`
}

func Default() Config {
	return Config{Host: "http://localhost:11434"}
}
