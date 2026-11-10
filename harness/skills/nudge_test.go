package skills

import (
	"github.com/EndoTheDev/omega/agent"
	"testing"
)

// nudgeObs feeds n observations of one tool call named tool.
func nudgeObs(n *TurnNudger, tool string, count int) {
	for range count {
		n.ObserveToolRun(tool, map[string]any{}, nil)
	}
}

// nudgeWrites feeds n observations of an ordinary write-class tool
// call - work that changes something but persists no lesson.
func nudgeWrites(n *TurnNudger, count int) { nudgeObs(n, "files.edit", count) }

// nudgeReads feeds n observations of a read-class tool call and
// checks, on EVERY one, that the turn prep stays quiet - the shape
// of a probe pass, where each tool batch is followed by a turn prep.
func nudgeReads(t *testing.T, n *TurnNudger, count int) {
	t.Helper()
	for i := range count {
		n.ObserveToolRun("files.read", map[string]any{}, nil)
		if line := n.NudgeLine(); line != "" {
			t.Fatalf("nudge fired during a read-only stretch after %d reads: %q", i+1, line)
		}
	}
}

// TestNudgeFiresAtFifteen: the default cadence - the line first comes
// on the 15th write-class iteration after the last lesson, once, and
// consumes itself (the next one is a full interval away).
func TestNudgeFiresAtFifteen(t *testing.T) {
	n := NewTurnNudger(DefaultNudgeInterval)
	nudgeWrites(n, DefaultNudgeInterval-1)
	if line := n.NudgeLine(); line != "" {
		t.Fatalf("nudge fired early at %d iterations: %q", DefaultNudgeInterval-1, line)
	}
	nudgeWrites(n, 1)
	line := n.NudgeLine()
	if line == "" {
		t.Fatal("nudge did not fire at the configured interval")
	}
	if want := nudgeLinePrefix(); line == "" || len(line) < len(want) || line[:len(want)] != want {
		t.Fatalf("nudge line %q does not carry the reminder shape", line)
	}
	if again := n.NudgeLine(); again != "" {
		t.Fatalf("nudge did not consume itself: %q", again)
	}
}

// nudgeLinePrefix is the stable head of the reminder line.
func nudgeLinePrefix() string { return "Learning loop:" }

// TestNudgeSilentInReadOnlyPass: the QA finding A5 shape - a pass
// constrained to probing (reads, greps, shell commands, searches)
// receives ZERO nudges no matter how long it runs.
func TestNudgeSilentInReadOnlyPass(t *testing.T) {
	n := NewTurnNudger(DefaultNudgeInterval)
	// 6 intervals' worth of read-class calls of every probe-pass
	// flavor: none of them arms the nudge.
	tools := []string{"files.read", "files.grep", "files.glob", "shell.run", "search", "web.fetch", "skills.read", "delegate.task"}
	for range 12 {
		for _, tool := range tools {
			n.ObserveToolRun(tool, map[string]any{}, nil)
			if line := n.NudgeLine(); line != "" {
				t.Fatalf("nudge fired during a read-only pass (tool %s): %q", tool, line)
			}
		}
	}
}

// TestNudgeFiresAfterReadStretchEnds: the counter keeps running while
// the nudge is backed off, so the FIRST write-class call after a long
// read-only stretch makes the reminder due at the next turn prep.
func TestNudgeFiresAfterReadStretchEnds(t *testing.T) {
	n := NewTurnNudger(15)
	nudgeReads(t, n, 40)
	n.ObserveToolRun("files.write", map[string]any{"path": "x"}, nil)
	if line := n.NudgeLine(); line == "" {
		t.Fatal("first write-class call after a read stretch did not make the nudge due")
	}
	// Consumed: a following read-only stretch is quiet again.
	nudgeReads(t, n, 30)
}

