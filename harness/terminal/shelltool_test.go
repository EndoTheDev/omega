package terminal

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/datadir"
)

// The shell tool's tests (the shell-ring shrink wave 1: shell.run
// moved here from the built-in tools capsule, behavior byte-exact).

// TestRunShellContextKill: a cancelled call returns PROMPTLY, but it
// does not kill the shell - detachment is the contract (card
// t_5a49790b): the call was cut short by a run timeout or a converted
// call's yield deadline, while the shell it started keeps running,
// readable and killable by the id the error names. Before the
// unification the cancelled ctx killed the in-process child.
func TestRunShellContextKill(t *testing.T) {
	p := testShellProvider(t)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	out, err := p.RunShell(ctx, map[string]any{"command": longSleeper()})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("cancelled ctx returned no error - the call reported a completed shell")
	}
	if elapsed > 5*time.Second {
		t.Fatalf("RunShell ignored ctx cancellation: blocked %v", elapsed)
	}
	id := shellIDFromText(t, err.Error())
	if !strings.Contains(err.Error(), "still running") || !strings.Contains(err.Error(), "shell.kill id="+id) {
		t.Fatalf("interrupted call must name the live shell and its kill path: %v (out %q)", err, out)
	}
	// The shell is genuinely still there: listed, readable, killable.
	list, err := p.List(context.Background(), map[string]any{})
	if err != nil || !strings.Contains(list, id) {
		t.Fatalf("the interrupted shell must stay listed: %q err=%v", list, err)
	}
	if got, err := p.Kill(context.Background(), map[string]any{"id": id}); err != nil || !strings.Contains(got, "stopped") {
		t.Fatalf("kill after an interrupted call: %q err=%v", got, err)
	}
}

// TestShellRunEscalationEligible (card t_1501cb04): shell.run carries
// NO static Yield flag - the fast path runs inline in the turn (zero
// overhead; previously EVERY shell.run paid the yield round trip)
// and only a call that runs past the loop's inline budget escalates
// to the wake path. The latency property is structural: inline IS the
// fast path now.
func TestShellRunEscalationEligible(t *testing.T) {
	// The tool SURFACE needs no registry: Tools() only builds the map,
	// and the registry is the verbs' business (a zero provider is a
	// valid source of the schema, never of an execution).
	tools := (&ShellProvider{}).Tools()
	if tools["shell.run"].Yield {
		t.Fatal("shell.run must not be immediate-yield - the static Yield class is superseded by budget escalation (t_1501cb04)")
	}
}

func TestShellRun(t *testing.T) {
	p := testShellProvider(t)
	ctx := context.Background()

	// Use a command that works on both Windows and Unix via the shell.
	out, err := p.RunShell(ctx, map[string]any{"command": "echo hello"})
	if err != nil {
		t.Fatalf("RunShell: %v", err)
	}
	if !strings.Contains(strings.TrimSpace(out), "hello") {
		t.Fatalf("shell output = %q", out)
	}
}

func TestShellEmptyCommand(t *testing.T) {
	p := testShellProvider(t)
	ctx := context.Background()

	_, err := p.RunShell(ctx, map[string]any{"command": "   "})
	if err == nil {
		t.Fatal("expected error for empty command, got nil")
	}
}

// TestRunShellDetachedGroup: the shell child is its own process group
// (the registry's detachment), so a cancelled call must not take the
// group down - the CI shape this test started life as (a forked child
// holding the output pipe blocked the run) is now impossible by
// construction: the call never waits on a pipe, it waits on the shell.
func TestRunShellDetachedGroup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the pgroup detachment is the posix shape; windows uses a job object")
	}
	p := testShellProvider(t)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	_, err := p.RunShell(ctx, map[string]any{"command": "sleep 30 & wait"})
	if err == nil {
		t.Fatal("cancelled ctx returned no error - command ran to completion")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("a cancelled call blocked %v", elapsed)
	}
	id := shellIDFromText(t, err.Error())
	if got, kerr := p.Kill(context.Background(), map[string]any{"id": id}); kerr != nil || !strings.Contains(got, "stopped") {
		t.Fatalf("the detached group must be killable by id: %q err=%v", got, kerr)
	}
}

