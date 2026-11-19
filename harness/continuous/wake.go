package continuous

// wake.go: the wake subprocess. Crash isolation is the subprocess boundary: a wedged
// wake is orphaned by the timeout fence and never touches whichever parent spawned it.

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
	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/harness/datadir"
)

// ErrAwake is the one-live-wake guard's answer: a second wake for an
// already-awake agent is refused, never queued (the skip law of the
// cron fire guard, ported to spawns).
var ErrAwake = errors.New("continuous: agent already awake")

// wakeTickPayload is the raw-JSON shape cron fires for wake ticks.
type wakeTickPayload struct {
	Agent  string `json:"agent"`
	Job    string `json:"job"`
	Prompt string `json:"prompt"`
	Model string `json:"model,omitempty"`
}

// tickObserver returns the spine subscription that turns cron's wake ticks into spawns.
// The observer is cheap by contract - it validates and hands off to its own goroutine;
// a malformed payload kills the wake, never the emitter.
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
			if err := m.Wake(p.Agent, p.Prompt, p.Model); err != nil && !errors.Is(err, ErrAwake) {
				m.logf("wake %s: %v", p.Agent, err)
			}
		}()
	}
}

// childExe resolves the wake child binary.
func (m *Machine) childExe() (string, error) {
	if m.spawnExe != nil {
		return m.spawnExe()
	}
	return os.Executable()
}

// logf writes to stderr.
func (m *Machine) logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "continuous: "+format+"\n", args...)
}

// Wake spawns one wake for the named agent: a human wake or a cron tick dispatch. model
// is the job's per-fire model override (the defaulting law: empty = inherit - the
// agent's own provider config, untouched); non-empty it rides OMEGA_MODEL on the
// child's env, the same wall the per-wake turn budget rides, so the override dies with
// the wake and never leaks into any other session. The call returns once the child is
// RUNNING, never when its loop finishes; the report lands in the agent's own canonical
// session by the child's own persistence.
func (m *Machine) Wake(name, message, model string) error {
	// The soft-disable law (v0.14.1). A disabled agent's home is never touched.
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
	go m.runWake(p, message, model)
	return nil
}

// Sleep terminates a live wake (the roster's sleep button).
func (m *Machine) Sleep(name string) {
	m.mu.Lock()
	cancel, ok := m.live[name]
	m.mu.Unlock()
	if ok && cancel != nil {
		cancel()
	}
}

// runWake builds and runs the child process for one wake.
func (m *Machine) runWake(p agent.Profile, message, model string) {
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
	// The child runs ONE loop bound to the canonical chat: the flag makes it persist input
	// + report into the session instead of printing and dropping (the run --session
	// contract).
	args := []string{"run", "--session", CanonicalSessionIDFor(p.Name)}
	if message != "" {
		args = append(args, message)
	}
	timeout := time.Duration(m.cfg.Timeout()) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Dir = p.Dir
	// Environment: the ROOT home.
	cmd.Env = append(os.Environ(),
		"OMEGA_HOME="+m.home,
		"OMEGA_AGENT="+p.Name,
	)
	if m.cfg.WakeTurnBudget > 0 {
		cmd.Env = append(cmd.Env, fmt.Sprintf("OMEGA_MAX_TURNS=%d", m.cfg.WakeTurnBudget))
	}
	if model != "" {
		cmd.Env = append(cmd.Env, "OMEGA_MODEL="+model)
	}
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
		// A failed wake is logged and done, nothing else: reports land only via the child's
		// own sink, and a crash never queues a retry (the skip law).
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

// Evidence reads the named agent's evidence ledger: rows grouped by run, latest first.
// When is an agent-home read but NOT a roster/canonical-chat read - it rides the
// allow-list as its own item because names the ledger as runtime-owned evidence the
// roster must be able to cite; it reads NOTHING else about the home.
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

// WakeNow is the roster's wake button.
func (m *Machine) WakeNow(name string) error {
	return m.Wake(name, "", "")
}
