package kanban

// Config configures the kanban extension.
type Config struct {
	DBPath string `yaml:"db_path"`
}

// Default returns the default kanban configuration. The host resolves
// the relative default against OMEGA_HOME (cmd/omega homeDefaults).
func Default() Config {
	return Config{DBPath: "kanban.db"}
}