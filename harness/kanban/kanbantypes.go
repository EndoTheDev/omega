package kanban

import (
	"context"
	"errors"
)

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
	// Board is filled only by the all-boards read (GET /tasks?scope=all); every
	// other route addresses a card by (board, id).
	Board string `json:"board,omitempty"`
}

type KanbanComment struct {
	ID        int64  `json:"id"`
	TaskID    string `json:"task_id"`
	Author    string `json:"author"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
}

type KanbanEvent struct {
	ID        int64  `json:"id"`
	TaskID    string `json:"task_id"`
	Kind      string `json:"kind"`
	Detail    string `json:"detail"`
	CreatedAt string `json:"created_at"`
}

// KanbanPatch is a partial task update from HTTP PATCH or task.update: pointer fields
// distinguish "set (possibly empty)" from "leave alone", and Status uses "" for no change.
type KanbanPatch struct {
	Title     *string
	Body      *string
	Assignee  *string
	Priority  *int
	Status    string // "" = no change
	SessionID *string
	Source    *string
}

func ValidPriority(p int) bool { return p >= 0 }

// ErrTaskNotFound is the shared "no such task" sentinel on the kanban seam: the HTTP
// channel maps it to a 404, so modules need not import each other to compare errors.
var ErrTaskNotFound = errors.New("task not found")

type BoardInfo struct {
	Name string `json:"name"`
	Current bool `json:"current"`
}

type BoardLister interface {
	Boards(ctx context.Context) ([]BoardInfo, error)
}

// Reorderer is the optional drag-order capability on the kanban seam.
type Reorderer interface {
	ReorderTask(ctx context.Context, board, taskID, status, afterID string) (KanbanTask, error)
}

// TaskDeleter is the optional delete capability on the kanban seam. A provider that
// cannot delete stays legal without it - the DELETE route then answers 400, and the board
// UI's delete control refuses.
type TaskDeleter interface {
	DeleteTask(ctx context.Context, board, taskID string) error
}

// Batcher is the optional atomic-batch capability on the kanban seam: the tool surface's
// `operations` array runs entirely inside one transaction, so a failed operation rolls
// back everything the batch already applied - a failed batch cannot orphan its creates.
type Batcher interface {
	BeginBatch(ctx context.Context) (batch KanbanProvider, commit func() error, cancel func(), err error)
}

type KanbanProvider interface {
	CreateTask(ctx context.Context, board string, in KanbanTask) (KanbanTask, error)
	ListTasks(ctx context.Context, board, status string) ([]KanbanTask, error)
	// GetTask returns one task by id on the named board (store.ErrNotFound when missing).
	GetTask(ctx context.Context, board, id string) (KanbanTask, error)
	GetTaskComments(ctx context.Context, board, taskID string) ([]KanbanComment, error)
	GetTaskEvents(ctx context.Context, board, taskID string) ([]KanbanEvent, error)
	// UpdateTask applies the patch on the named board and writes the audit events. There is
	// deliberately no cross-board move: changing board is a pointer switch, never a patch.
	UpdateTask(ctx context.Context, board, taskID string, patch KanbanPatch) (KanbanTask, error)
	// AddComment appends to the task's thread and writes the comment event. Author must be
	// non-empty.
	AddComment(ctx context.Context, board, taskID, author, body string) (KanbanComment, error)
	Close() error
}
