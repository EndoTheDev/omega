package terminal

import (
	"context"
	"os"
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

	out, err := p.RunShell(ctx, map[string]any{"command": "pwd"})
	if err != nil || !strings.Contains(out, ws) {
		t.Fatalf("shell pwd: %v (got %q, want it to contain %q)", err, out, ws)
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