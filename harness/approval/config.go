// Package approval implements the smart approval gate (card
// t_c2b52cb5): shell commands with write/network/system effects reach
// a reviewer before they run. Smart mode consults a decision model
// (one systemone forward pass) and applies a deterministic Go ladder;
// manual mode asks the human. Model advises, Go decides.
//
// Capsule law: the gate, detector, parser, ladder, cache and audit
// log all ship from this package's one Mount - removing the module
// removes the feature whole, zero approval vocabulary outside the
// seam (agent.ApprovalProvider).
package approval

// Config is the approvals config section. Mode/Model/SmartPolicy are
// Endo's knobs (config.yaml + the settings pane). The remaining
// fields are host wiring: the ConfigSection constructor copies them
// from the provider and home sections, they are not configuration.
type Config struct {
	// Mode is off | manual | smart. Empty = manual (Q5: ship dark -
	// the flip to smart is Endo's, after the corpus eval passes).
	Mode string `yaml:"mode"`
	// Model names the decision model the reviewer consults in smart
	// mode (the folded assumption: clef:27b via the configured
	// keypool provider). Empty in smart mode = no reachable reviewer;
	// every gated command escalates (fail-closed).
	Model string `yaml:"model"`
	// SmartPolicy is the operator policy text the reviewer sees
	// (Q2/Q8: trusted operator configuration, never command text).
	SmartPolicy string `yaml:"smart_policy"`
	// Monitor is the safeagents detector set's rollout mode (card
	// t_e3cd4381): "log-only" (or empty) records WOULD-BLOCK findings
	// in the audit log and never blocks - the soak every new detector
	// ships through; "enforce" lets the severity tiers decide.
	Monitor string `yaml:"monitor"`
	// JudgeFailClosed flips the evidence-only judge's failure law
	// (card t_e3cd4381): false (the default) fails OPEN - an
	// unreachable judge adds nothing and the human still decides;
	// true fails CLOSED - the call is refused.
	JudgeFailClosed bool `yaml:"judge_fail_closed"`

	// HTTPHost is the provider host the reviewer call targets (the
	// keypool mirror). Derived from config's provider section.
	HTTPHost string `yaml:"-"`
	// APIKey rides the provider's credential (Bearer when set).
	APIKey string `yaml:"-"`
	// Home is the omega home: the audit JSONL appends there.
	Home string `yaml:"-"`
}

// Canonical mode values (Q4: the tri-state).
const (
	ModeOff    = "off"
	ModeManual = "manual"
	ModeSmart  = "smart"
)

// Default returns the default approvals configuration: manual (Q5),
// with the safeagents detector set in its log-only soak (card
// t_e3cd4381).
func Default() Config {
	return Config{Mode: ModeManual, Monitor: MonitorLogOnly}
}

// Mode returns the config's normalized mode: an empty mode IS manual
// (the zero value is live, unlike most knobs - Q5's ship-dark default).
func (c Config) NormalizedMode() string {
	if c.Mode == "" {
		return ModeManual
	}
	return c.Mode
}

// ValidMode reports whether m is a mode value config may carry.
func ValidMode(m string) bool {
	return m == "" || m == ModeOff || m == ModeManual || m == ModeSmart
}

// Detector modes (card t_e3cd4381): the safeagents detector set's
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
