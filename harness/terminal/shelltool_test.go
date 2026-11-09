package terminal

import (
	"context"
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

// TestRunShellContextKill: the shell tool must run under the call ctx -
// a cancelled ctx kills the process so a hung command cannot outrun the
// detached run's wall-clock cap.
func TestRunShellContextKill(t *testing.T) {
	p := &ShellProvider{}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	_, err := p.RunShell(ctx, map[string]any{"command": "sleep 30"})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("cancelled ctx returned no error - command ran to completion")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("RunShell ignored ctx cancellation: blocked %v", elapsed)
	}
}

// TestShellRunEscalationEligible (card t_1501cb04): shell.run carries
// NO static Yield flag - the fast path runs inline in the turn (zero
// overhead; previously EVERY shell.run paid the yield round trip)
// and only a call that runs past the loop's inline budget escalates
// to the wake path. The latency property is structural: inline IS the
// fast path now.
func TestShellRunEscalationEligible(t *testing.T) {
	tools := (&ShellProvider{}).Tools()
	if tools["shell.run"].Yield {
		t.Fatal("shell.run must not be immediate-yield - the static Yield class is superseded by budget escalation (t_1501cb04)")
	}
}

func TestShellRun(t *testing.T) {
	p := &ShellProvider{}
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
	p := &ShellProvider{}
	ctx := context.Background()

	_, err := p.RunShell(ctx, map[string]any{"command": "   "})
	if err == nil {
		t.Fatal("expected error for empty command, got nil")
	}
}

// The CI failure shape generalized: a FORKED child (not exec-optimized)
// holds the output pipe. Killing only the direct child leaves the run
// blocked until the grandchild exits - the group kill is what ends it.
func TestRunShellContextKillsGroup(t *testing.T) {
	if runtime.GOOS == "windows" {
		// the group kill is the pgroup capability (posix shellKillSetup):
		// windows exec carries no process-group kill, so a grandchild
		// that inherits the output pipe (sleep from an msys PATH) keeps
		// the run blocked after cmd dies. Pinning a capability the
		// platform cannot have is the test's lie, not the product's;
		// a windows Job Object answer is its own card.
		t.Skip("group kill is the unix pgroup shape; windows exec has no process-group kill")
	}
	p := &ShellProvider{}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	_, err := p.RunShell(ctx, map[string]any{"command": "sleep 30 & wait"})
	if err == nil {
		t.Fatal("cancelled ctx returned no error - command ran to completion")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("group kill missed: blocked %v", elapsed)
	}
}

// TestShellRoot pins the session-workspace rooting (card t_9de5b009)
// for the shell half (the file half is pinned in harness/files): the
// command's working directory IS the workspace.
func TestShellRoot(t *testing.T) {
	ws := t.TempDir()
	ctx := core.WithRunWorkspace(context.Background(), ws, "")
	p := &ShellProvider{}

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
	p := &ShellProvider{}

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
	p := &ShellProvider{guard: datadir.NewGuard(dir + "/omega.db")}

	_, err := p.RunShell(context.Background(), map[string]any{"command": "sqlite3 " + db + " '.tables'"})
	if err == nil || !strings.Contains(err.Error(), datadir.DBDenyLaw) || !strings.Contains(err.Error(), "ENDO'S LAW") {
		t.Errorf("RunShell: err = %v, want loud refusal naming the law", err)
	}
}

// TestGuardedShellAllowNormal: a guard-carrying provider (and a
// nil-guard one) keeps normal shell operation untouched.
func TestGuardedShellAllowNormal(t *testing.T) {
	dir := t.TempDir()
	p := &ShellProvider{guard: datadir.NewGuard(dir + "/omega.db")}
	out, err := p.RunShell(context.Background(), map[string]any{"command": "echo omega rules"})
	if err != nil {
		t.Fatalf("RunShell normal command: %v", err)
	}
	if !strings.Contains(out, "omega rules") {
		t.Errorf("RunShell output = %q", out)
	}
	bare := &ShellProvider{}
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
	p := &ShellProvider{}
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
	p := &ShellProvider{}
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
