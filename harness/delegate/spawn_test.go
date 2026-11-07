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

// TestRunDelegateTaskExplicitBinary verifies the full spawn path: the
// task's tool RESULT is the child's own final answer (async agent loop
// phase 2, card t_0805d438) - the call returns what omega run printed,
// and the user-shaped inject is gone. The subagent=1 assertion pins the
// recursion guard: the host must set OMEGA_SUBAGENT=1 on the child env.
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
	// Normalize CRLF stub output to LF for assertion.
	text := strings.ReplaceAll(out, "\r\n", "\n")
	if got := strings.TrimSpace(text); got != "stub-result\nsubagent=1" {
		t.Fatalf("tool result = %q, want the child's final answer", got)
	}

	// A done task: the status surface marks it done.
	status, err := d.runDelegateStatus(context.Background(), nil)
	if err != nil {
		t.Fatalf("runDelegateStatus: %v", err)
	}
	if !strings.Contains(status, "Running: 0, Done: 1") {
		t.Fatalf("expected done counters, got %q", status)
	}
	if !strings.Contains(status, "[done]") {
		t.Fatalf("expected done status, got %q", status)
	}
	if !strings.Contains(status, "answer: stub-result") {
		t.Fatalf("done task must expose the final answer text, got %q", status)
	}
}

// DELEGATE-STATUS SHAPE (scope addition A4, card t_0805d438): every row
// carries id, status, the session reference and a prompt DIGEST - never
// the full prompt - and a done row re-enters the task's final answer as
// the queryable home.
func TestDelegateStatusPayloadShape(t *testing.T) {
	longPrompt := strings.Repeat("the very long prompt the model sent ", 20)
	running := &delegateTask{id: "task-1", prompt: longPrompt, session: "sess-running"}
	done := &delegateTask{id: "task-2", prompt: "short prompt", session: "sess-done", done: true,
		result: "the final answer text"}
	d := NewDelegate()
	d.tasks["task-1"] = running
	d.tasks["task-2"] = done
	status, err := d.runDelegateStatus(context.Background(), nil)
	if err != nil {
		t.Fatalf("runDelegateStatus: %v", err)
	}
	if !strings.Contains(status, "Running: 1, Done: 1") {
		t.Fatalf("counters = %q", status)
	}
	if !strings.Contains(status, "- task-1 [running] (session sess-running) ") {
		t.Fatalf("running row lost its identity triple: %q", status)
	}
	// The digest clip: the running row ends in the named cut, and the
	// 120-rune digest never carries the full prompt.
	rowStart := strings.Index(status, "- task-1 [running]")
	rowEnd := strings.Index(status[rowStart:], "\n")
	row := status[rowStart : rowStart+rowEnd]
	if !strings.HasSuffix(row, "...") {
		t.Fatalf("digest row never got clipped: %q", row)
	}
	digest := row[strings.LastIndex(row, ")")+2:]
	if len(digest) > statusSummaryClip+3 {
		t.Fatalf("digest row %d chars, want <= %d: %q", len(digest), statusSummaryClip+3, row)
	}
	if !strings.Contains(status, "- task-2 [done] (session sess-done) short prompt\n") {
		t.Fatalf("done row shape: %q", status)
	}
	if !strings.Contains(status, "  answer: the final answer text\n") {
		t.Fatalf("done row lost its answer line: %q", status)
	}
}

// TestRunDelegateTaskFailingBinary verifies the failure shape: a child
// that exits non-zero still delivers its tool result, but a SHORT
// failure line only - the child's output stays in its session and the
// task record, never spammed into the caller's context.
func TestRunDelegateTaskFailingBinary(t *testing.T) {
	bin := writeStub(t, "stub-fail", "boom", 3)
	t.Setenv("OMEGA_BIN", bin)

	d := NewDelegate()
	out, err := d.runDelegateTask(context.Background(), map[string]any{"prompt": "fail please"})
	if err != nil {
		t.Fatalf("runDelegateTask: %v", err)
	}
	wanted := "error: delegate task task-1 failed: exit status 3"
	if strings.ReplaceAll(out, "\r\n", "\n") != wanted {
		t.Fatalf("expected short failure line %q, got %q", wanted, out)
	}
	if strings.Contains(out, "boom") {
		t.Fatalf("failure line leaked child output: %q", out)
	}
	// The status surface carries the failure report for a done task.
	status, _ := d.runDelegateStatus(context.Background(), nil)
	if !strings.Contains(status, "answer: error: delegate task task-1 failed") {
		t.Fatalf("failed task's answer line wrong: %q", status)
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
