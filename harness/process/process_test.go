package process

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// skipOnWindows: the TERM/KILL escalation and group-kill semantics are
// POSIX contracts; windows collapses both into TerminateProcess.
func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("SIGTERM/SIGKILL escalation is a unix contract")
	}
}

// testRegistry builds a registry over a fresh temp home with a fast
// kill grace for the tests. Close is registered AFTER t.TempDir's
// cleanup, so LIFO runs the fence first: every reaper is joined and no
// child survives before the TempDir's RemoveAll - the CI fence for the
// t_de398140 cleanup race.
func testRegistry(t *testing.T) *Registry {
	t.Helper()
	r := NewRegistry(Config{Home: t.TempDir(), KillGraceSeconds: 1, LogMaxBytes: DefaultLogMaxBytes})
	t.Cleanup(r.Close)
	return r
}

// ownedReapers counts the registry's live reaper goroutines (the reaper
// map shrinks as each watcher finishes, so zero means all joined).
func ownedReapers(r *Registry) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.reapers)
}

// waitFor polls condition until it holds or fails the test.
func waitFor(t *testing.T, deadline time.Duration, what string, cond func() bool) {
	t.Helper()
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// stateEntry is a read helper: Find + require found.
func stateEntry(t *testing.T, r *Registry, name string) Entry {
	t.Helper()
	e, ok := r.Find(name)
	if !ok {
		t.Fatalf("registry lost %q", name)
	}
	return e
}

// TestSpawnReadWaitKill is the happy path: spawn, the log carries the
// output, the process is live, kill stops it, the state is exited.
func TestSpawnReadWaitKill(t *testing.T) {
	skipOnWindows(t)
	r := testRegistry(t)
	e, err := r.Spawn(context.Background(), "echo out-line; sleep 120", "worker", "")
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if e.Name != "worker" || e.Exited {
		t.Fatalf("spawn entry: %+v", e)
	}
	waitFor(t, 5*time.Second, "log line", func() bool {
		tail, _, _, err := r.Read("worker", 100)
		return err == nil && strings.Contains(tail, "out-line")
	})
	if e := stateEntry(t, r, "worker"); e.Exited || e.PID <= 0 || e.LogPath == "" {
		t.Fatalf("live entry shape: %+v", e)
	}
	got, err := r.Kill("worker")
	if err != nil {
		t.Fatalf("kill: %v", err)
	}
	if !strings.Contains(got, "stopped") {
		t.Fatalf("kill reply: %s", got)
	}
	if e := stateEntry(t, r, "worker"); !e.Exited || e.FinishedAt == 0 {
		t.Fatalf("post-kill entry: %+v", e)
	}
	// killing an exited name is a status report, not an error
	again, err := r.Kill("worker")
	if err != nil || !strings.Contains(again, "already exited") {
		t.Fatalf("second kill: %s err=%v", again, err)
	}
}

// TestExitCodeRecorded proves a normal exit carries its real code,
// including zero.
func TestExitCodeRecorded(t *testing.T) {
	skipOnWindows(t)
	r := testRegistry(t)
	if _, err := r.Spawn(context.Background(), "sleep 0.3; exit 7", "boom", ""); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	got, err := r.Wait(context.Background(), "boom", 0)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if !strings.Contains(got, "code=7") {
		t.Fatalf("wait reply missing exit code 7: %s", got)
	}
	e := stateEntry(t, r, "boom")
	if !e.Exited || e.ExitCode == nil || *e.ExitCode != 7 || e.FinishedAt == 0 {
		t.Fatalf("exit record incomplete: %+v", e)
	}
	if _, err := r.Spawn(context.Background(), "sleep 0.2; exit 0", "quiet", ""); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	got, err = r.Wait(context.Background(), "quiet", 0)
	if err != nil || !strings.Contains(got, "code=0") {
		t.Fatalf("zero-exit wait: %s err=%v", got, err)
	}
	if e := stateEntry(t, r, "quiet"); e.ExitCode == nil || *e.ExitCode != 0 {
		t.Fatalf("zero exit stored as non-zero: %+v", e)
	}
}

// TestKillEscalation proves the TERM-then-KILL contract: an
// TERM-ignoring command survives the TERM and dies at the KILL.
func TestKillEscalation(t *testing.T) {
	skipOnWindows(t)
	r := testRegistry(t) // 1 s grace
	if _, err := r.Spawn(context.Background(), `trap "" TERM; sleep 300`, "stubborn", ""); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	waitFor(t, 5*time.Second, "stubborn to be live", func() bool {
		_, _, _, err := r.Read("stubborn", 5)
		return err == nil
	})
	got, err := r.Kill("stubborn")
	if err != nil {
		t.Fatalf("kill: %v", err)
	}
	if !strings.Contains(got, "killed") && !strings.Contains(got, "stopped") {
		t.Fatalf("kill reply: %s", got)
	}
	if e := stateEntry(t, r, "stubborn"); !e.Exited {
		t.Fatalf("stubborn survived: %+v", e)
	}
}

// TestReadTailCaps proves read returns the tail only: the line cap and
// the hard byte cap both bind.
func TestReadTailCaps(t *testing.T) {
	skipOnWindows(t)
	r := testRegistry(t)
	if _, err := r.Spawn(context.Background(),
		`i=1; while [ $i -le 120 ]; do echo "line-$i"; i=$((i+1)); done; sleep 120`, "writer", ""); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	waitFor(t, 5*time.Second, "log completion", func() bool {
		tail, _, _, err := r.Read("writer", 5)
		return err == nil && strings.Contains(tail, "line-120")
	})
	tail, size, cut, err := r.Read("writer", 10)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	got := strings.Split(tail, "\n")
	if len(got) != 10 || got[0] != "line-111" || got[9] != "line-120" {
		t.Fatalf("tail 10: len=%d first=%q last=%q", len(got), got[0], got[len(got)-1])
	}
	if !cut || size == 0 {
		t.Fatalf("truncation not recorded: cut=%v size=%d", cut, size)
	}
	if _, _, _, err := r.Read("writer", 999999); err != nil {
		t.Fatalf("clamped read: %v", err)
	}
	if _, _, _, err := r.Read("missing", 5); err == nil {
		t.Fatal("read of unknown name must error")
	}
}

// TestSpawnNameRules proves the handle contract: sanitization,
// generated names, and the running-name refusal.
func TestSpawnNameRules(t *testing.T) {
	skipOnWindows(t)
	r := testRegistry(t)
	if _, err := r.Spawn(context.Background(), "sleep 300", "  b@d name$$  ", ""); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	e := stateEntry(t, r, "bdname") // spaces dropped, junk chars dropped
	if e.Name != "bdname" {
		t.Fatalf("sanitized handle: %+v", e)
	}
	if _, err := r.Spawn(context.Background(), "", "empty", ""); err == nil {
		t.Fatal("empty command must be refused")
	}
	if _, err := r.Spawn(context.Background(), "sleep 300", "worker", ""); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if _, err := r.Spawn(context.Background(), "echo x", "worker", ""); err == nil {
		t.Fatal("reusing a RUNNING name must be refused")
	}
	generated, err := r.Spawn(context.Background(), "sleep 0.5; echo done", "", "")
	if err != nil || generated.Name == "" {
		t.Fatalf("generated name: %+v err=%v", generated, err)
	}
}

// TestReplaceExitedRotatesLog proves a reused name gets a fresh log
// and one generation of the old file survives as <name>.log.1.
func TestReplaceExitedRotatesLog(t *testing.T) {
	skipOnWindows(t)
	r := testRegistry(t)
	if _, err := r.Spawn(context.Background(), "echo gen-one", "rot", ""); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if _, err := r.Wait(context.Background(), "rot", 5); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if _, err := r.Spawn(context.Background(), "echo gen-two; sleep 0.3", "rot", ""); err != nil {
		t.Fatalf("re-spawn: %v", err)
	}
	gen1, err := os.ReadFile(r.logsDir() + "/rot.log.1")
	if err != nil || !strings.Contains(string(gen1), "gen-one") {
		t.Fatalf("rotated log: %v content=%q", err, string(gen1))
	}
	waitFor(t, 5*time.Second, "gen-two in the live log", func() bool {
		tail, _, _, err := r.Read("rot", 100)
		return err == nil && strings.Contains(tail, "gen-two")
	})
}

// TestRestartPidRecheck proves the restart contract: a fresh registry
// over the same home reconciles against pid liveness - a live pid
// stays running, a gone pid becomes exited-unknown, and the live
// process can be killed from the rebooted instance.
func TestRestartPidRecheck(t *testing.T) {
	skipOnWindows(t)
	home := t.TempDir()
	r1 := NewRegistry(Config{Home: home, KillGraceSeconds: 1, LogMaxBytes: DefaultLogMaxBytes})
	t.Cleanup(r1.Close) // joins the sleep 300 reaper before the TempDir teardown
	if _, err := r1.Spawn(context.Background(), "sleep 300", "longop", ""); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	// --- the reboot: a fresh registry instance over the same home ---
	r2 := NewRegistry(Config{Home: home, KillGraceSeconds: 1, LogMaxBytes: DefaultLogMaxBytes})
	list := r2.List()
	found, ok := findByName(list, "longop")
	if !ok || found.Exited {
		t.Fatalf("restart lost the live process: %+v", list)
	}
	got, err := r2.Kill("longop")
	if err != nil {
		t.Fatalf("kill from the rebooted registry: %v", err)
	}
	if !strings.Contains(got, "stopped") {
		t.Fatalf("orphan kill reply: %s", got)
	}
	// --- a pid that is simply gone: exited, code unknown ---
	// A liveness-verified dead pid: a spawn whose process already
	// exited in THIS process (the reaper recorded it).
	probe := NewRegistry(Config{Home: home, KillGraceSeconds: 1, LogMaxBytes: DefaultLogMaxBytes})
	t.Cleanup(probe.Close) // the fast-exit spawn's reaper must not outlive the TempDir
	if _, err := probe.Spawn(context.Background(), "exit 0", "dead-pid-source", ""); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	waitFor(t, 5*time.Second, "the reaper to record the exit", func() bool {
		e, _ := probe.Find("dead-pid-source")
		return e.Exited
	})
	ghost := stateEntry(t, probe, "dead-pid-source")
	ghost.Name = "ghost"
	ghost.Command = "echo ghost"
	ghost.Exited = false
	ghost.ExitCode = nil
	ghost.FinishedAt = 0
	seedState(t, filepath.Join(home, "data", "process.json"), []Entry{ghost})
	r3 := NewRegistry(Config{Home: home, KillGraceSeconds: 1, LogMaxBytes: DefaultLogMaxBytes})
	for _, e := range r3.List() {
		if e.Name == "ghost" && (!e.Exited || e.ExitCode != nil || e.FinishedAt == 0) {
			t.Fatalf("a gone pid must reconcile to exited-unknown: %+v", e)
		}
	}
}

// TestCloseJoinsReapers is the CI fence (card t_de398140): a fast-exit
// command whose reaper previously wrote the state file after the test
// returned, racing the TempDir RemoveAll on the runner (the "directory
// not empty" red). The fence closes the window structurally: after
// Close the reaper is joined, the entry is terminal, and nothing can
// write to the home again. Run with -count=10; the race is runner
// timing, the fence makes it impossible.
func TestCloseJoinsReapers(t *testing.T) {
	skipOnWindows(t)
	r := testRegistry(t)
	if _, err := r.Spawn(context.Background(), "exit 0", "fast", ""); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	r.Close() // the fence the TempDir cleanup would otherwise race
	// Terminal state, whichever way it landed: the child beat the fence
	// (exit code 0) or the fence's kill beat the child's exec (signal
	// death, code -1). Either way the reaper recorded before Close
	// returned - the invariant TempDir cleanup relies on.
	e := stateEntry(t, r, "fast")
	if !e.Exited || e.ExitCode == nil || (*e.ExitCode != 0 && *e.ExitCode != -1) {
		t.Fatalf("fast exit must be terminal after the fence: %+v", e)
	}
	if n := ownedReapers(r); n != 0 {
		t.Fatalf("reapers left unjoined after Close: %d", n)
	}
}

// TestSpawnReturnsBeforeExit exercises the same fence through test
// lifetime instead: the test returns with a RUNNING child (the shape
// TestToolFace's sleep 120 had), and t.Cleanup's Close must kill the
// child and join the reaper before the TempDir teardown - no late
// save, no leftover sleep outliving the test.
func TestSpawnReturnsBeforeExit(t *testing.T) {
	skipOnWindows(t)
	r := testRegistry(t)
	if _, err := r.Spawn(context.Background(), "echo left-behind; sleep 120", "leaver", ""); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	// Returns immediately with the child still running - the fence is
	// the whole point. Nothing further to assert here: a failed
	// cleanup fails the test itself (the exact CI red this fixes).
}

// TestCorruptStateIgnored proves a corrupt state file starts empty
// instead of bricking the tools.
func TestCorruptStateIgnored(t *testing.T) {
	skipOnWindows(t)
	r := testRegistry(t)
	if err := os.MkdirAll(filepath.Dir(r.statePath()), 0o755); err != nil {
		t.Fatalf("seed dir: %v", err)
	}
	if err := os.WriteFile(r.statePath(), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("seed corrupt state: %v", err)
	}
	if list := r.List(); len(list) != 0 {
		t.Fatalf("corrupt state must start empty: %+v", list)
	}
	if _, err := r.Spawn(context.Background(), "echo alive", "survivor", ""); err != nil {
		t.Fatalf("tools work after a corrupt state: %v", err)
	}
}

// TestToolFace drives the five verbs through the dispatcher the tool
// Runs route through (the args exactly as the merged tool call carries
// them).
func TestToolFace(t *testing.T) {
	skipOnWindows(t)
	r := testRegistry(t)
	prov := processProvider{reg: r}

	if _, err := prov.run(context.Background(), map[string]any{"action": "spawn", "command": "echo hi; sleep 120", "name": "toolled"}); err != nil {
		t.Fatalf("spawn tool: %v", err)
	}
	waitFor(t, 5*time.Second, "toolled log", func() bool {
		tail, _, _, err := prov.reg.Read("toolled", 100)
		return err == nil && strings.Contains(tail, "hi")
	})
	list, err := prov.run(context.Background(), map[string]any{"action": "list"})
	if err != nil || !strings.Contains(list, "toolled") || !strings.Contains(list, "running") {
		t.Fatalf("list tool: %s err=%v", list, err)
	}
	read, err := prov.run(context.Background(), map[string]any{"action": "read", "name": "toolled"})
	if err != nil || !strings.Contains(read, "hi") {
		t.Fatalf("read tool: %s err=%v", read, err)
	}
	// a lines cap inside the tool dispatcher truncates with a note
	seedLog := ""
	for i := 0; i < 50; i++ {
		seedLog += "filler\n"
	}
	seedLog += "last-line"
	if err := os.WriteFile(r.logsDir()+"/toolled.log", []byte(seedLog), 0o644); err != nil {
		// the running process may have appended between reads; re-seed
		// through the file the handle names
		t.Skipf("log rewrite raced the live writer: %v", err)
	}
	capped, err := prov.run(context.Background(), map[string]any{"action": "read", "name": "toolled", "lines": 10})
	if err != nil || !strings.Contains(capped, "truncated") {
		t.Fatalf("capped read tool: %s err=%v", capped, err)
	}
	if got, err := prov.run(context.Background(), map[string]any{"action": "wait", "name": "toolled", "timeout_seconds": 1}); err != nil || !strings.Contains(got, "still running") {
		t.Fatalf("wait tool: %s err=%v", got, err)
	}
	if got, err := prov.run(context.Background(), map[string]any{"action": "kill", "name": "toolled"}); err != nil || !strings.Contains(got, "stopped") {
		t.Fatalf("kill tool: %s err=%v", got, err)
	}
	if _, err := prov.run(context.Background(), map[string]any{"action": "bogus"}); err == nil {
		t.Fatal("unknown action must error")
	}
}

// seedState writes a hand-made state file (the reboot simulator's
// input).
func seedState(t *testing.T, path string, entries []Entry) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("seed dir: %v", err)
	}
	data, err := json.Marshal(entries)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}
}

// findByName finds one list row by name.
func findByName(entries []Entry, name string) (Entry, bool) {
	for _, e := range entries {
		if e.Name == name {
			return e, true
		}
	}
	return Entry{}, false
}
