package skills

import (
	"fmt"
	"sync"
)

// TurnNudger implements core.TurnNudger, the learning-loop reminder
// seam (card t_9972dfd0). The loop reports every executed tool call;
// the nudger counts tool iterations since the last persisted lesson
// (a skills write or update) and, at the configured interval, hands
// back the one-line reminder delivered in the next turn prep.
//
// v1 delivery is the simple one the card rules: ONE prompt line, no
// background fork. The nudge consumes itself when it fires, so the
// next reminder is a full interval away.
type TurnNudger struct {
	// Interval is `skills.nudge_interval`: tool iterations between
	// reminders. 0 = nudging off (Observation is cheap accounting,
	// NudgeLine permanently empty).
	Interval int

	mu    sync.Mutex
	count int
}

// NewTurnNudger builds the nudger from the skills config.
func NewTurnNudger(interval int) *TurnNudger {
	return &TurnNudger{Interval: interval}
}

// ObserveToolRun notes one executed tool call. Safe for concurrent
// calls: the loop executes tool calls in goroutines. A skills write
// or update IS the persisted lesson the learning loop measures -
// reset the counter. A failed write still persists nothing, but the
// model tried; the reset is the same either way (Hermes resets on
// the skill-write call, not on its success).
func (n *TurnNudger) ObserveToolRun(name string, args map[string]any, _ error) {
	if n == nil || n.Interval <= 0 {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if skillsPersistedLesson(name, args) {
		n.count = 0
		return
	}
	n.count++
}

// skillsPersistedLesson reports whether one executed tool call was a
// skills write or update - the two actions that persist a lesson.
func skillsPersistedLesson(name string, args map[string]any) bool {
	if name != "skills" {
		return false
	}
	switch a, _ := args["action"].(string); a {
	case "write", "update":
		return true
	}
	return false
}

// NudgeLine returns the reminder line due for the next turn prep (""
// = none). A due nudge consumes itself: the counter resets so the
// next reminder is a full interval later.
func (n *TurnNudger) NudgeLine() string {
	if n == nil || n.Interval <= 0 {
		return ""
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.count < n.Interval {
		return ""
	}
	n.count = 0
	return fmt.Sprintf("Learning loop: %d tool iterations without persisting a lesson - consider a skills.write (create or patch a skill capturing what this work revealed).", n.Interval)
}