package store

// Config configures the session store extension.
type Config struct {
	DBPath string `yaml:"db_path"`
}

// Default returns the default store configuration.
func Default() Config {
	return Config{DBPath: "omega.db"}
}
