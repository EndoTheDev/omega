// Package continuous owns WHAT an agent wake means. Cron owns WHEN (harness/cron reads
// each agent home's config/crons.yaml and fires ticks); this capsule consumes them as
// event-spine payloads and runs the wake: home machinery (folder-as-registration), the
// canonical chat (one permanent session per agent, compaction rewrites its context, never
// forks it), the wake subprocess (one budgeted omega binary invoked against the agent home
// - process death IS the sleep, a wedged wake never touches the main daemon), the
// runtime-owned evidence ledger, and the roster shape.
package continuous

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/EndoTheDev/omega/agent"
)

// CanonicalSessionID is the fixed id of the DEFAULT agent's one permanent chat. The
// home's own store holds its chat plus the wake runs that ride it; compaction rewrites it
// in place - the id never changes, so the wake subprocess re-joins the same row.
const CanonicalSessionID = "canonical"

// CanonicalSessionIDFor returns the id of agentName's one permanent chat. The default
// agent's sessions live in the home's MAIN store and nothing else competes for the bare id,
// so `omega run --session canonical` keeps working.
func CanonicalSessionIDFor(agentName string) string {
	if agentName == "" || agentName == agent.DefaultAgentName {
		return CanonicalSessionID
	}
	return agentName + "-" + CanonicalSessionID
}

// IsCanonicalSessionID reports whether id names one of agentName's canonical chats: a
// suffix must be a positive integer, so an unrelated session sharing the base string is
// never mistaken for one.
func IsCanonicalSessionID(agentName, id string) bool {
	return canonicalGeneration(agentName, id) > 0
}

func canonicalGeneration(agentName, id string) int {
	base := CanonicalSessionIDFor(agentName)
	if id == base {
		return 1
	}
	suffix, ok := strings.CutPrefix(id, base+"-")
	if !ok {
		return 0
	}
	n, err := strconv.Atoi(suffix)
	if err != nil || n < 2 {
		return 0
	}
	return n
}

// NextCanonicalSessionID returns the id a NEW canonical mint takes: only a retire (an
// archived canonical) asks for this - a live canonical is simply opened.
func NextCanonicalSessionID(agentName string, taken map[string]bool) string {
	base := CanonicalSessionIDFor(agentName)
	if !taken[base] {
		return base
	}
	for n := 2; ; n++ {
		id := fmt.Sprintf("%s-%d", base, n)
		if !taken[id] {
			return id
		}
	}
}

// Config is the continuous capsule's config section (continuous: in config.yaml). The
// ONLY budget is the per-wake turn budget riding the existing max-turns machinery (spawned
// children see OMEGA_MAX_TURNS); no daily cap, no goal wallet - the agent cannot mint
// wakes, nothing to launder.
type Config struct {
	WakeTurnBudget int `yaml:"wake_turn_budget"`
	WakeTimeoutSeconds int `yaml:"wake_timeout_seconds"`
	// Disabled carries the config.yaml disabled.agents names. A disabled agent stays
	// registered (folder presence) and leaves the roster and every wake trigger; its home
	// is never touched.
	Disabled []string `yaml:"-"`
}

func Default() Config {
	return Config{
		WakeTurnBudget:     0,
		WakeTimeoutSeconds: 1800,
	}
}

func (c Config) Timeout() (seconds int) {
	if c.WakeTimeoutSeconds <= 0 {
		return Default().WakeTimeoutSeconds
	}
	return c.WakeTimeoutSeconds
}
