package delegate

import (
	"context"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// writeStub writes a platform-appropriate stub script that echoes the
// given line, echoes the OMEGA_SUBAGENT value it observed, and exits
// with the given code. The subagent line lets tests assert the host
// actually set the recursion guard env var on the child process.
func writeStub(t *testing.T, name, out string, code int) string {
	t.Helper()
	bin := t.TempDir() + "/" + name
	var body string
	if runtime.GOOS == "windows" {
		bin += ".bat"
		body = "@echo off\r\necho " + out + "\r\necho subagent=%OMEGA_SUBAGENT%\r\nexit /b " + strconv.Itoa(code) + "\r\n"
	} else {
		bin += ".sh"
		body = "#!/bin/sh\necho " + out + "\necho \"subagent=$OMEGA_SUBAGENT\"\nexit " + strconv.Itoa(code) + "\n"
	}
	if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

// TestRunDelegateTaskMissingBinary verifies runDelegateTask fails cleanly
// when no omega binary can be found: OMEGA_BIN unset and no ../omega or
// ../omega.exe next to the test executable.
func TestRunDelegateTaskMissingBinary(t *testing.T) {
	t.Setenv("OMEGA_BIN", "")
	d := NewDelegate()
	out, err := d.runDelegateTask(context.Background(), map[string]any{"prompt": "do a thing"})
	if err == nil {
		t.Fatalf("expected error when omega binary missing, got output %q", out)
	}
	if !strings.Contains(out, "error: could not find omega binary") {
		t.Fatalf("expected binary-not-found message, got %q", out)
	}
}

// TestRunDelegateTaskExplicitBinary verifies the full spawn path with
// OMEGA_BIN pointing at a stub that echoes output and exits 0. The
// subagent=1 assertion pins the recursion guard: the host must set
// OMEGA_SUBAGENT=1 on the child environment.
func TestRunDelegateTaskExplicitBinary(t *testing.T) {
	bin := writeStub(t, "stub-omega", "stub-result", 0)
	t.Setenv("OMEGA_BIN", bin)

	d := NewDelegate()
	out, err := d.runDelegateTask(context.Background(), map[string]any{
		"prompt":  "summarize the logs",
		"timeout": 30,
	})
	if err != nil {
		t.Fatalf("runDelegateTask: %v", err)
	}
	if !strings.Contains(out, "Subagent task-1 started") {
		t.Fatalf("expected task start confirmation, got %q", out)
	}

	// Wait for the goroutine to finish and inject the result.
	msg, ok := d.Wait(ctx15(t), "")
	if !ok {
		t.Fatal("timed out waiting for injected result")
	}
	// Normalize CRLF stub output to LF for assertion.
	msgText := strings.ReplaceAll(msg.Text, "\r\n", "\n")
	msgText = strings.TrimSpace(msgText)
	if msgText != "stub-result\nsubagent=1" {
		t.Fatalf("expected stub result with subagent=1 guard, got %q", msgText)
	}
	if msg.Source != "delegate:task-1" {
		t.Fatalf("expected source delegate:task-1, got %q", msg.Source)
	}

	// Task must now be marked done.
	if d.PendingCount("") != 0 {
		t.Fatal("expected 0 pending after completion")
	}
	status, err := d.runDelegateStatus(context.Background(), nil)
	if err != nil {
		t.Fatalf("runDelegateStatus: %v", err)
	}
	if !strings.Contains(status, "[done]") {
		t.Fatalf("expected done status, got %q", status)
	}
}

// TestRunDelegateTaskFailingBinary verifies the ruling's failure
// shape: a child that exits non-zero still delivers an injected turn,
// but a SHORT failure line only - the child's output stays in its
// session and the task record, never spammed into the caller's
// context.
func TestRunDelegateTaskFailingBinary(t *testing.T) {
	bin := writeStub(t, "stub-fail", "boom", 3)
	t.Setenv("OMEGA_BIN", bin)

	d := NewDelegate()
	if _, err := d.runDelegateTask(context.Background(), map[string]any{"prompt": "fail please"}); err != nil {
		t.Fatalf("runDelegateTask: %v", err)
	}

	msg, ok := d.Wait(ctx15(t), "")
	if !ok {
		t.Fatal("timed out waiting for injected failure result")
	}
	wanted := "error: delegate task task-1 failed: exit status 3"
	if msg.Text != wanted {
		t.Fatalf("expected short failure line %q, got %q", wanted, msg.Text)
	}
	if msg.Source != "delegate:task-1" {
		t.Fatalf("expected source delegate:task-1, got %q", msg.Source)
	}
	if strings.Contains(msg.Text, "boom") {
		t.Fatalf("failure line leaked child output: %q", msg.Text)
	}
}

// TestFindOmegaBinary verifies binary resolution: env override wins,
// missing env falls through to the executable-relative search which
// finds nothing in a test environment.
func TestFindOmegaBinary(t *testing.T) {
	t.Setenv("OMEGA_BIN", "")
	d := NewDelegate()
	if got := d.findOmegaBinary(); got != "" {
		t.Fatalf("expected empty binary path in test env, got %q", got)
	}

	bin := writeStub(t, "real-omega", "x", 0)
	t.Setenv("OMEGA_BIN", bin)
	if got := d.findOmegaBinary(); got != bin {
		t.Fatalf("expected env override %q, got %q", bin, got)
	}
}
