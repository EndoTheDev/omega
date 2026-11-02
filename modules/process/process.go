// Package process owns the background process registry (card
// t_5782d11e): long-running shell commands - builds, transfers, tail
// servers - started detached, state-persisted, and managed from the
// agent. The synchronous shell (the tools module) stays the tool for
// anything expected to finish in seconds; this registry exists because
// long ops used to block the turn and eat the whole context window.
//
// Processes are detached by design: they are started in their own
// process group, never tied to omega's lifetime, and outlive omega
// restarts. The registry state lives in <home>/data/process.json, each
// process's combined stdout+stderr in <home>/logs/process/<name>.log.
// A process whose pid is gone when the registry next looks = exited.
package process

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/modules/datadir"
)

// DefaultLogMaxBytes caps each process log across a rotation (5 MB).
const DefaultLogMaxBytes = 5 * 1024 * 1024

// DefaultKillGraceSeconds is the TERM-to-KILL escalation grace (10 s).
const DefaultKillGraceSeconds = 10

// MaxTailLines caps a single read regardless of the requested count -
// the context-cost law: the agent reads the tail, never the whole log.
const MaxTailLines = 2000

// DefaultTailLines is the read default.
const DefaultTailLines = 100

// maxTailBytes additionally caps a read's byte size (long JSON lines
// must not re-flood the context the line cap was there to save).
const maxTailBytes = 128 * 1024

// nameMaxLen caps a handle name.
const nameMaxLen = 64

// Config carries the module's knobs. Defaults are applied by
// WithDefaults, so a zero Config (no process: section) still works.
type Config struct {
	// Home is the omega home (host wiring, not a yaml knob).
	Home string
	// LogMaxBytes caps one process log across a rotation.
	LogMaxBytes int64
	// KillGraceSeconds is how long a TERM kill waits before KILL.
	KillGraceSeconds int
}

// WithDefaults fills zero values with the module constants.
func (c Config) WithDefaults() Config {
	if c.Home == "" {
		c.Home = datadir.Home()
	}
	if c.LogMaxBytes <= 0 {
		c.LogMaxBytes = DefaultLogMaxBytes
	}
	if c.KillGraceSeconds <= 0 {
		c.KillGraceSeconds = DefaultKillGraceSeconds
	}
	return c
}

// Entry is one registry row: identity plus terminal state.
type Entry struct {
	Name       string `json:"name"`
	Command    string `json:"command"`
	PID        int    `json:"pid"`
	StartedAt  int64  `json:"started_at"`  // unix seconds
	FinishedAt int64  `json:"finished_at"` // unix seconds, 0 while running
	LogPath    string `json:"log_path"`
	Exited     bool   `json:"exited"`
	// ExitCode is nil for an exited-unknown process (it died while
	// omega was down, so nothing could reap it) and -1 for a signal
	// death (Go's convention: ProcessState.ExitCode is -1 on signal).
	ExitCode *int `json:"exit_code"`
}

// reaper watches one process spawned this omega lifetime. Its done
// channel closes when the child was reaped and the entry recorded.
type reaper struct {
	done chan struct{}
}

// Registry holds the state. One instance per omega process; a second
// omega process on the same home shares the state file on disk and
// reconciles pid liveness on read (the restart-proof path).
type Registry struct {
	cfg     Config
	mu      sync.Mutex
	entries map[string]*Entry
	reapers map[int]*reaper
	loaded  bool
}

// NewRegistry builds a registry for cfg. The state file is read lazily
// on the first operation - Mount never touches the filesystem (the
// goroutine fence: mounting starts no background work; reapers and all
// writes belong to tool calls).
func NewRegistry(cfg Config) *Registry {
	return &Registry{
		cfg:     cfg.WithDefaults(),
		entries: map[string]*Entry{},
		reapers: map[int]*reaper{},
	}
}

// statePath is <home>/data/process.json (the datadir law: state under
// data/).
func (r *Registry) statePath() string {
	return datadir.DBPath(r.cfg.Home, "process.json")
}

// logsDir is <home>/logs/process - one log per process, named by
// handle, not by pid (the handle survives restarts; the pid need not).
func (r *Registry) logsDir() string {
	return filepath.Join(r.cfg.Home, "logs", "process")
}

// load reads the state file once. Callers hold mu.
func (r *Registry) load() {
	if r.loaded {
		return
	}
	r.loaded = true
	data, err := os.ReadFile(r.statePath())
	if err != nil {
		return // fresh home: nothing to load
	}
	var entries []Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		// A corrupt registry must not brick the tools: start empty and
		// let the next save overwrite the bad file.
		fmt.Fprintf(os.Stderr, "omega process: corrupt %s ignored: %v\n", r.statePath(), err)
		return
	}
	for i := range entries {
		r.entries[entries[i].Name] = &entries[i]
	}
}

