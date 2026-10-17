package delegate

import (
	"context"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
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
	select {
	case msg := <-d.InjectedChannel():
		// Normalize CRLF stub output to LF for assertion.
		msg.text = strings.ReplaceAll(msg.text, "\r\n", "\n")
		msg.text = strings.TrimSpace(msg.text)
		if msg.text != "stub-result\nsubagent=1" {
			t.Fatalf("expected stub result with subagent=1 guard, got %q", msg.text)
		}
		if msg.source != "delegate:task-1" {
			t.Fatalf("expected source delegate:task-1, got %q", msg.source)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for injected result")
	}

	// Task must now be marked done.
	if d.PendingCount() != 0 {
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

// TestRunDelegateTaskFailingBinary verifies a child process that exits
// non-zero still delivers an injected result, with the error captured.
func TestRunDelegateTaskFailingBinary(t *testing.T) {
	bin := writeStub(t, "stub-fail", "boom", 3)
	t.Setenv("OMEGA_BIN", bin)

	d := NewDelegate()
	if _, err := d.runDelegateTask(context.Background(), map[string]any{"prompt": "fail please"}); err != nil {
		t.Fatalf("runDelegateTask: %v", err)
	}

	select {
	case msg := <-d.InjectedChannel():
		if !strings.Contains(msg.text, "boom") {
			t.Fatalf("expected child output in result, got %q", msg.text)
		}
		if !strings.Contains(msg.text, "error:") {
			t.Fatalf("expected error prefix in result, got %q", msg.text)
		}
		if !strings.Contains(msg.text, "subagent=1") {
			t.Fatalf("expected subagent guard in child env, got %q", msg.text)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for injected failure result")
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
