package skills

import (
	"fmt"
	"strings"
	"sync"
)

// TurnNudger implements agent.TurnNudger, the learning-loop reminder seam. A pass
// constrained to probing - reads, greps, searches, shell.run - never arms the nudge, so
// it stays silent for its whole duration instead of interrupting a diagnosis with
// curator suggestions.
type TurnNudger struct {
	Interval int

	mu    sync.Mutex
	count int  // tool iterations since the last lesson or delivered nudge
	wrote bool // a write-class call landed in this same window
}

// NewTurnNudger builds the nudger from the skills config.
func NewTurnNudger(interval int) *TurnNudger {
	return &TurnNudger{Interval: interval}
}

// ObserveToolRun notes one executed tool call. Safe for concurrent calls: the loop
// executes tool calls in goroutines.
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

// skillsPersistedLesson reports whether one executed tool call was a skills write.
func skillsPersistedLesson(name string, _ map[string]any) bool {
	switch name {
	case "skills.write", "skills.update":
		return true
	}
	return false
}

// writeClassVerbs are the tool-name verbs that mutate state. Everything else is
// read-class: reads, greps, searches, delegate.task, unknown capsule tools - and
// shell.run, whose read/write nature cannot be known from the tool name (a probe pass
// and a coding pass both call it). A session that mutates ONLY through shell.run
// therefore never arms the nudge; the file, skills, memory and kanban tools are the
// honest signal.
var writeClassVerbs = map[string]bool{
	"write": true, "edit": true, "batch": true, "patch": true,
	"add": true, "replace": true, "remove": true, "delete": true,
	"create": true, "update": true, "save": true, "set": true,
	"move": true, "rename": true, "toggle": true, "complete": true,
	"comment": true, "curate": true, "apply": true, "rollback": true,
	"install": true, "enable": true, "disable": true, "archive": true,
	"pin": true, "upload": true, "commit": true, "post": true, "put": true,
}

// isWriteClass reports whether one tool name is write-class (mutates state): any name
// token that is exactly a writeClassVerbs entry wins.
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

// NudgeLine returns the reminder line due for the next turn prep ("" = none).
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
