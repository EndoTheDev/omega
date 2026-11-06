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
	// NudgeInterval is the learning-loop reminder cadence (card
	// t_9972dfd0): after this many executed tool calls without a
	// persisted lesson (skills write/update), the next turn prep
	// carries one reminder line. 0 = nudging off.
	NudgeInterval int `yaml:"nudge_interval"`
	// CuratorModel carries the model slot the weekly curator pass
	// consults for its merge/distill proposals: "instance:model" (a
	// named provider instance, card t_7dcd4280) or a bare model -
	// resolved through agent.ResolveModelSlot. Empty = the session
	// provider's own model (Endo's ruling). Source: the top-level
	// skills_model knob (provider settings, card t_fc0b2f2d). Live
	// since card t_f78acfd6: the host's hot-apply re-sends the
	// skills section on every settings save
	// (SkillsProvider.SetCuratorModel), so a skills_model flip
	// reaches the next curator action without a restart.
	CuratorModel string `yaml:"curator_model"`
}

// DefaultNudgeInterval is the learning-loop cadence (card t_9972dfd0):
// the nudge fires after 15 tool iterations without a persisted lesson.
const DefaultNudgeInterval = 15

// Default returns the default skills configuration.
func Default() Config {
	return Config{Dir: "skills", NudgeInterval: DefaultNudgeInterval}
}
