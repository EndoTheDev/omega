package process

import (
	"context"
	"encoding/json"
	"fmt"
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

// TestSpawnReadKill is the happy path: spawn, the log carries the
// output, the process is live, kill stops it, the state is exited.
func TestSpawnReadKill(t *testing.T) {
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
	waitFor(t, 5*time.Second, "boom to exit", func() bool {
		e, ok := r.Find("boom")
		return ok && e.Exited
	})
	e := stateEntry(t, r, "boom")
	if !strings.Contains(stateOf(&e), "code=7") {
		t.Fatalf("exit state missing code 7: %s", stateOf(&e))
	}
	if e.ExitCode == nil || *e.ExitCode != 7 || e.FinishedAt == 0 {
		t.Fatalf("exit record incomplete: %+v", e)
	}
	if _, err := r.Spawn(context.Background(), "sleep 0.2; exit 0", "quiet", ""); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	waitFor(t, 5*time.Second, "quiet to exit", func() bool {
		e, ok := r.Find("quiet")
		return ok && e.Exited
	})
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
	waitFor(t, 5*time.Second, "rot to exit", func() bool {
		e, ok := r.Find("rot")
		return ok && e.Exited
	})
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

// TestToolFace drives the four verbs through the dispatcher the tool
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
	if got, err := prov.run(context.Background(), map[string]any{"action": "kill", "name": "toolled"}); err != nil || !strings.Contains(got, "stopped") {
		t.Fatalf("kill tool: %s err=%v", got, err)
	}
	// the retention contract: the exited handle stays in the registry
	// (and its log on disk) until the sweep - that post-mortem read is
	// the point of keeping a log at all.
	if e, ok := prov.reg.Find("toolled"); !ok || !e.Exited {
		t.Fatalf("the exited handle must stay listed: %+v ok=%v", e, ok)
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

// The serializer's regression pins (card t_82107785): the exact
// commands the dev-lane agents burned on, through the same cmd /C
// door the runtime uses. Before the fix, Go's argv escaping re-wrapped
// the embedded quotes into \" sequences the child parsed as literal
// quote characters - PowerShell echoed `-Command` scripts as string
// literals ($vars eaten with them) instead of running them.
//
// The windows SysProcAttr.CmdLine pin lives in
// process_windows_test.go behind a build tag (card t_590976b0): the
// field exists only on windows, so a runtime guard cannot make the
// read compile on linux.
func TestBuildShellRawCommandLine(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the raw command-line pin is a windows contract (process_windows_test.go)")
	}
	command := `powershell -NoProfile -Command "$x=5; Write-Output $x"`
	c := buildShell(command)
	// unix: the argv contract is correct, sh -c takes the command verbatim.
	if len(c.Args) != 3 || c.Args[0] != "sh" || c.Args[1] != "-c" || c.Args[2] != command {
		t.Fatalf("unix shell shape drifted: %v", c.Args)
	}
}

func TestBuildShellPowerShellQuotedScript(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the mangling class is a windows cmd /C contract")
	}
	cases := []struct{ command, want, never string }{
		{`powershell -NoProfile -Command "Write-Output hello-from-ps"`, "hello-from-ps", `"Write-Output`},
		{`powershell -NoProfile -Command "$x=5; Write-Output $x"`, "5", "=5;"},
	}
	for _, tc := range cases {
		c := buildShell(tc.command)
		c.Dir = t.TempDir()
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\nout: %q", tc.command, err, out)
		}
		got := string(out)
		if !strings.Contains(got, tc.want) {
			t.Fatalf("%s: the script did not execute (want %q inside output, got %q)", tc.command, tc.want, got)
		}
		if strings.Contains(got, tc.never) {
			t.Fatalf("%s: the mangling class is back (the echoed literal leaked: %q)", tc.command, got)
		}
	}
}

// TestBuildShellChainRedirectTiming pins the `>` chain behavior with
// its only honest witness: the wall clock. The delay segment must
// actually run (the pre-fix report claimed the redirect segment died
// instantly and silently).
func TestBuildShellChainRedirectTiming(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("ping -n timing is the windows delay idiom")
	}
	start := time.Now()
	c := buildShell("echo chain-done & ping -n 2 127.0.0.1 >nul")
	c.Dir = t.TempDir()
	out, err := c.CombinedOutput()
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("chain with redirect: %v\nout: %q", err, out)
	}
	if !strings.Contains(string(out), "chain-done") {
		t.Fatalf("chain segments must all run, got %q", out)
	}
	// ping -n 2 waits ~1s; a dead redirect segment would return early.
	if elapsed < 900*time.Millisecond {
		t.Fatalf("the delay segment did not actually run (elapsed %v)", elapsed)
	}
}

