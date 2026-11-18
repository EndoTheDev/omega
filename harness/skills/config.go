package skills

type Config struct {
	Dir string `yaml:"dir"`
	SharedDir string `yaml:"shared_dir"`
	Disabled []string `yaml:"-"`
	// Allow is the agent definition layer's explicit skills allowlist. AllowSet
	// distinguishes "unset" (the host's own mount: every skill loads) from "set to empty"
	// (a secondary agent that enables NO skills) - the always-off law needs that
	// difference, and an empty slice cannot carry it.
	Allow    []string `yaml:"-"`
	AllowSet bool     `yaml:"-"`
	NudgeInterval int `yaml:"nudge_interval"`
	CuratorModel string `yaml:"curator_model"`
}

const DefaultNudgeInterval = 15

func Default() Config {
	return Config{Dir: "skills", NudgeInterval: DefaultNudgeInterval}
}
