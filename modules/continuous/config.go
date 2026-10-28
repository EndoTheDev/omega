// Package continuous owns WHAT an agent wake means (card t_72ecca67,
// settled design, rulings 1-18). Cron owns WHEN (modules/cron reads
// each agent home's config/crons.yaml and fires ticks); this capsule
// consumes them as event-spine payloads and runs the wake: home
// machinery (folder-as-registration, no registry file), the canonical
// chat (one permanent session per agent, compaction folds it, never
// forks it), the wake subprocess (one budgeted omega binary invoked
// against the agent home - process death IS the sleep, a wedged wake
// never touches the main daemon), the evidence ledger (runtime-owned,
// not tool-writable), and the roster shape.
package continuous

// CanonicalSessionID is the fixed id of the one permanent chat every
// agent home carries (ruling 6). The home's own store holds exactly it
// plus the wake runs that ride it; compaction folds it in place - the
// id never changes, the wake subprocess re-joins via
// `omega run --session canonical`.
const CanonicalSessionID = "canonical"

// Config is the continuous capsule's config section (continuous: in
// config.yaml). Cost ruling 17: the ONLY budget is the per-wake turn
// budget riding the existing max-turns machinery (spawned children see
// OMEGA_MAX_TURNS); no daily cap, no goal wallet - the agent cannot
// mint wakes, nothing to launder.
type Config struct {
	// WakeTurnBudget caps each wake's agent loop. Existing budget
	// machinery: the spawn maps it onto OMEGA_MAX_TURNS. 0 keeps the
	// config's own max_turns (no per-wake override).
	WakeTurnBudget int `yaml:"wake_turn_budget"`
	// WakeTimeoutSeconds kills a wedged wake (crash isolation is the
	// subprocess boundary; the timeout is the second fence). 0 keeps
	// the default (30 minutes).
	WakeTimeoutSeconds int `yaml:"wake_timeout_seconds"`
	// Disabled carries the config.yaml disabled.agents names (v0.14.1
	// registry-browser): not a continuous: section key - the host
	// copies it in from the disabled: fold at config routing. A
	// disabled agent stays registered (folder presence) and leaves
	// the roster and every wake trigger; its home is never touched.
	Disabled []string `yaml:"-"`
}

// Default returns the continuous configuration defaults.
func Default() Config {
	return Config{
		WakeTurnBudget:     0,
		WakeTimeoutSeconds: 1800,
	}
}

// Timeout returns the wake fence as a duration, default applied.
func (c Config) Timeout() (seconds int) {
	if c.WakeTimeoutSeconds <= 0 {
		return Default().WakeTimeoutSeconds
	}
	return c.WakeTimeoutSeconds
}
