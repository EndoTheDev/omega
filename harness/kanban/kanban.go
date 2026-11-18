package kanban

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"slices"
	"time"
)

// Status is the exact 9-status hermes board grammar.
const (
	StatusTriage    = "triage"
	StatusTodo      = "todo"
	StatusScheduled = "scheduled"
	StatusReady     = "ready"
	StatusRunning   = "running"
	StatusBlocked   = "blocked"
	StatusReview    = "review"
	StatusDone      = "done"
	StatusArchived  = "archived"
)

// statuses is the canonical ordered list (board column order).
var statuses = []string{
	StatusTriage, StatusTodo, StatusScheduled, StatusReady,
	StatusRunning, StatusBlocked, StatusReview, StatusDone, StatusArchived,
}

// IsStatus reports whether s is a legal board status. The empty string
// is NOT a status: callers mean "no change" separately.
func IsStatus(s string) bool {
	return slices.Contains(statuses, s)
}

// StatusAll is a list-only filter value, never a board status.
const StatusAll = "all"

// IsListStatus reports whether s is a legal list filter.
func IsListStatus(s string) bool {
	return s == StatusAll || IsStatus(s)
}

// IsSettled reports whether the status is a terminal resting state (done, archived).
func IsSettled(s string) bool {
	return s == StatusDone || s == StatusArchived
}

// newTaskID returns a "t_" + 8 random hex chars id (collision chance ~1 in 4 billion
// per draw.
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