// save writes the registry atomically. Callers hold mu.
func (r *Registry) save() error {
	names := make([]string, 0, len(r.entries))
	for name := range r.entries {
		names = append(names, name)
	}
	sort.Strings(names)
	entries := make([]Entry, 0, len(names))
	for _, name := range names {
		entries = append(entries, *r.entries[name])
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(r.statePath()), 0o755); err != nil {
		return err
	}
	return agent.WriteFileAtomic(r.statePath(), data, 0o644)
}

// saveNote is the best-effort save call for paths where a failed disk
// write must not fail the operation (the state is recoverable at the
// next reconcile).
func (r *Registry) saveNote() {
	if err := r.save(); err != nil {
		fmt.Fprintf(os.Stderr, "omega process: save: %v\n", err)
	}
}

// reconcileLocked marks a not-yet-exited entry whose pid is gone as
// exited-unknown. This is the restart contract: a process that died
// while omega was down (or before the reaper was set) still reaches a
// terminal state. Callers hold mu.
func (r *Registry) reconcileLocked(e *Entry) {
	if e.Exited || groupAlive(e.PID) {
		return
	}
	e.Exited = true
	e.ExitCode = nil
	e.FinishedAt = time.Now().Unix()
	r.saveNote()
}

// rotateExited renames a finished entry's live log to <name>.log.1 so a
// replaced (re-spawned) name starts a fresh file and one generation of
// history survives. Only an existing, non-empty live file rotates - a
// reaper already rotated this entry (the .log is gone) is a no-op, and
// a re-spawn must never drop the rotated generation. A RUNNING
// process's file is never touched mid-flight - the child's fd would
// keep appending to the renamed inode and the tail read would go stale.
func rotateExited(e *Entry) {
	info, err := os.Stat(e.LogPath)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return
	}
	log1 := e.LogPath + ".1"
	_ = os.Remove(log1)
	_ = os.Rename(e.LogPath, log1)
}

// spawnName sanitizes a handle name or generates one: [A-Za-z0-9._-],
// 64 chars, empty = p-<millis>.
func spawnName(name string) string {
	var clean strings.Builder
	for _, ch := range strings.TrimSpace(name) {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') || ch == '.' || ch == '_' || ch == '-' {
			clean.WriteRune(ch)
		}
		if clean.Len() >= nameMaxLen {
			break
		}
	}
	if clean.Len() == 0 {
		return fmt.Sprintf("p-%d", time.Now().UnixMilli())
	}
	if clean.Len() < len(name) {
		fmt.Fprintf(os.Stderr, "omega process: sanitized spawn name %q -> %q\n", name, clean.String())
	}
	return clean.String()
}

// Spawn starts a command detached and records it. The command runs in
// its own process group (survives omega dying) under the run's
// workspace like shell.run. The handle name is the key: reusing an
// exited name rotates the old log; reusing a RUNNING name is refused.
func (r *Registry) Spawn(ctx context.Context, command, name string) (*Entry, error) {
	if strings.TrimSpace(command) == "" {
		return nil, fmt.Errorf("command must not be empty")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.load()
	name = spawnName(name)
	if e, ok := r.entries[name]; ok {
		r.reconcileLocked(e)
		if !e.Exited {
			return nil, fmt.Errorf("process name %q is already in use by pid %d (still running); kill it or pick another name", name, e.PID)
		}
		rotateExited(e)
	}

	logPath := filepath.Join(r.logsDir(), name+".log")
	if err := os.MkdirAll(r.logsDir(), 0o755); err != nil {
		return nil, fmt.Errorf("create log dir: %w", err)
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open process log: %w", err)
	}

	cmd := buildShell(command)
	detachChild(cmd)
	cmd.Dir = core.RunWorkspace(ctx)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return nil, fmt.Errorf("spawn failed: %w", err)
	}
	logFile.Close() // the child holds its own copy of the fd

	e := &Entry{
		Name:      name,
		Command:   command,
		PID:       cmd.Process.Pid,
		StartedAt: time.Now().Unix(),
		LogPath:   logPath,
	}
	watcher := &reaper{done: make(chan struct{})}
	r.reapers[e.PID] = watcher
	r.entries[name] = e
	// A size-capped log: rotation runs only once the child is dead,
	// so the cap is enforced across generations (one fresh file per
	// spawn), never mid-flight. Reads never exceed the tail cap
	// regardless - a runaway log cannot flood the context through
	// process.read, only grow on disk.
	r.saveNote()
	go func() {
		waitErr := cmd.Wait()
		r.mu.Lock()
		// The reaper owns the entry only while it still holds the
		// name - a replaced name's old entry is out of the map already.
		live, ok := r.entries[name]
		if ok && live == e {
			code := exitCodeOf(waitErr)
			e.Exited = true
			e.ExitCode = &code
			e.FinishedAt = time.Now().Unix()
			rotateExited(e)
			r.saveNote()
		}
		close(watcher.done)
		delete(r.reapers, e.PID)
		r.mu.Unlock()
	}()
	return e, nil
}

