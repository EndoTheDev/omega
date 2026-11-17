// Package continuous owns WHAT an agent wake means (card t_72ecca67,
// settled design, rulings 1-18). Cron owns WHEN (harness/cron reads
// each agent home's config/crons.yaml and fires ticks); this capsule
// consumes them as event-spine payloads and runs the wake: home
// machinery (folder-as-registration, no registry file), the canonical
// chat (one permanent session per agent, compaction folds it, never
// forks it), the wake subprocess (one budgeted omega binary invoked
// against the agent home - process death IS the sleep, a wedged wake
// never touches the main daemon), the evidence ledger (runtime-owned,
// not tool-writable), and the roster shape.
package continuous

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/EndoTheDev/omega/agent"
)

// CanonicalSessionID is the fixed id of the DEFAULT agent's one
// permanent chat (ruling 6). Every AGENT HOME's chat carries the
// qualified id CanonicalSessionIDFor(name) instead - see below.
//
// The home's own store holds its chat plus the wake runs that ride it;
// compaction folds it in place - the id never changes, so the wake
// subprocess re-joins the same row.
const CanonicalSessionID = "canonical"

// CanonicalSessionIDFor returns the id of agentName's one permanent
// chat. An agent home's id is AGENT-QUALIFIED; the default agent keeps
// the bare one.
//
// Why the qualification exists (card t_5df01d0f): the daemon serves
// every agent session through ONE store handle, and that handle
// resolves an id by probing the home's agent stores for the row
// (harness/store/router.go). A bare "canonical" seeded in two homes is
// one id twice, and the probe answers the FIRST home that holds it - so
// the second agent's roster row opened the first agent's chat, the
// run typed into it executed on the wrong home (SessionOwner resolves
// by the same probe), and the roster's active-row match (the focused id
// against the row's canonical id) always took the first agent. A
// per-home id makes the id itself the owner, and every one of those
// three readers is correct without learning about agents.
//
// The default agent's sessions live in the home's MAIN store and
// nothing else competes for the bare id there, so it stays exactly as
// it was - `omega run --session canonical` keeps working.
func CanonicalSessionIDFor(agentName string) string {
	if agentName == "" || agentName == agent.DefaultAgentName {
		return CanonicalSessionID
	}
	return agentName + "-" + CanonicalSessionID
}

// IsCanonicalSessionID reports whether id names one of agentName's
// canonical chats: the base id, or a RETIRE generation of it
// (base-2, base-3, ... - card t_10ac04f0, archive = retire).
//
// The shape is the whole identity: a canonical chat is resolved from
// the id alone wherever a home's row is read (the roster payload, the
// preview, the webui's "is this an agent chat" test). A suffix has to
// be a positive integer so an unrelated session that happens to start
// with the base string is never mistaken for one.
func IsCanonicalSessionID(agentName, id string) bool {
	return canonicalGeneration(agentName, id) > 0
}

// canonicalGeneration answers which generation of agentName's canonical
// chat id names: 1 for the base id, n for "base-n", 0 for anything
// else. The number (not the string) is what orders two generations
// minted in the same second - "base-10" is later than "base-2".
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

// NextCanonicalSessionID returns the id a NEW canonical mint takes: the
// base id while no row carries it, else the lowest free generation.
// Only a retire (an archived canonical) asks for this - a live
// canonical is simply opened, never re-minted.
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
