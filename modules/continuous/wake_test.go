package continuous

// wake_test.go: the wake subprocess self-checks (rulings 7-9). The
// child binary is a stub script in every test - no test ever executes
// a real omega run; the contracts verified are the spawn surface (env,
// args, cwd, the log) and the guards (one live wake, sleep, ticks).

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// stubExeScript writes an executable child script that records its
// argv, cwd, and the OMEGA_ env deltas into outPath, then sleeps long
// enough for the guards to be observed. It runs as the wake child's
// binary with the machine's own argv.
func stubExeScript(t *testing.T, outPath string) func() (string, error) {
	t.Helper()
	script := filepath.Join(t.TempDir(), "wake-child")
	body := fmt.Sprintf(`#!/bin/sh
{
echo "ARGV:$*"
echo "PWD:$PWD"
env | grep '^OMEGA_' | sort
echo "DONE"
} >> %q
sleep 30
`, outPath)
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return func() (string, error) { return script, nil }
}

// waitFile polls outPath until the child finished writing (the DONE
// sentinel) and the predicate succeeds, or the deadline passes (the
// spawn is asynchronous by contract).
func waitFile(t *testing.T, outPath string, pred func(string) bool) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var data string
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(outPath); err == nil {
			data = string(b)
			if strings.Contains(data, "DONE") && pred(data) {
				return data
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("spawn marker never matched; captured:\n%s", data)
	return ""
}

// testAgent scaffolds scout into home (the machine never runs CreateHome
// in spawn tests - the agent exists, folder presence is registration).
func testAgent(t *testing.T, home string) agent.Profile {
	t.Helper()
	if err := agent.ScaffoldAgent(home, "scout", "test agent"); err != nil {
		t.Fatal(err)
	}
	p, err := agent.LoadProfile(home, "scout")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestWakeSpawnContract(t *testing.T) {
	home := testHome(t)
	p := testAgent(t, home)
	out := filepath.Join(t.TempDir(), "child.out")
	m := NewMachine(home, Config{WakeTurnBudget: 7, WakeTimeoutSeconds: 300})
	m.setSpawnExe(stubExeScript(t, out))
	if err := m.Wake("scout", "patrol now"); err != nil {
		t.Fatalf("Wake: %v", err)
	}
	data := waitFile(t, out, func(d string) bool { return strings.Contains(d, "ARGV:run") })
	// Args: the run is bound to the canonical chat (the M5 contract).
	if !strings.Contains(data, "--session "+CanonicalSessionID) {
		t.Errorf("child not bound to canonical chat:\n%s", data)
	}
	// Env: root home + agent isolation + the per-wake budget riding
	// OMEGA_MAX_TURNS (rulings 4, 17).
	if !strings.Contains(data, "OMEGA_AGENT=scout") {
		t.Errorf("OMEGA_AGENT missing:\n%s", data)
	}
	if !strings.Contains(data, "OMEGA_HOME="+home+"\n") {
		t.Errorf("OMEGA_HOME must be the ROOT home:\n%s", data)
	}
	if !strings.Contains(data, "OMEGA_MAX_TURNS=7") {
		t.Errorf("per-wake budget missing:\n%s", data)
	}
	// The child cwd is the agent home (ruling 1: an agent IS a home).
	if !strings.Contains(data, "PWD:"+p.Dir+"\n") {
		t.Errorf("child cwd = agent home expected:\n%s", data)
	}
	// The child exits, the live entry clears: wait for the marker then
	// verify the machine reports the agent asleep again (the sleep IS
	// the process death; the stub sleeps 30s so poll via Sleep kill).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !m.LiveWake("scout") {
			break
		}
		m.Sleep("scout")
		time.Sleep(100 * time.Millisecond)
	}
	if m.LiveWake("scout") {
		t.Fatal("wake never cleared after sleep")
	}
}

func TestWakeOneLiveGuardAndSleep(t *testing.T) {
	home := testHome(t)
	testAgent(t, home)
	out := filepath.Join(t.TempDir(), "child.out")
	m := NewMachine(home, Default())
	m.setSpawnExe(stubExeScript(t, out))
	if err := m.Wake("scout", "hello"); err != nil {
		t.Fatal(err)
	}
	// The child armed (the DONE sentinel proves the stub fully wrote
	// its record); the guard is already holding.
	waitFile(t, out, func(d string) bool { return strings.Contains(d, "OMEGA_AGENT=scout") })
	// A second wake while the child lives is refused, never queued.
	if err := m.Wake("scout", "again"); !errors.Is(err, ErrAwake) {
		t.Fatalf("second wake = %v, want ErrAwake", err)
	}
	// Sleep terminates the live child (the roster's sleep button).
	m.Sleep("scout")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && m.LiveWake("scout") {
		time.Sleep(50 * time.Millisecond)
	}
	if m.LiveWake("scout") {
		t.Fatal("sleep never ended the live wake")
	}
	// Sleeping a sleeping agent is a no-op, not an error.
	m.Sleep("scout")
}

func TestWakeMainNeverSpawns(t *testing.T) {
	home := testHome(t)
	m := NewMachine(home, Default())
	if err := m.Wake("default", ""); err == nil {
		t.Fatal("main accepted a subprocess wake")
	}
	if err := m.Wake("nobody", ""); err == nil {
		t.Fatal("unknown agent accepted a wake")
	}
}

func TestTickObserverDispatchesAndSkips(t *testing.T) {
	home := testHome(t)
	testAgent(t, home)
	out := filepath.Join(t.TempDir(), "child.out")
	m := NewMachine(home, Default())
	m.setSpawnExe(stubExeScript(t, out))
	obs := m.tickObserver()

	// Not a tick, not theirs: ignored without a side effect.
	obs(core.AgentStart{})
	obs(core.TickEvent{Module: "chat", Kind: "wake", Payload: json.RawMessage(`{"agent":"scout"}`)})
	if m.LiveWake("scout") {
		t.Fatal("foreign tick woke an agent")
	}
	// Malformed payloads are skipped, never invented (loud consumers
	// validate; the payload law keeps this JSON-raw).
	obs(core.TickEvent{Module: "cron", Kind: "wake", Payload: json.RawMessage(`not-json`)})
	obs(core.TickEvent{Module: "cron", Kind: "wake", Payload: nil})
	obs(core.TickEvent{Module: "cron", Kind: "wake", Payload: json.RawMessage(`{}`)})
	if m.LiveWake("scout") {
		t.Fatal("malformed tick woke an agent")
	}
	// The real shape cron fires.
	payload, err := json.Marshal(wakeTickPayload{Agent: "scout", Job: "wake", Prompt: "tick patrol"})
	if err != nil {
		t.Fatal(err)
	}
	obs(core.TickEvent{Module: "cron", Kind: "wake", Payload: payload})
	waitFile(t, out, func(d string) bool { return strings.Contains(d, "tick patrol") })
	m.Sleep("scout")
}