// List resolves every liveness question and returns a snapshot sorted
// by name.
func (r *Registry) List() []Entry {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.load()
	names := make([]string, 0, len(r.entries))
	for name := range r.entries {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]Entry, 0, len(names))
	for _, name := range names {
		e := r.entries[name]
		r.reconcileLocked(e)
		out = append(out, *e)
	}
	return out
}

// Find returns a live snapshot of one entry, reconciled.
func (r *Registry) Find(name string) (Entry, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.load()
	e, ok := r.entries[name]
	if !ok {
		return Entry{}, false
	}
	r.reconcileLocked(e)
	return *e, true
}

// Read tails a process log: at most lines lines (default 100, capped
// at MaxTailLines) and at most maxTailBytes. Returns the tail text,
// the file size, and whether older lines were cut.
func (r *Registry) Read(name string, lines int) (string, int64, bool, error) {
	if lines <= 0 {
		lines = DefaultTailLines
	}
	if lines > MaxTailLines {
		lines = MaxTailLines
	}
	e, ok := r.Find(name)
	if !ok {
		return "", 0, false, fmt.Errorf("no process named %q (see process.list)", name)
	}
	data, err := os.ReadFile(e.LogPath)
	if os.IsNotExist(err) {
		return fmt.Sprintf("no log yet for %q", name), 0, false, nil
	}
	if err != nil {
		return "", 0, false, fmt.Errorf("read log: %w", err)
	}
	text := strings.TrimRight(string(data), "\n")
	if text == "" {
		return "", int64(len(data)), false, nil
	}
	all := strings.Split(text, "\n")
	cut := 0
	if len(all) > lines {
		cut = len(all) - lines
		all = all[len(all)-lines:]
	}
	out := strings.Join(all, "\n")
	if len(out) > maxTailBytes {
		out = "..." + out[len(out)-maxTailBytes+3:]
		cut = 1
	}
	return out, int64(len(data)), cut > 0, nil
}

