package cron

// sweep_test.go: the cronstate.json GC rule (card t_a99ffcd7). State
// entries whose job no longer declares itself anywhere (main
// crons.yaml + agent homes) are swept at Start and on every hot
// reload, logged by name; a declared job's state is never touched,
// disabled included. The observed drift this pins: a live home whose
// crons.yaml declared jobs: [] while cronstate.json carried three
// jrn-*-roster/wake entries pointing at roster agents from FINISHED
// journey runs (one with no run record at all).

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/agent"
)

// writeState writes a cronstate.json fixture into home.
func writeState(t *testing.T, home string, st cronState) {
	t.Helper()
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, cronStateFileName), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// readState reads the on-disk cronstate.json back.
func readState(t *testing.T, home string) cronState {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, cronStateFileName))
	if err != nil {
		t.Fatal(err)
	}
	var st cronState
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatal(err)
	}
	return st
}

// waitForState polls until cronstate.json satisfies keep, or fails.
func waitForState(t *testing.T, home string, keep func(cronState) bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if keep(readState(t, home)) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("cronstate.json never reached the swept shape: %+v", readState(t, home))
}

// The observed drift, replayed: crons.yaml declares jobs: [], the
// state carries three jrn-* wake entries from finished journeys -
// one fired clean, one errored, one never ran at all (a fire record
// with no outcome). A real Start (the same path a serve boot runs:
// loadCronState -> sweep -> catch-up) must drop all three.
func TestSweepRemovesObservedJourneyDriftOnStart(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "config", stateFileName),
		[]byte("jobs: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeState(t, home, cronState{
		Jobs: map[string]int64{
			"jrn-05122240-roster/wake": 1764600000,
			"jrn-06180915-roster/wake": 1765000000,
			"jrn-06191002-roster/wake": 1765000100, // no run record at all
		},
		Last: map[string]fireOutcome{
			"jrn-05122240-roster/wake": {At: 1764600010, Ok: true},
			"jrn-06180915-roster/wake": {At: 1765000010, Ok: false, Error: "continuous not mounted, wake inert"},
		},
	})

	cfg, err := LoadAllConfigs(home)
	if err != nil {
		t.Fatalf("LoadAllConfigs: %v", err)
	}
	r := NewRunner(cfg, home)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = r.Start(ctx, runnerDeps{}) }()

	waitForState(t, home, func(st cronState) bool {
		return len(st.Jobs) == 0 && len(st.Last) == 0
	})
	_ = r.Stop()
}

// The conservative half: declared jobs keep their state, disabled
// ones included ("never drop a job that still declares itself"),
// while undeclared leftovers go - on the same Start.
func TestSweepKeepsDeclaredDropsStale(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "config", stateFileName), []byte(
		"jobs:\n"+
			"  - name: daily\n"+
			"    schedule: \"0 8 * * *\"\n"+
			"    prompt: p\n"+
			"  - name: off\n"+
			"    schedule: \"0 9 * * *\"\n"+
			"    prompt: p\n"+
			"    enabled: false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := agent.ScaffoldAgent(home, "scout", ""); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "agents", "scout", "config", stateFileName),
		[]byte("jobs:\n  - name: wake\n    schedule: \"17 8 * * *\"\n    prompt: patrol\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeState(t, home, cronState{
		Jobs: map[string]int64{
			"daily":                    100,
			"off":                      100,
			"scout/wake":               100,
			"jrn-05122240-roster/wake": 100,
			"ghost":                    100,
		},
		Last: map[string]fireOutcome{
			"daily":      {At: 100, Ok: true},
			"scout/wake": {At: 100, Ok: true},
			"ghost":      {At: 100, Ok: false, Error: "x"},
		},
	})

	r := NewRunner(mustLoadAll(t, home), home)
	r.loadCronState()
	r.sweepStaleState()

	st := readState(t, home)
	for _, keep := range []string{"daily", "off", "scout/wake"} {
		if _, ok := st.Jobs[keep]; !ok {
			t.Errorf("declared job %q state swept", keep)
		}
	}
	for _, drop := range []string{"jrn-05122240-roster/wake", "ghost"} {
		if _, ok := st.Jobs[drop]; ok {
			t.Errorf("stale fire record %q survived", drop)
		}
	}
	if _, ok := st.Last["ghost"]; ok {
		t.Error("stale fire outcome survived")
	}
	if _, ok := st.Last["daily"]; !ok {
		t.Error("declared job's outcome swept")
	}
}

// The live journey shape: an agent home with a declared wake is
// removed (the recipe's rm -rf), the watcher-style reload re-loads
// the job set without it, and the sweep drops its state entries.
func TestSweepOnReloadAfterAgentHomeRemoved(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, stateFileName), []byte("jobs: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := agent.ScaffoldAgent(home, "jrn-05-roster", ""); err != nil {
		t.Fatal(err)
	}
	agentCfg := filepath.Join(home, "agents", "jrn-05-roster", "config", stateFileName)
	if err := os.WriteFile(agentCfg,
		[]byte("jobs:\n  - name: wake\n    schedule: \"17 8 * * *\"\n    prompt: p\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := NewRunner(mustLoadAll(t, home), home)
	r.loadCronState()
	r.recordFire(r.jobsSnapshot()[0], time.Unix(1764600000, 0))
	if _, ok := r.lastFireOf("jrn-05-roster/wake"); !ok {
		t.Fatal("fixture never recorded the wake")
	}

	// the journey's rm -rf: home gone, the reload re-reads without it
	if err := os.RemoveAll(filepath.Join(home, "agents", "jrn-05-roster")); err != nil {
		t.Fatal(err)
	}
	r.applyReload(context.Background(), mustLoadAll(t, home))

	if _, ok := r.lastFireOf("jrn-05-roster/wake"); ok {
		t.Error("removed agent home's wake state survived the reload")
	}
	st := readState(t, home)
	if len(st.Jobs) != 0 || len(st.Last) != 0 {
		t.Errorf("cronstate.json after reload = %+v, want empty", st)
	}
}

func mustLoadAll(t *testing.T, home string) *Config {
	t.Helper()
	cfg, err := LoadAllConfigs(home)
	if err != nil {
		t.Fatalf("LoadAllConfigs: %v", err)
	}
	return cfg
}
