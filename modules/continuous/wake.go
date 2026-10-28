package continuous

// wake.go: the wake subprocess (card t_72ecca67, rulings 7-9). A wake
// is one spawned omega binary invoked against the agent home, running
// ONE budgeted agent loop; the process death IS the sleep (ruling 7).
// Crash isolation is the subprocess boundary: a wedged wake is orphaned
// by the timeout fence and never touches whichever parent spawned it
// (ruling 9). The wake report is the run's closing message persisted
// into the agent's canonical session by the child itself (`omega run
// --session canonical`, the TurnSink ride); the parent only spawns,
// guards one live wake per agent, and keeps the log.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/engine/agent"
	"github.com/EndoTheDev/omega/modules/datadir"
)

// ErrAwake is the one-live-wake guard's answer: a second wake for an
// already-awake agent is refused, never queued (the skip law of the
// cron fire guard, ported to spawns).
var ErrAwake = errors.New("continuous: agent already awake")

// wakeTickPayload is the raw-JSON shape cron fires for wake ticks
// (payload law: raw JSON, namespaced strings - no Go types cross).
type wakeTickPayload struct {
	Agent  string `json:"agent"`
	Job    string `json:"job"`
	Prompt string `json:"prompt"`
}

// tickObserver returns the spine subscription that turns cron's wake
// ticks into spawns (ruling 13: cron fires; continuous owns WHAT). The
// observer is cheap by contract - it validates and hands off to its own
// goroutine; a malformed payload kills the wake, never the emitter.
func (m *Machine) tickObserver() core.EventObserver {
	return func(ev core.Event) {
		tick, ok := core.Tick(ev)
		if !ok || tick.Kind != "wake" || tick.Module != "cron" {
			return
		}
		var p wakeTickPayload
		if err := json.Unmarshal(tick.Payload, &p); err != nil || p.Agent == "" {
			return // malformed tick: skip, never invent a wake
		}
		go func() {
			if err := m.Wake(p.Agent, p.Prompt); err != nil && !errors.Is(err, ErrAwake) {
				m.logf("wake %s: %v", p.Agent, err)
			}
		}()
	}
}

// childExe resolves the wake child binary: os.Executable by law
// (one binary, many homes); the swappable hook exists for tests.
func (m *Machine) childExe() (string, error) {
	if m.spawnExe != nil {
		return m.spawnExe()
	}
	return os.Executable()
}

// logf writes to stderr. The subprocess log law (ruling 1: the home
// holds <name>.log) is served by the child's own capture; this path is
// for parent-side spawn failures only.
func (m *Machine) logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "continuous: "+format+"\n", args...)
}

// Wake spawns one wake for the named agent: a human wake (ruling 8's
// second trigger) or a cron tick dispatch. One live wake per agent -
// a second call returns ErrAwake. The call returns once the child is
// RUNNING, never when its loop finishes; the report lands in the
// agent's own canonical session by the child's own persistence.
func (m *Machine) Wake(name, message string) error {
	// The soft-disable law (v0.14.1): every wake trigger - the cron
	// tick, the human's canonical-chat POST, a scripted spawn - passes
	// this one wall. A disabled agent's home is never touched.
	if m.agentDisabled(name) {
		return fmt.Errorf("continuous: agent %s is disabled (config.yaml disabled.agents) - wake refused", name)
	}
	p, err := agent.LoadProfile(m.home, name)
	if err != nil {
		return fmt.Errorf("continuous: wake: %w", err)
	}
	if !p.Continuous {
		return fmt.Errorf("continuous: wake %s: main never wakes as a subprocess", name)
	}
	m.mu.Lock()
	if _, awake := m.live[name]; awake {
		m.mu.Unlock()
		return ErrAwake
	}
	m.live[name] = func() {} // placeholder replaced when the child arms
	m.mu.Unlock()
	go m.runWake(p, message)
	return nil
}

// Sleep terminates a live wake (the roster's sleep button). An agent
// with no live wake sleeps already (ruling 7) - no-op, not an error.
func (m *Machine) Sleep(name string) {
	m.mu.Lock()
	cancel, ok := m.live[name]
	m.mu.Unlock()
	if ok && cancel != nil {
		cancel()
	}
}

