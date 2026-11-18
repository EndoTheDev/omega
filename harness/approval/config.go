package approval

type Config struct {
	Mode string `yaml:"mode"`
	Model string `yaml:"model"`
	// SmartPolicy is the operator policy text the reviewer sees
	// (Q2/Q8: trusted operator configuration, never command text).
	SmartPolicy string `yaml:"smart_policy"`

	HTTPHost string `yaml:"-"`
	APIKey string `yaml:"-"`
	Home string `yaml:"-"`
}

const (
	ModeOff    = "off"
	ModeManual = "manual"
	ModeSmart  = "smart"
)

func Default() Config {
	return Config{Mode: ModeManual}
}

func (c Config) NormalizedMode() string {
	if c.Mode == "" {
		return ModeManual
	}
	return c.Mode
}

func ValidMode(m string) bool {
	return m == "" || m == ModeOff || m == ModeManual || m == ModeSmart
}
