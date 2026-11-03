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

// Default returns the default approvals configuration: manual (Q5).
func Default() Config {
	return Config{Mode: ModeManual}
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
