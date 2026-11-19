package approval

type Config struct {
	Mode string `yaml:"mode"`
	Model string `yaml:"model"`
	// SmartPolicy is the operator policy text the reviewer sees
	// (Q2/Q8: trusted operator configuration, never command text).
	SmartPolicy string `yaml:"smart_policy"`
	// Monitor is the safeagents detector set's rollout mode (card
	//): "log-only" (or empty) records WOULD-BLOCK findings
	// in the audit log and never blocks - the soak every new detector
	// ships through; "enforce" lets the severity tiers decide.
	Monitor string `yaml:"monitor"`
	// JudgeFailClosed flips the evidence-only judge's failure law
	//: false (the default) fails OPEN - an
	// unreachable judge adds nothing and the human still decides;
	// true fails CLOSED - the call is refused.
	JudgeFailClosed bool `yaml:"judge_fail_closed"`

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
	return Config{Mode: ModeManual, Monitor: MonitorLogOnly}
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

// Detector modes: the safeagents detector set's
// rollout. Empty = MonitorLogOnly - a new detector ships dark, soaks
// on real runs, and only then flips.
const (
	MonitorLogOnly = "log-only"
	MonitorEnforce = "enforce"
)

// NormalizedMonitor returns the config's normalized detector mode.
func (c Config) NormalizedMonitor() string {
	if c.Monitor == MonitorEnforce {
		return MonitorEnforce
	}
	return MonitorLogOnly
}

// DetectorsEnforce reports whether the safeagents detector set may
// block or escalate. False (the default) is the log-only soak: findings
// are audited as WOULD-BLOCK and the call proceeds untouched.
func (c Config) DetectorsEnforce() bool { return c.NormalizedMonitor() == MonitorEnforce }

// ValidMonitor reports whether m is a detector mode config may carry.
func ValidMonitor(m string) bool {
	return m == "" || m == MonitorLogOnly || m == MonitorEnforce
}
