package skills

import (
	"fmt"
	"strings"
	"sync"
)

// TurnNudger implements agent.TurnNudger, the learning-loop reminder
// seam (card t_9972dfd0). The loop reports every executed tool call;
// the nudger counts tool iterations since the last persisted lesson
// (a skills write or update) and, at the configured interval, hands
// back the one-line reminder delivered in the next turn prep.
//
// v1 delivery is the simple one the card rules: ONE prompt line, no
// background fork. The nudge consumes itself when it fires, so the
// next reminder is a full interval away.
//
// READ-ONLY BACK-OFF (card t_d5d4d586, QA finding A5): the reminder
// only fires when the counted window contains at least one
// write-class tool call (see isWriteClass). A pass constrained to
// probing - reads, greps, searches, shell.run - never arms the
// nudge, so it stays silent for its whole duration instead of
// interrupting a diagnosis with curator suggestions. The counter
// keeps running while the nudge is backed off, so the first
// write-class call after a long read stretch makes the reminder due
// at the next turn prep.
type TurnNudger struct {
	// Interval is `skills.nudge_interval`: tool iterations between
	// reminders. 0 = nudging off (Observation is cheap accounting,
	// NudgeLine permanently empty).
	Interval int

	mu    sync.Mutex
	count int  // tool iterations since the last lesson or delivered nudge
	wrote bool // a write-class call landed in this same window
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
		n.wrote = false
		return
	}
	n.count++
	if isWriteClass(name) {
		n.wrote = true
	}
}

// skillsPersistedLesson reports whether one executed tool call was a
// skills write or update - the two tools that persist a lesson (the
// dotted skills wire, t_d8fbad57: no action arg to inspect, the
// verb IS the tool name).
func skillsPersistedLesson(name string, _ map[string]any) bool {
	switch name {
	case "skills.write", "skills.update":
		return true
	}
	return false
}

// writeClassVerbs are the tool-name verbs that mutate state. A tool
// whose name carries one of these tokens changes something, and only
// such a call arms the learning-loop reminder. Dots, underscores,
// dashes and slashes separate tokens, so `files.batch` and a bridged
// `mcp__fs__write_file` both parse.
//
// Everything else is read-class: reads, greps, searches,
// delegate.task, unknown capsule tools - and shell.run, whose
// read/write nature cannot be known from the tool name (a probe pass
// and a coding pass both call it). A session that mutates ONLY
// through shell.run therefore never arms the nudge; the file, skills,
// memory and kanban tools are the honest signal.
var writeClassVerbs = map[string]bool{
	"write": true, "edit": true, "batch": true, "patch": true,
	"add": true, "replace": true, "remove": true, "delete": true,
	"create": true, "update": true, "save": true, "set": true,
	"move": true, "rename": true, "toggle": true, "complete": true,
	"comment": true, "curate": true, "apply": true, "rollback": true,
	"install": true, "enable": true, "disable": true, "archive": true,
	"pin": true, "upload": true, "commit": true, "post": true, "put": true,
}

// isWriteClass reports whether one tool name is write-class (mutates
// state): any name token that is exactly a writeClassVerbs entry
// wins. Unknown names are read-class - the conservative arm, so an
// unrecognized mutating tool silences the nudge instead of nagging a
// probe pass.
func isWriteClass(name string) bool {
	tokens := strings.FieldsFunc(name, func(r rune) bool {
		switch r {
		case '.', '_', '-', '/':
			return true
		}
		return false
	})
	for _, tok := range tokens {
		if writeClassVerbs[strings.ToLower(tok)] {
			return true
		}
	}
	return false
}

// NudgeLine returns the reminder line due for the next turn prep (""
// = none). A due nudge consumes itself: the counter and the
// write-class arm reset so the next reminder is a full interval
// later. A window with no write-class call is a read-only stretch -
// the line stays empty and the counter keeps running.
func (n *TurnNudger) NudgeLine() string {
	if n == nil || n.Interval <= 0 {
		return ""
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.count < n.Interval {
		return ""
	}
	if !n.wrote {
		return ""
	}
	n.count = 0
	n.wrote = false
	return fmt.Sprintf("Learning loop: %d tool iterations without persisting a lesson - consider a skills.write (create or patch a skill capturing what this work revealed).", n.Interval)
}