// TestNudgeQuietAgainAfterWriteWindow: once a nudge fires, a stretch
// of plain reads does not produce another - the reminder is armed by
// writing, never by probing.
func TestNudgeQuietAgainAfterWriteWindow(t *testing.T) {
	n := NewTurnNudger(15)
	nudgeWrites(n, 15)
	if line := n.NudgeLine(); line == "" {
		t.Fatal("nudge did not fire on a write-class interval")
	}
	nudgeReads(t, n, 60)
}

// TestWriteClass: the arm classifier. Write verbs arm, read verbs and
// the tool names a probe pass actually uses (shell.run above all) do
// not; bridged MCP tool names parse through their separators.
func TestWriteClass(t *testing.T) {
	write := []string{
		"files.write", "files.edit", "files.batch", "skills.write",
		"skills.update", "skills.curate", "memory.add", "memory.remove",
		"kanban.create", "kanban.comment", "kanban.delete",
		"mcp__fs__write_file", "mcp__github__create_issue",
	}
	for _, name := range write {
		if !isWriteClass(name) {
			t.Errorf("isWriteClass(%q) = false, want true", name)
		}
	}
	read := []string{
		"files.read", "files.grep", "files.glob", "skills.read",
		"shell.run", "search", "web.fetch", "delegate.task",
		"delegate.status", "shell.list", "shell.read", "shell.kill", "browser.goto",
		"", "terminal",
	}
	for _, name := range read {
		if isWriteClass(name) {
			t.Errorf("isWriteClass(%q) = true, want false", name)
		}
	}
}

// TestNudgeResetsOnSkillsWrite: a skills write or update is the
// persisted lesson - the counter resets and the next nudge is a full
// interval later. A failed skills write still counts as the lesson
// attempt (the reset rides the call, not its success).
func TestNudgeResetsOnSkillsWrite(t *testing.T) {
	n := NewTurnNudger(15)
	nudgeWrites(n, 14)
	n.ObserveToolRun("skills.write", map[string]any{"name": "s"}, nil)
	nudgeWrites(n, 14)
	if line := n.NudgeLine(); line != "" {
		t.Fatalf("nudge survived the skills write: %q", line)
	}
	// 15 more write-class iterations - fires.
	nudgeWrites(n, 15)
	if line := n.NudgeLine(); line == "" {
		t.Fatal("nudge did not fire a full interval after the write")
	}
	// An update resets too, and so does a failed write. Reads do not.
	n.ObserveToolRun("files.edit", map[string]any{}, nil)
	n.ObserveToolRun("skills.update", map[string]any{"name": "s"}, nil)
	n.ObserveToolRun("skills.write", map[string]any{"name": "s"}, errFake{})
	nudgeWrites(n, 14)
	if line := n.NudgeLine(); line != "" {
		t.Fatalf("update/failed-write did not reset the counter: %q", line)
	}
}

type errFake struct{}

func (errFake) Error() string { return "no" }

// TestNudgeOffAtZero: interval 0 is the off switch - observation
// acccounts nothing, the line never comes.
func TestNudgeOffAtZero(t *testing.T) {
	n := NewTurnNudger(0)
	nudgeWrites(n, 500)
	if line := n.NudgeLine(); line != "" {
		t.Fatalf("interval 0 nudged: %q", line)
	}
}

// TestNudgeConcurrentObservation: the loop executes tool calls in
// goroutines - the counters must survive a fan without tearing
// (run with -race in CI).
func TestNudgeConcurrentObservation(t *testing.T) {
	n := NewTurnNudger(100)
	done := make(chan struct{})
	for range 4 {
		go func() {
			defer func() { done <- struct{}{} }()
			for range 50 {
				n.ObserveToolRun("files.edit", map[string]any{}, nil)
			}
		}()
	}
	for range 4 {
		<-done
	}
	if line := n.NudgeLine(); line == "" {
		t.Fatal("200 concurrent observations did not reach the interval")
	}
}

// TestTurnNudgerMeetsSeam: the compile-time shape the loop consumes.
var _ agent.TurnNudger = (*TurnNudger)(nil)
