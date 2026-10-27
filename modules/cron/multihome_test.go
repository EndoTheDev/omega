package cron

// multihome_test.go: the multi-home WHEN self-checks (card t_72ecca67,
// ruling 12). One module reads main's crons.yaml AND each agent home's
// config/crons.yaml; agent jobs are wakes - one tick through the event
// spine, never an in-process turn; continuous off = inert.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/engine/agent"
)

func TestLoadAllConfigsReadsAgentHomes(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "crons.yaml"),
		[]byte("jobs:\n  - name: main-job\n    schedule: \"0 8 * * *\"\n    prompt: p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := agent.ScaffoldAgent(home, "scout", ""); err != nil {
		t.Fatal(err)
	}
	// Two agents can honestly carry the same job name - the roster of
	// the state keys namespaces them (scout/wake, warden/wake).
	if err := agent.ScaffoldAgent(home, "warden", ""); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "agents", "scout", "config", "crons.yaml"),
		[]byte("jobs:\n  - name: wake\n    schedule: \"17 * * * *\"\n    prompt: patrol\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "agents", "warden", "config", "crons.yaml"),
		[]byte("jobs:\n  - name: wake\n    schedule: \"23 * * * *\"\n    prompt: guard\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadAllConfigs(home)
	if err != nil {
		t.Fatalf("LoadAllConfigs: %v", err)
	}
	if len(cfg.Jobs) != 3 {
		t.Fatalf("jobs = %d, want 3", len(cfg.Jobs))
	}
	byKey := map[string]Job{}
	for _, j := range cfg.Jobs {
		byKey[j.StateKey()] = j
	}
	if j := byKey["main-job"]; j.Agent != "" {
		t.Errorf("main job agent = %q, want empty", j.Agent)
	}
	if j := byKey["scout/wake"]; !j.IsAgent() || j.Prompt != "patrol" || j.Schedule != "17 * * * *" {
		t.Errorf("scout/wake = %+v", j)
	}
	if j := byKey["warden/wake"]; !j.IsAgent() || j.Prompt != "guard" {
		t.Errorf("warden/wake = %+v", j)
	}
}

func TestLoadAllConfigsRejectsAgentDeliver(t *testing.T) {
	home := t.TempDir()
	if err := agent.ScaffoldAgent(home, "scout", ""); err != nil {
		t.Fatal(err)
	}
	// A wake's delivery IS its report; a deliver target would cross
	// the home boundary (ruling 10). Loud rejection.
	if err := os.WriteFile(filepath.Join(home, "agents", "scout", "config", "crons.yaml"),
		[]byte("jobs:\n  - name: wake\n    schedule: \"17 * * * *\"\n    prompt: patrol\n    deliver: journal\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAllConfigs(home); err == nil {
		t.Fatal("agent job with deliver target accepted")
	}
}

func TestAgentJobFiresWakeTickNotTurn(t *testing.T) {
	var payload *wakeTickPayload
	r := newTestRunner(t, t.TempDir(),
		Job{Name: "wake", Schedule: "17 * * * *", Prompt: "patrol", Agent: "scout"})
	r.runTurn = func(ctx context.Context, sessionID string, job Job) (string, error) {
		t.Error("agent job ran an in-process turn")
		return "", nil
	}
	r.tickEmit = func(p wakeTickPayload) { payload = &p }
	r.fire(context.Background(), r.jobsSnapshot()[0], r.now())
	if payload == nil {
		t.Fatal("agent fire emitted no wake tick")
	}
	if payload.Agent != "scout" || payload.Job != "wake" || payload.Prompt != "patrol" {
		t.Errorf("tick payload = %+v", payload)
	}
	// The tick payload survives a JSON round trip (the payload law).
	raw, err := json.Marshal(*payload)
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
}

func TestAgentJobInertWithoutContinuous(t *testing.T) {
	r := newTestRunner(t, t.TempDir(),
		Job{Name: "wake", Schedule: "17 * * * *", Prompt: "patrol", Agent: "scout"})
	r.tickEmit = nil // the continuous capsule is toggled off
	// Off = wakes go inert, schedules stay (ruling 14): the fire is a
	// no-op that still records its occurrence - no panic, no spawn.
	r.fire(context.Background(), r.jobsSnapshot()[0], r.now())
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if v, _ := r.lastFireOf("scout/wake"); v != 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if v, _ := r.lastFireOf("scout/wake"); v == 0 {
		t.Error("agent fire occurrence never recorded")
	}
}

func TestAgentJobStateKeysNamespace(t *testing.T) {
	home := t.TempDir()
	for _, name := range []string{"scout", "warden"} {
		if err := agent.ScaffoldAgent(home, name, ""); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(home, "agents", name, "config", "crons.yaml"),
			[]byte("jobs:\n  - name: wake\n    schedule: \"17 * * * *\"\n    prompt: p\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := LoadAllConfigs(home)
	if err != nil {
		t.Fatal(err)
	}
	r := NewRunner(cfg, home)
	r.now = time.Now
	// Both fire: same yaml name, distinct state keys, both guarded.
	for _, j := range r.jobsSnapshot() {
		if !j.IsAgent() {
			t.Fatalf("unexpected main job in snapshot: %+v", j)
		}
		r.fire(context.Background(), j, r.now())
	}
	if _, ok := r.lastFireOf("scout/wake"); !ok {
		t.Error("scout/wake never recorded")
	}
	if _, ok := r.lastFireOf("warden/wake"); !ok {
		t.Error("warden/wake never recorded")
	}
}