// TestShellRoot pins the session-workspace rooting (card t_9de5b009)
// for the shell half (the file half is pinned in harness/files): the
// command's working directory IS the workspace.
func TestShellRoot(t *testing.T) {
	ws := t.TempDir()
	ctx := core.WithRunWorkspace(context.Background(), ws, "")
	p := testShellProvider(t)

	// the cwd-printing probe is OS-shaped: `pwd` is an external binary
	// everywhere, and on windows `cmd /C pwd` resolves whatever
	// pwd.exe sits on the inherited PATH (an MSYS coreutils install
	// answers with the /tmp-translated path, not the workspace).
	// `cd` bare is the cmd builtin that prints the cwd.
	probe := "pwd"
	if runtime.GOOS == "windows" {
		probe = "cd"
	}

	out, err := p.RunShell(ctx, map[string]any{"command": probe})
	if err != nil || !strings.Contains(out, ws) {
		t.Fatalf("shell %s: %v (got %q, want it to contain %q)", probe, err, out, ws)
	}
}

// TestShellDirOverride pins the per-call working-directory override
// (card t_ccf74d88): an absolute dir wins over the session workspace, a
// relative dir roots at the session workspace, and an impossible dir
// is a named tool error instead of a cryptic exec chdir failure.
func TestShellDirOverride(t *testing.T) {
	ws := t.TempDir()
	sub := filepath.Join(ws, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	ctx := core.WithRunWorkspace(context.Background(), ws, "")
	p := testShellProvider(t)

	probe := "pwd"
	if runtime.GOOS == "windows" {
		probe = "cd"
	}

	// Relative dir rides the session workspace root.
	out, err := p.RunShell(ctx, map[string]any{"command": probe, "dir": "sub"})
	if err != nil || !strings.Contains(out, sub) {
		t.Fatalf("relative dir: %v (got %q, want cwd %q)", err, out, sub)
	}

	// Absolute dir wins over the session workspace.
	out, err = p.RunShell(ctx, map[string]any{"command": probe, "dir": other})
	if err != nil || !strings.Contains(out, other) {
		t.Fatalf("absolute dir: %v (got %q, want cwd %q)", err, out, other)
	}

	// No dir argument: the session workspace stands (t_9de5b009).
	out, err = p.RunShell(ctx, map[string]any{"command": probe})
	if err != nil || !strings.Contains(out, filepath.Clean(ws)) {
		t.Fatalf("no dir: %v (got %q, want cwd %q)", err, out, ws)
	}

	// The impossible dir names itself.
	_, err = p.RunShell(ctx, map[string]any{"command": probe, "dir": "/omega-definitely-not-here"})
	if err == nil || !strings.Contains(err.Error(), "omega-definitely-not-here") {
		t.Fatalf("missing dir: err = %v, want a dir-named error", err)
	}
}

// TestGuardedShellRefuse: the shell tool refuses a db-facing command
// with the law named, and nothing executes.
func TestGuardedShellRefuse(t *testing.T) {
	dir := t.TempDir()
	db := dir + "/omega.db"
	if err := os.WriteFile(db, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := testShellProvider(t)
	p.guard = datadir.NewGuard(dir + "/omega.db")

	_, err := p.RunShell(context.Background(), map[string]any{"command": "sqlite3 " + db + " '.tables'"})
	if err == nil || !strings.Contains(err.Error(), datadir.DBDenyLaw) || !strings.Contains(err.Error(), "ENDO'S LAW") {
		t.Errorf("RunShell: err = %v, want loud refusal naming the law", err)
	}
}

// TestGuardedShellAllowNormal: a guard-carrying provider (and a
// nil-guard one) keeps normal shell operation untouched.
func TestGuardedShellAllowNormal(t *testing.T) {
	dir := t.TempDir()
	p := testShellProvider(t)
	p.guard = datadir.NewGuard(dir + "/omega.db")
	out, err := p.RunShell(context.Background(), map[string]any{"command": "echo omega rules"})
	if err != nil {
		t.Fatalf("RunShell normal command: %v", err)
	}
	if !strings.Contains(out, "omega rules") {
		t.Errorf("RunShell output = %q", out)
	}
	bare := testShellProvider(t)
	if out, err := bare.RunShell(context.Background(), map[string]any{"command": "echo bare"}); err != nil || !strings.Contains(out, "bare") {
		t.Errorf("nil-guard RunShell: %v (got %q)", err, out)
	}
}

// The serializer's regression pins (card t_82107785): the exact
// commands the dev-lane agents burned on, driven through shell.run -
// the door the agent-facing symptoms reported through. Before the
// fix, Go's argv escaping re-wrapped the embedded quotes into \"
// sequences the child parsed as literal quote characters: PowerShell
// echoed `-Command` scripts as string literals ($vars eaten with
// them) instead of running them.
func TestRunShellQuotedPowerShell(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the mangling class is a windows cmd /C contract")
	}
	p := testShellProvider(t)
	ctx := context.Background()
	cases := []struct{ command, want, never string }{
		{`powershell -NoProfile -Command "Write-Output hello-from-ps"`, "hello-from-ps", `"Write-Output`},
		{`powershell -NoProfile -Command "$x=5; Write-Output $x"`, "5", "=5;"},
	}
	for _, tc := range cases {
		out, err := p.RunShell(ctx, map[string]any{"command": tc.command})
		if err != nil {
			t.Fatalf("%s: %v", tc.command, err)
		}
		if !strings.Contains(out, tc.want) {
			t.Fatalf("%s: the script did not execute (want %q inside output, got %q)", tc.command, tc.want, out)
		}
		if strings.Contains(out, tc.never) {
			t.Fatalf("%s: the mangling class is back (the echoed literal leaked: %q)", tc.command, out)
		}
	}
}

// TestRunShellChainRedirectTiming pins the `>` chain behavior with its
// only honest witness: the wall clock (visible output is identical
// whether the chain ran or died - the report's own repro demand).
func TestRunShellChainRedirectTiming(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("ping -n timing is the windows delay idiom")
	}
	p := testShellProvider(t)
	start := time.Now()
	out, err := p.RunShell(context.Background(), map[string]any{"command": "echo chain-done & ping -n 2 127.0.0.1 >nul"})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("chain with redirect: %v\nout: %q", err, out)
	}
	if !strings.Contains(out, "chain-done") {
		t.Fatalf("chain segments must all run, got %q", out)
	}
	// ping -n 2 waits ~1s; a dead redirect segment would return early.
	if elapsed < 900*time.Millisecond {
		t.Fatalf("the delay segment did not actually run (elapsed %v)", elapsed)
	}
}