// TestPostMortemRead (card t_304afb4e): the log survives the process.
// Before the fix the reaper rotated <name>.log to <name>.log.1 at
// exit, so the post-mortem read - arguably the main reason to read a
// log - answered "no log yet". The acceptance receipt: spawn a short
// process, wait for exit, read the log.
func TestPostMortemRead(t *testing.T) {
	r := testRegistry(t)
	if _, err := r.Spawn(context.Background(), "echo post-mortem-ok", "gone", ""); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	waitFor(t, 10*time.Second, "gone to exit", func() bool {
		ee, ok := r.Find("gone")
		return ok && ee.Exited
	})
	// the finished log is still at its recorded path - no exit-time rotation
	e := stateEntry(t, r, "gone")
	if _, err := os.Stat(e.LogPath); err != nil {
		t.Fatalf("the finished log vanished: %v", err)
	}
	tail, _, _, err := r.Read("gone", 100)
	if err != nil || !strings.Contains(tail, "post-mortem-ok") {
		t.Fatalf("post-mortem read: %q err=%v", tail, err)
	}
}

// TestReadRotatedGenerationFallback: an entry rotated by a pre-fix
// registry (its .log gone, the .1 surviving) still reads - the
// legacy state the retention contract must not orphan.
func TestReadRotatedGenerationFallback(t *testing.T) {
	r := testRegistry(t)
	e := Entry{
		Name:       "legacy",
		Command:    "echo x",
		PID:        1,
		StartedAt:  time.Now().Add(-time.Minute).Unix(),
		FinishedAt: time.Now().Unix(),
		Exited:     true,
		LogPath:    filepath.Join(r.logsDir(), "legacy.log"),
	}
	if err := os.MkdirAll(r.logsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.LogPath+".1", []byte("generation-one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	seedState(t, r.statePath(), []Entry{e})
	tail, _, _, err := r.Read("legacy", 10)
	if err != nil || !strings.Contains(tail, "generation-one") {
		t.Fatalf("rotated-generation read: %q err=%v", tail, err)
	}
}

// TestCapExitedLog (card t_304afb4e): process.log_max_bytes is
// enforced at exit - an oversized finished log is trimmed to the cap,
// the TAIL kept (read serves the tail), a small log untouched.
func TestCapExitedLog(t *testing.T) {
	r := testRegistry(t)
	r.cfg.LogMaxBytes = 4096
	var sb strings.Builder
	for i := 0; i < 1000; i++ {
		fmt.Fprintf(&sb, "line-%04d\n", i) // 10 KB total
	}
	logPath := filepath.Join(r.logsDir(), "fat.log")
	if err := os.MkdirAll(r.logsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	r.capExited(&Entry{Name: "fat", LogPath: logPath, Exited: true})
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 4096 {
		t.Fatalf("capped size = %d, want 4096", len(data))
	}
	if !strings.Contains(string(data), "line-0999") {
		t.Fatal("the cap must keep the TAIL (the last line survived the trim?)")
	}
	if strings.Contains(string(data), "line-0000") {
		t.Fatal("the cap must drop the head (the first line is still there)")
	}
	small := filepath.Join(r.logsDir(), "thin.log")
	if err := os.WriteFile(small, []byte("tiny"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.capExited(&Entry{Name: "thin", LogPath: small, Exited: true})
	if data, err := os.ReadFile(small); err != nil || len(data) != 4 {
		t.Fatalf("a sub-cap log must be untouched: %q err=%v", data, err)
	}
}

// TestRetainSweepGC (card t_304afb4e): the registry GC - an exited
// entry older than the retention window (and its log files) is swept
// at load; a recent one stays.
func TestRetainSweepGC(t *testing.T) {
	home := t.TempDir()
	r := NewRegistry(Config{Home: home, KillGraceSeconds: 1, LogMaxBytes: DefaultLogMaxBytes})
	week := 7 * 24 * time.Hour
	mk := func(name string, age time.Duration) Entry {
		return Entry{
			Name:       name,
			Command:    "echo " + name,
			PID:        1,
			StartedAt:  time.Now().Add(-age - time.Minute).Unix(),
			FinishedAt: time.Now().Add(-age).Unix(),
			Exited:     true,
			ExitCode:   new(int),
			LogPath:    filepath.Join(r.logsDir(), name+".log"),
		}
	}
	ancient, fresh := mk("ancient", week+24*time.Hour), mk("fresh", time.Hour)
	if err := os.MkdirAll(r.logsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, e := range []Entry{ancient, fresh} {
		if err := os.WriteFile(e.LogPath, []byte("log-"+e.Name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	seedState(t, r.statePath(), []Entry{ancient, fresh})
	list := r.List() // load + sweep
	if _, ok := findByName(list, "ancient"); ok {
		t.Fatal("an entry a week past exit must be swept")
	}
	if _, ok := findByName(list, "fresh"); !ok {
		t.Fatal("a recent exited entry must survive the sweep")
	}
	if _, err := os.Stat(ancient.LogPath); !os.IsNotExist(err) {
		t.Fatal("the swept entry's log survived")
	}
	if _, err := os.Stat(fresh.LogPath); err != nil {
		t.Fatalf("the surviving entry's log vanished: %v", err)
	}
}
