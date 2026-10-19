package kanban

import (
	"context"
	"database/sql"

	"github.com/EndoTheDev/omega/agent"
)

// getTaskTx scans one task inside an open transaction.
func getTaskTx(ctx context.Context, tx *sql.Tx, taskID string) (agent.KanbanTask, error) {
	row := tx.QueryRowContext(ctx, `SELECT id, title, body, assignee, status, session_id, source, created_at, started_at, completed_at FROM tasks WHERE id = ?`, taskID)
	t, err := scanTask(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return agent.KanbanTask{}, ErrNotFound
		}
		return agent.KanbanTask{}, err
	}
	return *t, nil
}

// recordEvent appends one audit-trail entry on the store's db handle.
func (s *Store) recordEvent(ctx context.Context, taskID, kind, detail string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO task_events (task_id, kind, detail, created_at) VALUES (?, ?, ?, ?)`,
		taskID, kind, detail, now())
	return err
}

// recordEventTx is recordEvent inside an open transaction.
func recordEventTx(ctx context.Context, tx *sql.Tx, taskID, kind, detail string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO task_events (task_id, kind, detail, created_at) VALUES (?, ?, ?, ?)`,
		taskID, kind, detail, now())
	return err
}