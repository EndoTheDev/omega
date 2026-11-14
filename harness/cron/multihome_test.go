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

	"github.com/EndoTheDev/omega/agent"
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
		Job{Name: "wake", Schedule: "17 * * * *", Prompt: "patrol", Agent: "scout", AgentOwned: true})
	r.runTurn = func(ctx context.Context, sessionID string, job Job, prompt string) (string, error) {
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
		Job{Name: "wake", Schedule: "17 * * * *", Prompt: "patrol", Agent: "scout", AgentOwned: true})
	r.tickEmit = nil // the continuous capsule is toggled off
	// Off = wakes go inert, schedules stay (ruling 14): the fire is a
	// no-op that still records its occurrence - no panic, no spawn.
	r.fire(context.Background(), r.jobsSnapshot()[0], r.now())
	deadline := time.Now().Add(testWaitTimeout)
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

// TestJobsListingMarksHomeOwnership pins the wire's ownership mark (card
// t_21824d98): a main-home continuous-agent job carrying an AUTHORED
// `agent:` and an agent home's wake row both put a non-empty Agent on the
// row, and only AgentOwned tells them apart. The editor's read-only
// refusal (and the toggle's state key) read that mark, so the
// authored-agent row keeps its pencil instead of stranding as a wake.
func TestJobsListingMarksHomeOwnership(t *testing.T) {
	home := t.TempDir()
	mainYAML := "jobs:\n" +
		"  - name: daily\n    schedule: \"0 8 * * *\"\n    prompt: p\n" +
		"  - name: pulse\n    kind: interval\n    minutes: 15\n" +
		"    deliver: continuous-agent\n    agent: ghost\n    prompt: p\n"
	if err := os.WriteFile(filepath.Join(home, "crons.yaml"), []byte(mainYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := agent.ScaffoldAgent(home, "scout", ""); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "agents", "scout", "config", "crons.yaml"),
		[]byte("jobs:\n  - name: wake\n    schedule: \"17 * * * *\"\n    prompt: patrol\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadAllConfigs(home)
	if err != nil {
		t.Fatalf("LoadAllConfigs: %v", err)
	}
	r := NewRunner(cfg, home)
	r.now = time.Now
	byName := map[string]JobInfo{}
	for _, row := range r.Jobs() {
		byName[row.Name] = row
	}
	if row := byName["pulse"]; row.Agent != "ghost" || row.AgentOwned {
		t.Errorf("authored-agent main row = %+v, want agent ghost and AgentOwned false (editable)", row)
	}
	if row := byName["daily"]; row.Agent != "" || row.AgentOwned {
		t.Errorf("plain main row = %+v, want no agent and AgentOwned false", row)
	}
	if row := byName["wake"]; row.Agent != "scout" || !row.AgentOwned {
		t.Errorf("agent-home row = %+v, want agent scout and AgentOwned true (read-only)", row)
	}
	// The wire carries it: agent_owned appears only on the owned row, so
	// the SPA can tell the two classes apart without guessing.
	rows := r.JobRows()
	if len(rows) != 3 {
		t.Fatalf("wire rows = %d, want 3", len(rows))
	}
	for _, raw := range rows {
		var wire struct {
			Name       string `json:"name"`
			AgentOwned bool   `json:"agent_owned"`
		}
		if err := json.Unmarshal(raw, &wire); err != nil {
			t.Fatalf("unmarshal row: %v", err)
		}
		if wire.Name == "pulse" && wire.AgentOwned {
			t.Errorf("authored-agent row marked agent_owned on the wire: %s", raw)
		}
		if wire.Name == "wake" && !wire.AgentOwned {
			t.Errorf("wake row missing agent_owned on the wire: %s", raw)
		}
	}
}
