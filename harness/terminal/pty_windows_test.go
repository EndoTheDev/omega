//go:build windows

// Package terminal - the ConPTY door's self-check, the windows
// counterpart of TestRealPTYShellEcho (pty_linux.go's /dev/ptmx guard
// skips it here): a real shell on a real pseudoconsole must echo,
// resize, exit with a real exit code, and die on Kill.
package terminal

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestConPTYShellEcho pins the loop: spawn -> resize -> prompt ->
// echo. The cmd prompt carries the cwd, so the temp dir's base name
// is the arrival needle for "the console is live and at the prompt"
// - and the resize landed first, since the prompt only renders at
// the sized buffer. cmd reads \r input, never \n.
func TestConPTYShellEcho(t *testing.T) {
	dir := t.TempDir()
	proc, err := spawnProcess(dir)
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	defer proc.Kill()
	if err := proc.Resize(30, 100); err != nil {
		t.Fatalf("resize: %v", err)
	}
	if err := drainUntil(t, proc, filepath.Base(dir), testWaitTimeout); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if _, err := proc.Write([]byte("echo omega-term-proof\r")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := drainUntil(t, proc, "omega-term-proof", testWaitTimeout); err != nil {
		t.Fatalf("echo: %v", err)
	}
}

// TestConPTYExitCode pins the exit watcher: an explicit shell exit
// yields Done + a zero code (cmd.exe's own status), the same contract
// the linux ptyProcess carries.
func TestConPTYExitCode(t *testing.T) {
	dir := t.TempDir()
	proc, err := spawnProcess(dir)
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	defer proc.Kill()
	if _, err := proc.Write([]byte("exit\r")); err != nil {
		t.Fatalf("write: %v", err)
	}
	select {
	case <-proc.Done():
	case <-time.After(testWaitTimeout):
		t.Fatal("shell never exited after its own exit command")
	}
	if code := proc.ExitCode(); code != 0 {
		t.Fatalf("exit code: %d, want 0", code)
	}
}

// TestConPTYKill pins the kill door: TerminateProcess tears the shell
// down, the done fence closes, and the output pipe hangs up (the read
// loop's exit shape).
func TestConPTYKill(t *testing.T) {
	dir := t.TempDir()
	proc, err := spawnProcess(dir)
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if err := proc.Kill(); err != nil {
		t.Fatalf("kill: %v", err)
	}
	select {
	case <-proc.Done():
	case <-time.After(testWaitTimeout):
		t.Fatal("done fence never closed after Kill")
	}
	// the stream is done: a blocking read returns (hangup error or
	// quiet EOF-adjacent refusal), never parks forever
	late := make(chan error, 1)
	go func() {
		buf := make([]byte, 16)
		_, err := proc.Read(buf)
		late <- err
	}()
	select {
	case <-late: // any outcome: hangup is the contract, silence is not
	case <-time.After(testWaitTimeout):
		t.Fatal("read still parked after Kill")
	}
	if err := proc.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// TestShellPathWindowsComSpec pins the pick law: ComSpec wins (the
// bulletproof ConPTY client), the stat-checked fallbacks only carry
// exotic boxes.
func TestShellPathWindowsComSpec(t *testing.T) {
	t.Setenv("COMSPEC", `C:\Windows\System32\cmd.exe`)
	if got := shellPath(); got != `C:\Windows\System32\cmd.exe` {
		t.Fatalf("shellPath: %q, want the ComSpec door", got)
	}
	t.Setenv("COMSPEC", "")
	got := shellPath()
	if !strings.Contains(strings.ToLower(got), "cmd.exe") && !strings.Contains(strings.ToLower(got), "powershell") {
		t.Fatalf("shellPath fallback: %q, want cmd.exe or powershell.exe", got)
	}
}
