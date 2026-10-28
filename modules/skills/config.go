package skills

// Config configures the skills extension.
type Config struct {
	// Dir is the primary scan directory (one agent's skill home).
	Dir string `yaml:"dir"`
	// SharedDir is scanned FIRST when set (card t_7e2b7002): the root
	// skills/shared home every agent sees. Name collisions resolve to
	// the primary dir - an agent's own skills shadow shared ones.
	SharedDir string `yaml:"shared_dir"`
	// Disabled carries the config.yaml disabled.skills names (v0.14.1
	// registry-browser): not a skills: section key - the host copies it
	// in from the disabled: fold at config routing. A disabled skill
	// stays loaded (the tab lists it) and just leaves the prompt.
	Disabled []string `yaml:"-"`
}

// Default returns the default skills configuration.
func Default() Config {
	return Config{Dir: "skills"}
}
