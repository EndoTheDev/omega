package skills

import (
	"github.com/EndoTheDev/omega/agent"
	"testing"

)

// nudgeObs feeds n observations of one ordinary (non-skills) tool call.
func nudgeObs(n *TurnNudger, count int) {
	for range count {
		n.ObserveToolRun("terminal", map[string]any{}, nil)
	}
}

// TestNudgeFiresAtFifteen: the default cadence - the line first comes
// on the 15th iteration after the last lesson, once, and consumes
// itself (the next one is a full interval away).
func TestNudgeFiresAtFifteen(t *testing.T) {
	n := NewTurnNudger(DefaultNudgeInterval)
	nudgeObs(n, DefaultNudgeInterval-1)
	if line := n.NudgeLine(); line != "" {
		t.Fatalf("nudge fired early at %d iterations: %q", DefaultNudgeInterval-1, line)
	}
	nudgeObs(n, 1)
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

// TestNudgeResetsOnSkillsWrite: a skills write or update is the
// persisted lesson - the counter resets and the next nudge is a full
// interval later. A failed skills write still counts as the lesson
// attempt (the reset rides the call, not its success).
func TestNudgeResetsOnSkillsWrite(t *testing.T) {
	n := NewTurnNudger(15)
	nudgeObs(n, 14)
	n.ObserveToolRun("skills.write", map[string]any{"name": "s"}, nil)
	nudgeObs(n, 14)
	if line := n.NudgeLine(); line != "" {
		t.Fatalf("nudge survived the skills write: %q", line)
	}
	// 15 more ordinary iterations - fires.
	nudgeObs(n, 15)
	if line := n.NudgeLine(); line == "" {
		t.Fatal("nudge did not fire a full interval after the write")
	}
	// An update resets too, and so does a failed write. Reads do not.
	n.ObserveToolRun("terminal", map[string]any{}, nil)
	n.ObserveToolRun("skills.update", map[string]any{"name": "s"}, nil)
	n.ObserveToolRun("skills.write", map[string]any{"name": "s"}, errFake{})
	nudgeObs(n, 14)
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
	nudgeObs(n, 500)
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
				n.ObserveToolRun("terminal", map[string]any{}, nil)
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
