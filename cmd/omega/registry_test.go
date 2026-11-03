package main

// registry_test.go: the seam-level lock (card t_2d8deb8c). The
// DEFAULT agent's disable refuses at the handler AND the seam - a
// seam caller (the setup wizard, a future CLI verb) hits the same
// wall, and the fold stays untouched.

import (
	"strings"
	"sync"
	"testing"

	"github.com/EndoTheDev/omega/agent"
)

func newLockTestRegistries(t *testing.T) *omegaRegistries {
	t.Helper()
	r := &omegaRegistries{
		fold:       &DisabledNamespaces{},
		configPath: t.TempDir(),
		home:       t.TempDir(),
		mu:         newTestMutex(),
	}
	return r
}

// newTestMutex avoids importing sync in two places - same shape the
// other seams' tests use.
func newTestMutex() *sync.Mutex { return &sync.Mutex{} }

// TestToggleAgentRefusesDefaultAgent pins the belt-and-suspenders law:
// the disable direction refuses before any fold mutation or write.
func TestToggleAgentRefusesDefaultAgent(t *testing.T) {
	r := newLockTestRegistries(t)
	err := r.ToggleAgent("default", false)
	if err == nil {
		t.Fatal("the default agent was disabled at the seam")
	}
	if !strings.Contains(err.Error(), "cannot be disabled") {
		t.Fatalf("refusal text = %q, want the locked-agent law", err)
	}
	if len(r.fold.Agents) != 0 {
		t.Fatalf("fold changed: %v - the refusal bypassed into the fold", r.fold.Agents)
	}
	// the enable direction stays a no-op passthrough (stale-entry
	// cleanup), and non-default names keep toggling.
	if err := r.ToggleAgent("default", true); err != nil {
		t.Fatalf("enable default refused: %v", err)
	}
	// unknown agent home: the loud 404-error lane still works.
	if err := r.ToggleAgent(agent.DefaultAgentName+"-ghost", false); err == nil {
		t.Fatal("an unknown agent was folded")
	}
}