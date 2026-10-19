package kanban

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// Status is the exact 9-status hermes board grammar. Any status may
// move to any other (one agent, no dispatch races to armor); unknown
// statuses are rejected at the door.
const (
	StatusTriage   = "triage"
	StatusTodo     = "todo"
	StatusScheduled = "scheduled"
	StatusReady    = "ready"
	StatusRunning  = "running"
	StatusBlocked  = "blocked"
	StatusReview   = "review"
	StatusDone     = "done"
	StatusArchived = "archived"
)

// statuses is the canonical ordered list (board column order).
var statuses = []string{
	StatusTriage, StatusTodo, StatusScheduled, StatusReady,
	StatusRunning, StatusBlocked, StatusReview, StatusDone, StatusArchived,
}

// Statuses returns the ordered status list.
func Statuses() []string {
	out := make([]string, len(statuses))
	copy(out, statuses)
	return out
}

// IsStatus reports whether s is a legal board status. The empty string
// is NOT a status: callers mean "no change" separately.
func IsStatus(s string) bool {
	for _, legal := range statuses {
		if s == legal {
			return true
		}
	}
	return false
}

// IsSettled reports whether the status is a terminal resting state
// (done, archived) - the board renders those columns dimmed.
func IsSettled(s string) bool {
	return s == StatusDone || s == StatusArchived
}

// newTaskID returns a "t_" + 8 random hex chars id (collision chance
// ~1 in 4 billion per draw; the store retries on the UNIQUE constraint
// just in case).
func newTaskID() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("t_%016x", time.Now().UnixNano())
	}
	return "t_" + hex.EncodeToString(b[:])
}

// now is the UTC RFC3339 timestamp stamp used across the module.
func now() string {
	return time.Now().UTC().Format(time.RFC3339)
}