package skills

// Config configures the skills extension.
type Config struct {
	Dir string `yaml:"dir"`
}

// Default returns the default skills configuration.
func Default() Config {
	return Config{Dir: "skills"}
}