// TestShellSmallOutputNoMarker (card t_304afb4e): the truncation
// marker fires ONLY when bytes were actually dropped. The pre-fix
// cappedBuffer counted every written byte in over (over was
// total-written, not overflow), so an 8-byte echo read as a
// truncation and the reported total double-counted the kept bytes -
// the exact QA receipt ("[output truncated, 8 bytes total]" on a
// visible "A"). The condition over > 0 was honest; the accumulator
// lied.
func TestShellSmallOutputNoMarker(t *testing.T) {
	p := testShellProvider(t)
	out, err := p.RunShell(context.Background(), map[string]any{"command": "echo A"})
	if err != nil {
		t.Fatalf("RunShell: %v", err)
	}
	if strings.Contains(out, "truncated") {
		t.Fatalf("marker on a small output (the QA receipt is back): %q", out)
	}
	if !strings.Contains(strings.TrimSpace(out), "A") {
		t.Fatalf("shell output = %q, want the echo", out)
	}
}

// TestShellOversizeOutputHonestMarker (card t_304afb4e): past the 1 MB
// cap the marker is honest - kept-first size, real total, and the
// retrieval path named (shell.read tails the log, which survives the
// shell; the retired process.spawn spelling is gone, card t_5a49790b).
func TestShellOversizeOutputHonestMarker(t *testing.T) {
	big := filepath.Join(t.TempDir(), "big.txt")
	payload := bytes.Repeat([]byte("xxxxxxxxxx\n"), 160000) // 1,760,000 bytes
	if err := os.WriteFile(big, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	cat := "cat " + big
	if runtime.GOOS == "windows" {
		cat = "type " + big
	}
	p := testShellProvider(t)
	out, err := p.RunShell(context.Background(), map[string]any{"command": cat})
	if err != nil {
		t.Fatalf("RunShell: %v", err)
	}
	if !strings.HasPrefix(out, "xxxxxxxxxx") {
		t.Fatalf("the cap must keep the HEAD of the output: %q...", out[:40])
	}
	want := fmt.Sprintf("kept the first %d bytes of %d", shellOutputCap, len(payload))
	if !strings.Contains(out, want) {
		t.Fatalf("marker missing or dishonest (want %q): tail of out = %q", want, out[len(out)-120:])
	}
	// The retrieval path is the shell domain's own verb (card
	// t_5a49790b): shell.read tails the log the run already wrote, and
	// the log survives the shell - the retired process.* name is gone.
	if !strings.Contains(out, "shell.read id=sh-") || !strings.Contains(out, "survives the shell") {
		t.Fatalf("the marker must name shell.read and the retention contract: %q", out[len(out)-160:])
	}
	if strings.Contains(out, "process.spawn") {
		t.Fatalf("the marker still teaches the retired process.spawn path: %q", out[len(out)-160:])
	}
}

// -- the unification's helpers and round-trip pins -----------------------

// testShellProvider builds a provider over a private home: the registry
// state and logs land in the test's temp dir, and the registry's Close
// fence kills any child a test left running (a windows temp-dir teardown
// would otherwise race an open log handle).
func testShellProvider(t *testing.T) *ShellProvider {
	t.Helper()
	reg := NewRegistry(RegistryConfig{Home: t.TempDir(), KillGraceSeconds: 1})
	t.Cleanup(reg.Close)
	return &ShellProvider{reg: reg}
}

// TestShellProviderWithoutRegistryNamesItself pins the review-round-1
// fix (card t_5a49790b): there is NO lazy default registry. A zero
// provider used to build one whose config resolved datadir.Home(), so
// any test executing through it rewrote the installed instance's
// process.json and dropped logs in ~/.omega - two gate-run sites did
// exactly that. Every verb now answers the named wiring error instead,
// and nothing resolves a home.
func TestShellProviderWithoutRegistryNamesItself(t *testing.T) {
	p := &ShellProvider{}
	ctx := context.Background()
	cases := []struct {
		verb string
		call func() (string, error)
	}{
		{"shell.run", func() (string, error) {
			return p.RunShell(ctx, map[string]any{"command": "echo probe"})
		}},
		{"shell.list", func() (string, error) { return p.List(ctx, map[string]any{}) }},
		{"shell.read", func() (string, error) {
			return p.Read(ctx, map[string]any{"id": "sh-1"})
		}},
		{"shell.kill", func() (string, error) {
			return p.Kill(ctx, map[string]any{"id": "sh-1"})
		}},
	}
	for _, c := range cases {
		out, err := c.call()
		if !errors.Is(err, errNoRegistry) {
			t.Fatalf("%s without a registry: out=%q err=%v, want the named wiring error", c.verb, out, err)
		}
	}
}

// longSleeper is a command that reliably outlives a test's patience on
// both shells.
func longSleeper() string {
	if runtime.GOOS == "windows" {
		return "ping -n 300 127.0.0.1 > nul"
	}
	return "sleep 300"
}

// echoThenSleeper composes an echo and a sleeper in one command.
// The separator matters: cmd.exe has no `;`, and a redirect anywhere on
// the line binds to the whole command - `echo X; ping ... > nul` sends
// the ECHO to nul and the shell exits with an empty log. `&&` binds the
// redirect to the ping alone on windows; sh takes `;`.
func echoThenSleeper(word string) string {
	if runtime.GOOS == "windows" {
		return "echo " + word + " && " + longSleeper()
	}
	return "echo " + word + "; " + longSleeper()
}

// shellIDFromText pulls the shell id out of a "<...> id=<id>" phrase.
func shellIDFromText(t *testing.T, text string) string {
	t.Helper()
	i := strings.Index(text, "shell.read id=")
	if i < 0 {
		t.Fatalf("no shell id in %q", text)
	}
	rest := text[i+len("shell.read id="):]
	end := strings.IndexAny(rest, " ,;)]")
	if end < 0 {
		end = len(rest)
	}
	return rest[:end]
}

// TestShellVerbRoundTrip is the addressing surface's proof (card
// t_5a49790b): a running shell shows up in shell.list with id, command,
// age and log path; shell.read tails its log live; shell.kill stops it;
// and shell.read still answers afterwards (post-mortem retention, the
// t_304afb4e contract) while shell.list has dropped it.
func TestShellVerbRoundTrip(t *testing.T) {
	p := testShellProvider(t)
	ctx := context.Background()

	if _, err := p.reg.Spawn(ctx, echoThenSleeper("round-trip"), "rt", ""); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	// The echo may land a beat after Spawn returns.
	deadline := time.Now().Add(10 * time.Second)
	for {
		read, err := p.Read(ctx, map[string]any{"id": "rt"})
		if err == nil && strings.Contains(read, "round-trip") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("live read never saw the output: %q err=%v", read, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	list, err := p.List(ctx, map[string]any{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, want := range []string{"ID", "AGE", "COMMAND", "LOG", "rt", "sleep 300"} {
		if want == "sleep 300" && runtime.GOOS == "windows" {
			break // the windows sleeper is ping, not sleep
		}
		if !strings.Contains(list, want) {
			t.Fatalf("list missing %q:\n%s", want, list)
		}
	}
	if got, err := p.Kill(ctx, map[string]any{"id": "rt"}); err != nil || !strings.Contains(got, "stopped") {
		t.Fatalf("kill: %q err=%v", got, err)
	}
	// post-mortem: the log survives the shell, the list does not show it
	after, err := p.Read(ctx, map[string]any{"id": "rt"})
	if err != nil || !strings.Contains(after, "round-trip") {
		t.Fatalf("post-mortem read: %q err=%v", after, err)
	}
	list, err = p.List(ctx, map[string]any{})
	if err != nil || strings.Contains(list, "rt") {
		t.Fatalf("a killed shell must leave the list: %q err=%v", list, err)
	}
	// The addressing surface is ids-only: an unknown id is a named miss.
	if _, err := p.Read(ctx, map[string]any{"id": "no-such-shell"}); err == nil {
		t.Fatal("read of an unknown id must error")
	}
	if _, err := p.Kill(ctx, map[string]any{"id": "no-such-shell"}); err == nil {
		t.Fatal("kill of an unknown id must error")
	}
}

// TestShellRunReturnsOutputAndStatus: the inline path is unchanged in
// what the model sees - combined output and a failed exit as an error -
// while the output now also lands in the shell's log file.
func TestShellRunReturnsOutputAndStatus(t *testing.T) {
	p := testShellProvider(t)
	out, err := p.RunShell(context.Background(), map[string]any{"command": "echo on-stdout"})
	if err != nil || !strings.Contains(out, "on-stdout") {
		t.Fatalf("RunShell: %q err=%v", out, err)
	}
	fail := "exit 3"
	if runtime.GOOS == "windows" {
		fail = "exit /b 3"
	}
	_, err = p.RunShell(context.Background(), map[string]any{"command": fail})
	if err == nil || !strings.Contains(err.Error(), "exit status 3") {
		t.Fatalf("a failing command must report its status: %v", err)
	}
}
