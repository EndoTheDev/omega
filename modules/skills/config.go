package skills

// Config configures the skills extension.
type Config struct {
	// Dir is the primary scan directory (one agent's skill home).
	Dir string `yaml:"dir"`
	// SharedDir is scanned FIRST when set (card t_7e2b7002): the root
	// skills/shared home every agent sees. Name collisions resolve to
	// the primary dir - an agent's own skills shadow shared ones.
	SharedDir string `yaml:"shared_dir"`
}

// Default returns the default skills configuration.
func Default() Config {
	return Config{Dir: "skills"}
}
