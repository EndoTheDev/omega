package agent

import (
	"context"
	"errors"
)

// KanbanTask is one board task. Timestamps are RFC3339 UTC strings;
// zero-value ("") timestamps are unset. All 9 statuses share the
// hermes board grammar: triage todo scheduled ready running blocked
// review done archived.
type KanbanTask struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Body        string `json:"body"`
	Assignee    string `json:"assignee"`
	Status      string `json:"status"`
	SessionID   string `json:"session_id"`
	Source      string `json:"source"`
	CreatedAt   string `json:"created_at"`
	StartedAt   string `json:"started_at"`
	CompletedAt string `json:"completed_at"`
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
	Status    string // "" = no change
	SessionID *string
	Source    *string
}

// ErrTaskNotFound is the shared "no such task" sentinel on the kanban
// seam: the store returns it and the HTTP channel maps it to a 404
// (modules must not import each other to compare errors).
var ErrTaskNotFound = errors.New("task not found")

// KanbanProvider is the board seam: task CRUD plus the comment and
// event threads. Implemented by the kanban module; consumed by the
// HTTP channel (routes) and the kanban tools.
type KanbanProvider interface {
	// CreateTask inserts a task. The store fills ID, CreatedAt and the
	// status default ("triage"), and writes the created event.
	CreateTask(ctx context.Context, in KanbanTask) (KanbanTask, error)
	// ListTasks returns tasks, newest first, filtered by status.
	// Empty status lists every status.
	ListTasks(ctx context.Context, status string) ([]KanbanTask, error)
	// GetTask returns one task by id (store.ErrNotFound when missing).
	GetTask(ctx context.Context, id string) (KanbanTask, error)
	// GetTaskComments returns the task's comment thread, oldest first.
	GetTaskComments(ctx context.Context, taskID string) ([]KanbanComment, error)
	// GetTaskEvents returns the task's audit trail, oldest first.
	GetTaskEvents(ctx context.Context, taskID string) ([]KanbanEvent, error)
	// UpdateTask applies the patch and writes the audit events
	// (moved/assigned/edited/linked). Returns the updated task.
	UpdateTask(ctx context.Context, taskID string, patch KanbanPatch) (KanbanTask, error)
	// AddComment appends to the task's thread and writes the
	// comment_added event. Author must be non-empty.
	AddComment(ctx context.Context, taskID, author, body string) (KanbanComment, error)
	// Close releases the underlying resources.
	Close() error
}