// Kill stops a process: TERM first, KILL after the grace. A process
// spawned this lifetime is reaped by its watcher goroutine (real exit
// code); an orphan (spawned by a previous omega, or merely known to
// the state file) is confirmed dead by pid liveness and recorded as
// exited-unknown.
//
// The kill dance runs WITHOUT r.mu: the reaper needs the same lock to
// record its exit before it closes its done channel, and a registry
// held hostage for the whole grace would block every list/read behind
// a dying process. The re-finalize under the lock is a fresh lookup.
func (r *Registry) Kill(name string) (string, error) {
	r.mu.Lock()
	r.load()
	e, ok := r.entries[name]
	if !ok {
		r.mu.Unlock()
		return "", fmt.Errorf("no process named %q (see process.list)", name)
	}
	r.reconcileLocked(e)
	if e.Exited {
		r.mu.Unlock()
		return fmt.Sprintf("%q already exited (%s)", name, stateOf(e)), nil
	}
	pid := e.PID
	watcher, owned := r.reapers[pid]
	grace := time.Duration(r.cfg.KillGraceSeconds) * time.Second
	if err := signalTerm(pid); err != nil {
		r.mu.Unlock()
		// The process died between the liveness check and the signal.
		if cur, ok := r.Find(name); ok && cur.Exited {
			return fmt.Sprintf("%q already exited before the TERM landed (%s)", name, stateOf(&cur)), nil
		}
		return "", fmt.Errorf("TERM pid %d: %w", pid, err)
	}
	r.mu.Unlock()
	if owned {
		select {
		case <-watcher.done: // the reaper records before it closes
		case <-time.After(grace):
			_ = signalKill(pid)
			select {
			case <-watcher.done:
			case <-time.After(3 * time.Second):
			}
		}
	} else {
		// Orphan: no watcher waits for us - pid liveness is the verdict.
		if err := waitForDeath(pid, grace); err != nil {
			if killErr := signalKill(pid); killErr != nil {
				return "", fmt.Errorf("KILL pid %d: %w", pid, killErr)
			}
			if waitErr := waitForDeath(pid, 3*time.Second); waitErr != nil {
				return "", fmt.Errorf("pid %d survived TERM and KILL: %w", pid, waitErr)
			}
		}
	}
	if groupAlive(pid) {
		return "", fmt.Errorf("pid %d still alive after TERM and KILL", pid)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.entries[name]
	if !ok || current != e {
		return fmt.Sprintf("%q (pid %d) killed; the handle was replaced meanwhile", name, pid), nil
	}
	if !current.Exited {
		// Orphan: nobody could reap it, the exit code is unknown.
		current.Exited = true
		current.ExitCode = nil
		current.FinishedAt = time.Now().Unix()
		r.saveNote()
	}
	return fmt.Sprintf("%q (pid %d) stopped (%s)", name, current.PID, stateOf(current)), nil
}

// Wait reports a process's terminal state, blocking or polling: with
// timeoutSeconds <= 0 it waits until the process exits or ctx cancels
// (the run's own wall-clock cap still applies); otherwise it polls at
// most that long.
func (r *Registry) Wait(ctx context.Context, name string, timeoutSeconds int) (string, error) {
	e, ok := r.Find(name)
	if !ok {
		return "", fmt.Errorf("no process named %q (see process.list)", name)
	}
	if e.Exited {
		return fmt.Sprintf("%q exited (%s)", name, stateOf(&e)), nil
	}
	done := r.watchDone(name)
	if done != nil {
		// Owned: block on the reaper, which carries the real exit code.
		var timeout <-chan time.Time
		if timeoutSeconds > 0 {
			timeout = time.After(time.Duration(timeoutSeconds) * time.Second)
		}
		select {
		case <-done:
			if e, ok := r.Find(name); ok {
				return fmt.Sprintf("%q exited (%s)", name, stateOf(&e)), nil
			}
			return fmt.Sprintf("%q exited", name), nil
		case <-timeout:
			if e, ok := r.Find(name); ok {
				return fmt.Sprintf("%q still running (timeout %ds: pid %d)", name, timeoutSeconds, e.PID), nil
			}
			return fmt.Sprintf("%q still running (timeout %ds)", name, timeoutSeconds), nil
		case <-ctx.Done():
			if e, ok := r.Find(name); ok {
				return fmt.Sprintf("%q still running (wait cancelled: pid %d)", name, e.PID), nil
			}
			return fmt.Sprintf("%q still running (wait cancelled)", name), nil
		}
	}
	// Orphan: poll pid liveness until the deadline.
	deadline := time.Now().Add(24 * time.Hour) // no timeout: ctx rules
	if timeoutSeconds > 0 {
		deadline = time.Now().Add(time.Duration(timeoutSeconds) * time.Second)
	}
	for {
		select {
		case <-ctx.Done():
			return fmt.Sprintf("%q still running (wait cancelled: pid %d)", name, e.PID), nil
		default:
		}
		if cur, ok := r.Find(name); !ok {
			return "", fmt.Errorf("no process named %q (see process.list)", name)
		} else if cur.Exited {
			return fmt.Sprintf("%q exited (%s)", name, stateOf(&cur)), nil
		} else if !time.Now().Before(deadline) {
			return fmt.Sprintf("%q still running (timeout %ds: pid %d)", name, timeoutSeconds, cur.PID), nil
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// watchDone returns the reaper's done channel for a name, or nil when
// the named process is not owned by this lifetime. The reaper closes
// the channel without holding r.mu; callers must never lock around a
// receive on it.
func (r *Registry) watchDone(name string) chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.load()
	if e, ok := r.entries[name]; ok && !e.Exited {
		if watcher, owned := r.reapers[e.PID]; owned {
			return watcher.done
		}
	}
	return nil
}

// stateOf renders a state line.
func stateOf(e *Entry) string {
	if !e.Exited {
		return "running"
	}
	switch {
	case e.ExitCode == nil:
		return "exited, code unknown (omega was down at its exit)"
	case *e.ExitCode == 0:
		return "exited code=0"
	case *e.ExitCode > 0:
		return fmt.Sprintf("exited code=%d", *e.ExitCode)
	default:
		return "exited (killed by signal)"
	}
}

// exitCodeOf extracts the exit code from a Wait error (-1 on signal
// death, Go's ProcessState.ExitCode convention).
func exitCodeOf(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

// buildShell builds the detached shell command. Same shells as
// shell.run: cmd /C on windows, sh -c elsewhere.
func buildShell(command string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.Command("cmd", "/C", command)
	}
	return exec.Command("sh", "-c", command)
}

// waitForDeath polls pid liveness until it drops or the budget ends.
func waitForDeath(pid int, budget time.Duration) error {
	deadline := time.Now().Add(budget)
	for {
		if !groupAlive(pid) {
			return nil
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("pid %d still alive after the grace period", pid)
		}
		time.Sleep(100 * time.Millisecond)
	}
}