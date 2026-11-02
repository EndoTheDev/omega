package kanban

import (
	"context"
	"database/sql"
)

// ExecContexter is the write half *sql.DB and *sql.Tx share - the
// event recorder accepts either.
type ExecContexter interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// getTaskTx scans one task inside an open transaction. The board_id
// filter carries the isolation: a task id only answers on its board.
func getTaskTx(ctx context.Context, tx *sql.Tx, boardID int64, taskID string) (KanbanTask, error) {
	row := tx.QueryRowContext(ctx, `SELECT id, title, body, assignee, status, session_id, source, created_at, started_at, completed_at FROM tasks WHERE id = ? AND board_id = ?`, taskID, boardID)
	t, err := scanTask(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return KanbanTask{}, ErrNotFound
		}
		return KanbanTask{}, err
	}
	return *t, nil
}

// recordEvent appends one audit-trail entry (board_id stamped - the
// events all live in the one database now). db may be *sql.DB or an
// open *sql.Tx.
func recordEvent(ctx context.Context, db ExecContexter, boardID int64, taskID, kind, detail string) error {
	_, err := db.ExecContext(ctx, `INSERT INTO task_events (task_id, kind, detail, created_at, board_id) VALUES (?, ?, ?, ?, ?)`,
		taskID, kind, detail, now(), boardID)
	return err
}