// runWake builds and runs the child process for one wake. Guarded by
// Wake's lock bracket; clears the live entry when the child exits.
func (m *Machine) runWake(p agent.Profile, message string) {
	defer func() {
		m.mu.Lock()
		delete(m.live, p.Name)
		m.mu.Unlock()
	}()

	exe, err := m.childExe()
	if err != nil {
		m.logf("wake %s: resolve binary: %v", p.Name, err)
		return
	}
	// The child runs ONE loop bound to the canonical chat: the flag
	// makes it persist input + report into the session instead of
	// printing and dropping (the run --session contract).
	args := []string{"run", "--session", CanonicalSessionID}
	if message != "" {
		args = append(args, message)
	}
	// The timeout fence is the second crash wall: the subprocess
	// boundary keeps a wedged wake from the parent, the fence ends a
	// wedged wake at all.
	timeout := time.Duration(m.cfg.Timeout()) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Dir = p.Dir
	// Environment: the ROOT home (datadir.AgentHome re-derives the
	// agent's own data dir from it), the agent name (the layer + the
	// data-isolation key), and the per-wake turn budget riding the
	// existing max-turns machinery (ruling 17 - config.go's env table).
	cmd.Env = append(os.Environ(),
		"OMEGA_HOME="+m.home,
		"OMEGA_AGENT="+p.Name,
	)
	if m.cfg.WakeTurnBudget > 0 {
		cmd.Env = append(cmd.Env, fmt.Sprintf("OMEGA_MAX_TURNS=%d", m.cfg.WakeTurnBudget))
	}
	// The log: the agent home holds <name>.log (ruling 1). stdout of a
	// run child is the report text; both streams append here.
	log, err := os.OpenFile(filepath.Join(p.Dir, p.Name+".log"),
		os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err == nil {
		defer log.Close()
		cmd.Stdout = log
		cmd.Stderr = log
	}
	m.mu.Lock()
	m.live[p.Name] = cancel
	m.mu.Unlock()

	cmd.Cancel = func() error { // sleep button + timeout fence: terminate
		if cmd.Process != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
	cmd.WaitDelay = 5 * time.Second
	if err := cmd.Run(); err != nil && ctx.Err() != context.DeadlineExceeded {
		// A failed wake is logged and done, nothing else: reports land
		// only via the child's own sink, and a crash never queues a
		// retry (the skip law). The timeout fence logs its own line.
		m.logf("wake %s: %v", p.Name, err)
	}
	if ctx.Err() == context.DeadlineExceeded {
		m.logf("wake %s: fence fired after %s", p.Name, timeout)
	}
}

// LiveWake reports whether the named agent currently has a wake running.
func (m *Machine) LiveWake(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.live[name]
	return ok
}

// EvidenceRow is one ledger row as the detail strip renders it.
type EvidenceRow struct {
	ID       int64  `json:"id"`
	TS       string `json:"ts"`
	RunID    string `json:"run_id"`
	Tool     string `json:"tool"`
	ArgsHash string `json:"args_hash"`
	OK       bool   `json:"ok"`
	Verifier string `json:"verifier,omitempty"`
}

// Evidence reads the named agent's evidence ledger (ruling 11's read
// face): rows grouped by run, latest first. When is an agent-home read
// but NOT a roster/canonical-chat read - it rides the allow-list as its
// own item because ruling 11 names the ledger as runtime-owned evidence
// the roster must be able to cite; it reads NOTHING else about the home.
func (m *Machine) Evidence(name string) ([]EvidenceRow, error) {
	p, err := agent.LoadProfile(m.home, name)
	if err != nil {
		return nil, err
	}
	agentDataDir := datadir.Dir(p.Dir)
	lg, err := OpenLedger(agentDataDir)
	if err != nil {
		return nil, fmt.Errorf("continuous: %s ledger: %w", name, err)
	}
	defer lg.Close()
	return lg.All(context.Background())
}

// WakeNow is the roster's wake button: a manual wake carrying the
// agent's standing prompt.
func (m *Machine) WakeNow(name string) error {
	return m.Wake(name, "")
}
