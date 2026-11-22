package observability

type Config struct {
	Enabled bool   `yaml:"enabled"`
	File    string `yaml:"file"`
}

func Default() Config {
	return Config{Enabled: true, File: ""}
}
