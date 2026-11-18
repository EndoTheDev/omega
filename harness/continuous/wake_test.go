package continuous


import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/agent"
)

// testWaitTimeout bounds every poll-until-true wait in this package's tests. It exists
// to catch a HANG, not to time correctness: a healthy condition can take seconds when
// the full suite runs packages in parallel and starves the goroutine under test.
const testWaitTimeout = 30 * time.Second

// stubExeScript writes an executable child script that records its argv, cwd, and the.
// Windows gets a.bat (CreateProcess cannot run a shebang script), posix keeps the sh
// stub.
func stubExeScript(t *testing.T, outPath string) func() (string, error) {
	t.Helper()
	if runtime.GOOS == "windows" {
		if stubExePath == "" {
			t.Fatal("wake stub exe missing (TestMain build failed?)")
		}
		// The stub reads its out path from the env.
		t.Setenv("WAKE_STUB_OUT", outPath)
		return func() (string, error) { return stubExePath, nil }
	}
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

// waitFile polls outPath until the child finished writing (the DONE sentinel) and the
// predicate succeeds, or the deadline passes (the spawn is asynchronous by contract).
func waitFile(t *testing.T, outPath string, pred func(string) bool) string {
	t.Helper()
	deadline := time.Now().Add(testWaitTimeout)
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

// testAgent scaffolds scout into home (the machine never runs CreateHome in spawn
// tests.
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
	if err := m.Wake("scout", "patrol now", ""); err != nil {
		t.Fatalf("Wake: %v", err)
	}
	data := waitFile(t, out, func(d string) bool { return strings.Contains(d, "ARGV:run") })
	// Args: the run is bound to the canonical chat (the M5 contract), under the agent's.
	if !strings.Contains(data, "--session "+CanonicalSessionIDFor("scout")) {
		t.Errorf("child not bound to the agent's canonical chat:\n%s", data)
	}
	if !strings.Contains(data, "OMEGA_AGENT=scout") {
		t.Errorf("OMEGA_AGENT missing:\n%s", data)
	}
	if !strings.Contains(data, "OMEGA_HOME="+home+"\n") {
		t.Errorf("OMEGA_HOME must be the ROOT home:\n%s", data)
	}
	if !strings.Contains(data, "OMEGA_MAX_TURNS=7") {
		t.Errorf("per-wake budget missing:\n%s", data)
	}
	if !strings.Contains(data, "PWD:"+p.Dir+"\n") {
		t.Errorf("child cwd = agent home expected:\n%s", data)
	}
	// The child exits, the live entry clears.
	wakeSleep(t, m, "scout")
}

func TestWakeOneLiveGuardAndSleep(t *testing.T) {
	home := testHome(t)
	testAgent(t, home)
	out := filepath.Join(t.TempDir(), "child.out")
	m := NewMachine(home, Default())
	m.setSpawnExe(stubExeScript(t, out))
	if err := m.Wake("scout", "hello", ""); err != nil {
		t.Fatal(err)
	}
	// The child armed (the DONE sentinel proves the stub fully wrote its record).
	waitFile(t, out, func(d string) bool { return strings.Contains(d, "OMEGA_AGENT=scout") })
	// A second wake while the child lives is refused, never queued.
	if err := m.Wake("scout", "again", ""); !errors.Is(err, ErrAwake) {
		t.Fatalf("second wake = %v, want ErrAwake", err)
	}
	wakeSleep(t, m, "scout")
	m.Sleep("scout")
}

func TestWakeMainNeverSpawns(t *testing.T) {
	home := testHome(t)
	m := NewMachine(home, Default())
	if err := m.Wake("default", "", ""); err == nil {
		t.Fatal("main accepted a subprocess wake")
	}
	if err := m.Wake("nobody", "", ""); err == nil {
		t.Fatal("unknown agent accepted a wake")
	}
}

// TestWakeModelOverrideRidesEnv: a cron job's per-fire model rides OMEGA_MODEL on the
// child's env.
func TestWakeModelOverrideRidesEnv(t *testing.T) {
	home := testHome(t)
	testAgent(t, home)
	out := filepath.Join(t.TempDir(), "model.out")
	m := NewMachine(home, Default())
	m.setSpawnExe(stubExeScript(t, out))
	if err := m.Wake("scout", "hello", "override-model"); err != nil {
		t.Fatal(err)
	}
	waitFile(t, out, func(d string) bool { return strings.Contains(d, "OMEGA_MODEL=override-model") })

	out2 := filepath.Join(t.TempDir(), "inherit.out")
	m2 := NewMachine(home, Default())
	m2.setSpawnExe(stubExeScript(t, out2))
	if err := m2.Wake("scout", "hello", ""); err != nil {
		t.Fatal(err)
	}
	data := waitFile(t, out2, func(d string) bool { return strings.Contains(d, "OMEGA_AGENT=scout") })
	if strings.Contains(data, "OMEGA_MODEL=") {
		t.Errorf("empty model must inherit, env carries an override:\n%s", data)
	}
	// Both wakes end before the temp dirs are cleaned (windows holds an open file.
	wakeSleep(t, m, "scout")
	wakeSleep(t, m2, "scout")
}

func TestTickObserverDispatchesAndSkips(t *testing.T) {
	home := testHome(t)
	testAgent(t, home)
	out := filepath.Join(t.TempDir(), "child.out")
	m := NewMachine(home, Default())
	m.setSpawnExe(stubExeScript(t, out))
	obs := m.tickObserver()

	obs(core.AgentStart{})
	obs(core.TickEvent{Module: "chat", Kind: "wake", Payload: json.RawMessage(`{"agent":"scout"}`)})
	if m.LiveWake("scout") {
		t.Fatal("foreign tick woke an agent")
	}
	// Malformed payloads are skipped, never invented (loud consumers validate.
	obs(core.TickEvent{Module: "cron", Kind: "wake", Payload: json.RawMessage(`not-json`)})
	obs(core.TickEvent{Module: "cron", Kind: "wake", Payload: nil})
	obs(core.TickEvent{Module: "cron", Kind: "wake", Payload: json.RawMessage(`{}`)})
	if m.LiveWake("scout") {
		t.Fatal("malformed tick woke an agent")
	}
	payload, err := json.Marshal(wakeTickPayload{Agent: "scout", Job: "wake", Prompt: "tick patrol"})
	if err != nil {
		t.Fatal(err)
	}
	obs(core.TickEvent{Module: "cron", Kind: "wake", Payload: payload})
	waitFile(t, out, func(d string) bool { return strings.Contains(d, "tick patrol") })
	wakeSleep(t, m, "scout")
}
