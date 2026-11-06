package kanban

import (
	"context"
	"errors"
)

// KanbanTask is one board task. Timestamps are RFC3339 UTC strings;
// zero-value ("") timestamps are unset. All 9 statuses share the
// hermes board grammar: triage todo scheduled ready running blocked
// review done archived. Position is the card's order inside its
// column - store-internal state, never serialized to the wire (the
// GET /tasks list arrives in position order already).
// position-order already). Assigned/started/completed stamps and
// Position are store-owned fields too; Priority is the urgency
// tiebreak (Hermes parity: higher = picked sooner, 0 = normal,
// negative is refused at the door).
type KanbanTask struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Body        string `json:"body"`
	Assignee    string `json:"assignee"`
	Priority    int    `json:"priority"`
	Status      string `json:"status"`
	SessionID   string `json:"session_id"`
	Source      string `json:"source"`
	CreatedAt   string `json:"created_at"`
	StartedAt   string `json:"started_at"`
	CompletedAt string `json:"completed_at"`
	Position    int64  `json:"-"`
}

// KanbanComment is one comment in a task's thread.
type KanbanComment struct {
	ID        int64  `json:"id"`
	TaskID    string `json:"task_id"`
	Author    string `json:"author"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
}

// KanbanEvent is one audit-trail entry on a task (created, moved,
// comment, ...). Events are write-only history; nothing deletes them.
type KanbanEvent struct {
	ID        int64  `json:"id"`
	TaskID    string `json:"task_id"`
	Kind      string `json:"kind"`
	Detail    string `json:"detail"`
	CreatedAt string `json:"created_at"`
}

// KanbanPatch is a partial task update from HTTP PATCH or task.update.
// Pointer fields distinguish "change to this value (possibly empty)"
// from "leave alone"; empty strings mean "no change" for simple fields
// because Go zero values cannot express omission without pointers.
type KanbanPatch struct {
	Title     *string
	Body      *string
	Assignee  *string
	Priority  *int
	Status    string // "" = no change
	SessionID *string
	Source    *string
}

// ValidPriority reports whether p may ride a task: the urgency
// tiebreak is non-negative (0 = normal; higher = more urgent, the
// Hermes parity law).
func ValidPriority(p int) bool { return p >= 0 }

// ErrTaskNotFound is the shared "no such task" sentinel on the kanban
// seam: the store returns it and the HTTP channel maps it to a 404
// (modules must not import each other to compare errors).
var ErrTaskNotFound = errors.New("task not found")

// BoardInfo is one row of the board registry (multi-board listing).
type BoardInfo struct {
	// Name is the board's registry name ("main" is the default).
	Name string `json:"name"`
	// Current reports whether this row carries the is_current flag
	// (the board "" resolves to).
	Current bool `json:"current"`
}

// BoardLister is the optional multi-board capability on the kanban
// seam. The HTTP channel serves GET /boards only when the mounted
// provider also implements this - single-board implementations stay
// legal by implementing just KanbanProvider.
type BoardLister interface {
	// Boards lists every board the registry knows about, the current
	// one flagged. Never includes non-existent boards.
	Boards(ctx context.Context) ([]BoardInfo, error)
}

// Reorderer is the optional drag-order capability on the kanban seam:
// place a task at an explicit slot (the column plus the sibling it
// lands after, "" = top). The kanban module's Store implements it; a
// provider without order persistence stays legal without it (the
// PATCH after_id then answers 400 - the UI refetches and the card
// snaps back).
type Reorderer interface {
	// ReorderTask moves taskID to (status, after afterID) on the named
	// board and resequences that column to dense positions. status ""
	// keeps the current column; afterID "" drops at the top.
	ReorderTask(ctx context.Context, board, taskID, status, afterID string) (KanbanTask, error)
}

// TaskDeleter is the optional delete capability on the kanban seam
// (BoardLister/Reorderer's shape): permanently remove a card. A
// provider that cannot delete stays legal without it - the DELETE
// route then answers 400 with that reason, and the board UI's delete
// control refuses. Deleting a card takes its comments and events with
// it (the FK cascades); archive is the reversible path, this is not.
type TaskDeleter interface {
	// DeleteTask removes taskID from the named board. Missing tasks
	// answer ErrTaskNotFound.
	DeleteTask(ctx context.Context, board, taskID string) error
}

// KanbanProvider is the board seam: task CRUD plus the comment and
// event threads. Implemented by the kanban module; consumed by the
// HTTP channel (routes) and the kanban tools.
type KanbanProvider interface {
	// CreateTask inserts a task on the named board ("" = the current
	// board, resolved at mount from the pointer). The store fills ID,
	// CreatedAt and the status default ("triage"), and writes the
	// created event.
	CreateTask(ctx context.Context, board string, in KanbanTask) (KanbanTask, error)
	// ListTasks returns the board's tasks, newest first, filtered by
	// status. Empty status lists every status.
	ListTasks(ctx context.Context, board, status string) ([]KanbanTask, error)
	// GetTask returns one task by id on the named board
	// (store.ErrNotFound when missing).
	GetTask(ctx context.Context, board, id string) (KanbanTask, error)
	// GetTaskComments returns the task's comment thread, oldest first.
	GetTaskComments(ctx context.Context, board, taskID string) ([]KanbanComment, error)
	// GetTaskEvents returns the task's audit trail, oldest first.
	GetTaskEvents(ctx context.Context, board, taskID string) ([]KanbanEvent, error)
	// UpdateTask applies the patch on the named board and writes the
	// audit events (moved/assigned/edited/linked). Returns the updated
	// task. There is deliberately no cross-board move: changing board
	// is a pointer switch (two operations), never a task patch.
	UpdateTask(ctx context.Context, board, taskID string, patch KanbanPatch) (KanbanTask, error)
	// AddComment appends to the task's thread and writes the
	// comment event. Author must be non-empty.
	AddComment(ctx context.Context, board, taskID, author, body string) (KanbanComment, error)
	// Close releases the underlying resources.
	Close() error
}
