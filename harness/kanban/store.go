package kanban

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/datadir"
)

// ErrNotFound is returned when a task does not exist (the agent seam's
// shared sentinel, aliased here so store callers stay short).
var ErrNotFound = ErrTaskNotFound

// Store implements KanbanProvider over SQLite. Safe for concurrent use via *sql.DB (WAL
// + busy_timeout, same recipe as the session store).
type Store struct {
	db *sql.DB

	// q is the statement surface every task read and write executes through. Registry ops
	// (boards.go) stay on db directly - ATTACH/DETACH and Begin cannot run inside a
	// transaction, and registry work never joins a batch.
	q stmtDB

	// tx is the open batch transaction this store executes inside.
	tx *sql.Tx

	// batchEmits buffers finished-things emissions while a batch is open. Nil on a plain
	// store (emit at once).
	batchEmits *[]string

	// notifier is the finished-things seam, wired by the kanban plugin after MountAllAPI.
	// Nil-safe: a store without the seam just doesn't notify.
	notifier finishedThings
}

// stmtDB is the full statement face *sql.DB and *sql.Tx share.
type stmtDB interface {
	ExecContexter
	QueryRowContexter
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// finishedThings is the consumer-side face of the finished-things store.
type finishedThings interface {
	Emit(n core.Notice) (core.Notice, error)
}

// SetNotifier wires the finished-things consumers face.
func (s *Store) SetNotifier(n finishedThings) {
	s.notifier = n
}

// emitDone records one task notification when a task ENTERED done in this update.
// Best-effort: a notification failure never fails the board write (the caller swallows
// the error). Inside a batch the emission buffers: replayed on commit, dropped on
// rollback - a batch that reverts cannot leave a done-notice behind.
func (s *Store) emitDone(taskID string) {
	if s.batchEmits != nil {
		*s.batchEmits = append(*s.batchEmits, taskID)
		return
	}
	s.noticeDone(taskID)
}

// noticeDone emits through the notifier (nil-safe).
func (s *Store) noticeDone(taskID string) {
	if s.notifier == nil {
		return
	}
	_, _ = s.notifier.Emit(core.Notice{
		Kind:     "task",
		SourceID: taskID,
		Title:    "task " + taskID + " done",
		Verdict:  "ok",
	})
}

// Compile-time assertions: *Store implements the kanban seam plus the
// board-registry, delete and atomic-batch capabilities.
var (
	_ KanbanProvider = (*Store)(nil)
	_ BoardLister    = (*Store)(nil)
	_ Reorderer      = (*Store)(nil)
	_ TaskDeleter    = (*Store)(nil)
	_ Batcher        = (*Store)(nil)
)

// Open opens (creating if needed) the board database at dsn and ensures the schema.
// ":memory:" works for SEQUENTIAL tests only - the loop executes tool calls
// concurrently, and every fresh pooled connection to a:memory: dsn is its own empty
// database (use a file dsn under concurrency).
func Open(dsn string) (*Store, error) {
	if err := datadir.Migrate(dsn); err != nil {
		return nil, err
	}
	db, err := openDB(dsn)
	if err != nil {
		return nil, err
	}
	s := &Store{db: db, q: db}
	if err := s.migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// openDB is the dsn-widening + sql.Open step, shared by Open.
func openDB(dsn string) (*sql.DB, error) {
	dsnParams := dsn
	if !strings.Contains(dsn, "?") {
		dsnParams += "?_journal_mode=WAL&_busy_timeout=5000"
	} else if !strings.Contains(dsnParams, "_journal_mode=") {
		dsnParams += "&_journal_mode=WAL&_busy_timeout=5000"
	}
	db, err := sql.Open("sqlite", dsnParams)
	if err != nil {
		return nil, err
	}
	return db, nil
}

// Close releases the database handle.
func (s *Store) Close() error {
	return s.db.Close()
}

// migrate ensures the schema on the single database.
const boardIndexes = `
CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_task_comments_task ON task_comments(task_id, id);
CREATE INDEX IF NOT EXISTS idx_task_events_task ON task_events(task_id, id);
CREATE INDEX IF NOT EXISTS idx_tasks_board ON tasks(board_id);
CREATE INDEX IF NOT EXISTS idx_task_comments_board ON task_comments(board_id);
CREATE INDEX IF NOT EXISTS idx_task_events_board ON task_events(board_id);
`

func (s *Store) migrate(db *sql.DB) error {
	if err := ensureBoardsTable(db); err != nil {
		return err
	}
	if _, err := db.Exec(taskTables); err != nil {
		return err
	}
	if err := rebuildLegacyRows(db); err != nil {
		return err
	}
	if err := migrateTaskPositions(db); err != nil {
		return err
	}
	if err := migrateTaskPriority(db); err != nil {
		return err
	}
	_, err := db.Exec(boardIndexes)
	return err
}

// migrateTaskPositions adds the drag-order column when it is missing.
func migrateTaskPositions(db *sql.DB) error {
	if !tableMissingColumn(db, "tasks", "position") {
		return nil
	}
	if _, err := db.Exec(`ALTER TABLE tasks ADD COLUMN position INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	_, err := db.Exec(`
UPDATE tasks SET position = (
	SELECT COUNT(*) FROM tasks t2
	WHERE t2.board_id = tasks.board_id AND t2.status = tasks.status
	  AND (t2.created_at, t2.id) > (tasks.created_at, tasks.id))`)
	return err
}

// taskTables is the three card tables in the boards-aware shape.
const taskTables = `
CREATE TABLE IF NOT EXISTS tasks (
	id           TEXT PRIMARY KEY,
	title        TEXT NOT NULL,
	body         TEXT NOT NULL DEFAULT '',
	assignee     TEXT NOT NULL DEFAULT '',
	status       TEXT NOT NULL DEFAULT 'triage',
	session_id   TEXT NOT NULL DEFAULT '',
	source       TEXT NOT NULL DEFAULT '',
	created_at   TEXT NOT NULL,
	started_at   TEXT NOT NULL DEFAULT '',
	completed_at TEXT NOT NULL DEFAULT '',
	priority     INTEGER NOT NULL DEFAULT 0,
	board_id     INTEGER NOT NULL REFERENCES boards(id) ON DELETE CASCADE,
	position     INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS task_comments (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	task_id    TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
	author     TEXT NOT NULL,
	body       TEXT NOT NULL,
	created_at TEXT NOT NULL,
	board_id   INTEGER NOT NULL REFERENCES boards(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS task_events (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	task_id    TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
	kind       TEXT NOT NULL,
	detail     TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	board_id   INTEGER NOT NULL REFERENCES boards(id) ON DELETE CASCADE
);
`

// rebuildLegacyRows upgrades a database from the boardless legacy shape. The table
// re-creation must exist in the same file because sqlite has no ALTER TABLE ADD
// CONSTRAINT.
func rebuildLegacyRows(db *sql.DB) error {
	if !tableMissingColumn(db, "tasks", "board_id") {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// board_id backfill: everything already in this db is main's
	stmts := []string{
		`ALTER TABLE task_events RENAME TO legacy_task_events`,
		`ALTER TABLE task_comments RENAME TO legacy_task_comments`,
		`ALTER TABLE tasks RENAME TO legacy_tasks`,
		`CREATE TABLE tasks (
	id           TEXT PRIMARY KEY,
	title        TEXT NOT NULL,
	body         TEXT NOT NULL DEFAULT '',
	assignee     TEXT NOT NULL DEFAULT '',
	status       TEXT NOT NULL DEFAULT 'triage',
	session_id   TEXT NOT NULL DEFAULT '',
	source       TEXT NOT NULL DEFAULT '',
	created_at   TEXT NOT NULL,
	started_at   TEXT NOT NULL DEFAULT '',
	completed_at TEXT NOT NULL DEFAULT '',
	priority     INTEGER NOT NULL DEFAULT 0,
	board_id     INTEGER NOT NULL REFERENCES boards(id) ON DELETE CASCADE
)`,
		`CREATE TABLE task_comments (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	task_id    TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
	author     TEXT NOT NULL,
	body       TEXT NOT NULL,
	created_at TEXT NOT NULL,
	board_id   INTEGER NOT NULL REFERENCES boards(id) ON DELETE CASCADE
)`,
		`CREATE TABLE task_events (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	task_id    TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
	kind       TEXT NOT NULL,
	detail     TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	board_id   INTEGER NOT NULL REFERENCES boards(id) ON DELETE CASCADE
)`,
		`INSERT INTO tasks (id, title, body, assignee, status, session_id, source, created_at, started_at, completed_at, board_id)
			SELECT id, title, body, assignee, status, session_id, source, created_at, started_at, completed_at,
			       (SELECT id FROM boards WHERE name = 'main') FROM legacy_tasks`,
		`INSERT INTO task_comments (task_id, author, body, created_at, board_id)
			SELECT task_id, author, body, created_at,
			       (SELECT id FROM boards WHERE name = 'main') FROM legacy_task_comments`,
		`INSERT INTO task_events (task_id, kind, detail, created_at, board_id)
			SELECT task_id, kind, detail, created_at,
			       (SELECT id FROM boards WHERE name = 'main') FROM legacy_task_events`,
		`DROP TABLE legacy_tasks`,
		`DROP TABLE legacy_task_comments`,
		`DROP TABLE legacy_task_events`,
	}
	for _, q := range stmts {
		if _, err := tx.Exec(q); err != nil {
			return fmt.Errorf("kanban schema upgrade: %w", err)
		}
	}
	return tx.Commit()
}

// tableMissingColumn reports whether table lacks column.
func tableMissingColumn(db *sql.DB, table, column string) bool {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return false
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notNull int
			dflt    any
			pk      int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notNull, &dflt, &pk); err != nil {
			return false
		}
		if name == column {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		return false
	}
	return !found
}

// topPosition is the slot a landing card takes on top of its column.
func topPosition(ctx context.Context, db QueryRowContexter, boardID int64, status string) (int64, error) {
	var top int64
	err := db.QueryRowContext(ctx,
		`SELECT COALESCE(MIN(position), 0) - 1 FROM tasks WHERE board_id = ? AND status = ?`,
		boardID, status).Scan(&top)
	return top, err
}

// QueryRowContexter is the single-row read half *sql.DB and *sql.Tx share.
type QueryRowContexter interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// writeScope is one task-write transaction.
type writeScope struct {
	tx     *sql.Tx
	commit func() error
	revert func()
}

// beginWrite opens the transaction one mutating task op needs.
func (s *Store) beginWrite(ctx context.Context) (writeScope, error) {
	if s.tx != nil {
		return writeScope{tx: s.tx, commit: func() error { return nil }, revert: func() {}}, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return writeScope{}, err
	}
	return writeScope{tx: tx, commit: tx.Commit, revert: func() { _ = tx.Rollback() }}, nil
}

// BeginBatch opens one SQLite transaction and returns a clone of the store that
// executes every op inside it (the Batcher capability, the atomic kanban batch). A
// nested batch is refused: there is exactly one open batch transaction per store.
func (s *Store) BeginBatch(ctx context.Context) (KanbanProvider, func() error, func(), error) {
	if s.tx != nil {
		return nil, nil, nil, fmt.Errorf("a batch is already open")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, nil, err
	}
	b := *s
	b.q = tx
	b.tx = tx
	emits := []string{}
	b.batchEmits = &emits
	commit := func() error {
		if err := tx.Commit(); err != nil {
			return err
		}
		for _, id := range *b.batchEmits {
			b.noticeDone(id)
		}
		*b.batchEmits = nil
		return nil
	}
	cancel := func() { _ = tx.Rollback() }
	return &b, commit, cancel, nil
}

// migrateTaskPriority adds the urgency column when it is missing.
func migrateTaskPriority(db *sql.DB) error {
	if !tableMissingColumn(db, "tasks", "priority") {
		return nil
	}
	_, err := db.Exec(`ALTER TABLE tasks ADD COLUMN priority INTEGER NOT NULL DEFAULT 0`)
	return err
}

// CreateTask inserts a task on the named board ("" = the current board). ID, Status
// (default triage) and CreatedAt are filled by the store; title must be non-empty.
func (s *Store) CreateTask(ctx context.Context, board string, in KanbanTask) (KanbanTask, error) {
	if strings.TrimSpace(in.Title) == "" {
		return KanbanTask{}, fmt.Errorf("task title is required")
	}
	status := in.Status
	if status == "" {
		status = StatusTriage
	}
	if !IsStatus(status) {
		return KanbanTask{}, fmt.Errorf("illegal status %q", status)
	}
	if !ValidPriority(in.Priority) {
		return KanbanTask{}, fmt.Errorf("illegal priority %d", in.Priority)
	}
	bid, err := s.boardID(board)
	if err != nil {
		return KanbanTask{}, err
	}
	top, err := topPosition(ctx, s.q, bid, status)
	if err != nil {
		return KanbanTask{}, err
	}
	in.Position = top
	for range 5 {
		in.ID = newTaskID()
		in.Status = status
		in.CreatedAt = now()
		_, err := s.q.ExecContext(ctx, `
INSERT INTO tasks (id, title, body, assignee, status, session_id, source, created_at, started_at, completed_at, priority, board_id, position)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			in.ID, in.Title, in.Body, in.Assignee, in.Status,
			in.SessionID, in.Source, in.CreatedAt, in.StartedAt, in.CompletedAt, in.Priority, bid, in.Position)
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				continue // id collision: retry with a fresh draw
			}
			return KanbanTask{}, err
		}
		if err := recordEvent(ctx, s.q, bid, in.ID, "created", describeNew(in)); err != nil {
			return KanbanTask{}, err
		}
		return in, nil
	}
	return KanbanTask{}, fmt.Errorf("could not allocate a task id after 5 tries")
}

// describeNew renders the created-event detail line.
func describeNew(t KanbanTask) string {
	detail := "status " + t.Status
	if t.Priority != 0 {
		detail += fmt.Sprintf(", priority %d", t.Priority)
	}
	if t.SessionID != "" {
		detail += ", session " + t.SessionID
	}
	if t.Source != "" {
		detail += ", source " + t.Source
	}
	return detail
}

// ListTasks returns the board's tasks in board order.
func (s *Store) ListTasks(ctx context.Context, board, status string) ([]KanbanTask, error) {
	bid, err := s.boardID(board)
	if err != nil {
		if errors.Is(err, errNoBoards) {
			return []KanbanTask{}, nil // zero boards is legal: the list is empty, not broken
		}
		return nil, err
	}
	query := `SELECT id, title, body, assignee, status, session_id, source, created_at, started_at, completed_at, priority, position FROM tasks WHERE board_id = ?`
	where, args, err := statusFilter(status)
	if err != nil {
		return nil, err
	}
	query += where
	args = append([]any{bid}, args...)
	query += " ORDER BY position ASC, created_at DESC, id DESC"
	rows, err := s.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []KanbanTask{}
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// statusFilter turns the ?status= argument into a WHERE fragment: "" hides archived
// (the active view), StatusAll lists everything, and a named status must be legal.
// Shared by the board read and the all-boards read so the two can never drift.
func statusFilter(status string) (string, []any, error) {
	switch status {
	case "":
		return " AND status != ?", []any{StatusArchived}, nil
	case StatusAll:
		return "", nil, nil
	default:
		if !IsStatus(status) {
			return "", nil, fmt.Errorf("illegal status %q", status)
		}
		return " AND status = ?", []any{status}, nil
	}
}

// ListAllTasks is the all-boards read behind the board view's "all boards" search.
func (s *Store) ListAllTasks(ctx context.Context, status string) ([]KanbanTask, error) {
	where, args, err := statusFilter(status)
	if err != nil {
		return nil, err
	}
	query := `SELECT t.id, t.title, t.body, t.assignee, t.status, t.session_id, t.source,
		t.created_at, t.started_at, t.completed_at, t.priority, t.position, b.name
		FROM tasks t JOIN boards b ON b.id = t.board_id WHERE 1 = 1` + where +
		` ORDER BY b.position, b.id, t.position ASC, t.created_at DESC, t.id DESC`
	rows, err := s.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []KanbanTask{}
	for rows.Next() {
		var t KanbanTask
		if err := rows.Scan(&t.ID, &t.Title, &t.Body, &t.Assignee, &t.Status,
			&t.SessionID, &t.Source, &t.CreatedAt, &t.StartedAt, &t.CompletedAt,
			&t.Priority, &t.Position, &t.Board); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func scanTask(row interface{ Scan(...any) error }) (*KanbanTask, error) {
	var t KanbanTask
	err := row.Scan(&t.ID, &t.Title, &t.Body, &t.Assignee, &t.Status,
		&t.SessionID, &t.Source, &t.CreatedAt, &t.StartedAt, &t.CompletedAt, &t.Priority, &t.Position)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// GetTask returns one task by id on the named board.
func (s *Store) GetTask(ctx context.Context, board, id string) (KanbanTask, error) {
	bid, err := s.boardID(board)
	if err != nil {
		return KanbanTask{}, err
	}
	row := s.q.QueryRowContext(ctx, `SELECT id, title, body, assignee, status, session_id, source, created_at, started_at, completed_at, priority, position FROM tasks WHERE id = ? AND board_id = ?`, id, bid)
	t, err := scanTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return KanbanTask{}, ErrNotFound
	}
	if err != nil {
		return KanbanTask{}, err
	}
	return *t, nil
}

// GetTaskComments returns the comment thread, oldest first.
func (s *Store) GetTaskComments(ctx context.Context, board, taskID string) ([]KanbanComment, error) {
	bid, err := s.boardID(board)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.QueryContext(ctx, `
SELECT id, task_id, author, body, created_at FROM task_comments WHERE task_id = ? AND board_id = ? ORDER BY id`, taskID, bid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []KanbanComment{}
	for rows.Next() {
		var c KanbanComment
		if err := rows.Scan(&c.ID, &c.TaskID, &c.Author, &c.Body, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetTaskEvents returns the audit trail, oldest first.
func (s *Store) GetTaskEvents(ctx context.Context, board, taskID string) ([]KanbanEvent, error) {
	bid, err := s.boardID(board)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.QueryContext(ctx, `
SELECT id, task_id, kind, detail, created_at FROM task_events WHERE task_id = ? AND board_id = ? ORDER BY id`, taskID, bid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []KanbanEvent{}
	for rows.Next() {
		var e KanbanEvent
		if err := rows.Scan(&e.ID, &e.TaskID, &e.Kind, &e.Detail, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// AddComment appends to the thread and writes the comment event.
func (s *Store) AddComment(ctx context.Context, board, taskID, author, body string) (KanbanComment, error) {
	if strings.TrimSpace(body) == "" {
		return KanbanComment{}, fmt.Errorf("comment body is required")
	}
	if author == "" {
		author = "user"
	}
	bid, err := s.boardID(board)
	if err != nil {
		return KanbanComment{}, err
	}
	// the FK needs a live task; GetTask also answers ErrNotFound cleanly
	if _, err := s.GetTask(ctx, board, taskID); err != nil {
		return KanbanComment{}, err
	}
	res, err := s.q.ExecContext(ctx, `
INSERT INTO task_comments (task_id, author, body, created_at, board_id) VALUES (?, ?, ?, ?, ?)`,
		taskID, author, body, now(), bid)
	if err != nil {
		return KanbanComment{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return KanbanComment{}, err
	}
	c := KanbanComment{ID: id, TaskID: taskID, Author: author, Body: body, CreatedAt: now()}
	err = recordEvent(ctx, s.q, bid, taskID, "comment", "author "+author)
	return c, err
}

// UpdateTask applies the patch and writes the audit events. The board argument selects
// which board holds the task - patches cannot move a task across boards.
func (s *Store) UpdateTask(ctx context.Context, board, taskID string, patch KanbanPatch) (KanbanTask, error) {
	bid, err := s.boardID(board)
	if err != nil {
		return KanbanTask{}, err
	}
	ws, err := s.beginWrite(ctx)
	if err != nil {
		return KanbanTask{}, err
	}
	defer ws.revert()
	tx := ws.tx

	t, err := getTaskTx(ctx, tx, bid, taskID)
	if err != nil {
		return KanbanTask{}, err
	}

	enteredDone := false
	events := [][2]string{}
	edits := []string{}

	if patch.Status != "" {
		if !IsStatus(patch.Status) {
			return KanbanTask{}, fmt.Errorf("illegal status %q", patch.Status)
		}
		if patch.Status != t.Status {
			detail := t.Status + " -> " + patch.Status
			if patch.Status == StatusRunning && t.StartedAt == "" {
				t.StartedAt = now()
			}
			// done stamps completion once; leaving a settled status
			// (reopening) clears the stamp; archived keeps whatever
			// exists - archiving a never-done task leaves it unset
			if patch.Status == StatusDone && t.CompletedAt == "" {
				t.CompletedAt = now()
				enteredDone = true
			}
			if !IsSettled(patch.Status) && t.CompletedAt != "" {
				t.CompletedAt = ""
			}
			t.Status = patch.Status
			// a column change drops the card on top of its new column (the same slot a drag
			// without a target position takes - stale positions from the old column must not
			// leak into the new column's order)
			top, err := topPosition(ctx, tx, bid, patch.Status)
			if err != nil {
				return KanbanTask{}, err
			}
			t.Position = top
			events = append(events, [2]string{"moved", detail})
		}
	}
	if patch.Title != nil && *patch.Title != t.Title {
		if strings.TrimSpace(*patch.Title) == "" {
			return KanbanTask{}, fmt.Errorf("task title cannot be empty")
		}
		t.Title = *patch.Title
		edits = append(edits, "title")
	}
	if patch.Body != nil && *patch.Body != t.Body {
		t.Body = *patch.Body
		edits = append(edits, "body")
	}
	if patch.Priority != nil && *patch.Priority != t.Priority {
		if !ValidPriority(*patch.Priority) {
			return KanbanTask{}, fmt.Errorf("illegal priority %d", *patch.Priority)
		}
		t.Priority = *patch.Priority
		edits = append(edits, fmt.Sprintf("priority %d", t.Priority))
	}
	if patch.Assignee != nil && *patch.Assignee != t.Assignee {
		t.Assignee = *patch.Assignee
		if t.Assignee == "" {
			events = append(events, [2]string{"assigned", "unassigned"})
		} else {
			events = append(events, [2]string{"assigned", t.Assignee})
		}
	}
	if patch.SessionID != nil && *patch.SessionID != t.SessionID {
		t.SessionID = *patch.SessionID
		events = append(events, [2]string{"linked", t.SessionID})
	}
	if patch.Source != nil && *patch.Source != t.Source {
		t.Source = *patch.Source
		events = append(events, [2]string{"sourced", t.Source})
	}
	if len(edits) > 0 {
		events = append(events, [2]string{"edited", strings.Join(edits, ", ")})
	}

	if _, err := tx.ExecContext(ctx, `
UPDATE tasks SET title=?, body=?, assignee=?, status=?, session_id=?, source=?, priority=?,
	started_at=?, completed_at=?, position=? WHERE id=? AND board_id=?`,
		t.Title, t.Body, t.Assignee, t.Status, t.SessionID, t.Source, t.Priority,
		t.StartedAt, t.CompletedAt, t.Position, t.ID, bid); err != nil {
		return KanbanTask{}, err
	}
	for _, e := range events {
		if err := recordEvent(ctx, tx, bid, taskID, e[0], e[1]); err != nil {
			return KanbanTask{}, err
		}
	}
	if err := ws.commit(); err != nil {
		return KanbanTask{}, err
	}
	if enteredDone {
		s.emitDone(taskID)
	}
	return t, nil
}

// DeleteTask implements TaskDeleter.
func (s *Store) DeleteTask(ctx context.Context, board, taskID string) error {
	bid, err := s.boardID(board)
	if err != nil {
		return err
	}
	if _, err := s.GetTask(ctx, board, taskID); err != nil {
		return err
	}
	ws, err := s.beginWrite(ctx)
	if err != nil {
		return err
	}
	defer ws.revert()
	tx := ws.tx
	for _, q := range []string{
		`DELETE FROM task_comments WHERE task_id = ? AND board_id = ?`,
		`DELETE FROM task_events WHERE task_id = ? AND board_id = ?`,
		`DELETE FROM tasks WHERE id = ? AND board_id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, q, taskID, bid); err != nil {
			return err
		}
	}
	return ws.commit()
}

// ReorderTask places a task at an explicit slot on the named board. afterID must sit in
// the target column; afterID naming the task itself resolves to the top slot.
func (s *Store) ReorderTask(ctx context.Context, board, taskID, status, afterID string) (KanbanTask, error) {
	if status != "" && !IsStatus(status) {
		return KanbanTask{}, fmt.Errorf("illegal status %q", status)
	}
	bid, err := s.boardID(board)
	if err != nil {
		return KanbanTask{}, err
	}
	t, err := s.GetTask(ctx, board, taskID)
	if err != nil {
		return KanbanTask{}, err
	}
	target := status
	if target == "" {
		target = t.Status
	}
	if target != t.Status {
		t, err = s.UpdateTask(ctx, board, taskID, KanbanPatch{Status: target})
		if err != nil {
			return KanbanTask{}, err
		}
	}

	ws, err := s.beginWrite(ctx)
	if err != nil {
		return KanbanTask{}, err
	}
	defer ws.revert()
	tx := ws.tx
	rows, err := tx.QueryContext(ctx, `
SELECT id FROM tasks WHERE board_id = ? AND status = ?
ORDER BY position ASC, created_at DESC, id DESC`, bid, target)
	if err != nil {
		return KanbanTask{}, err
	}
	all := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return KanbanTask{}, err
		}
		all = append(all, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return KanbanTask{}, err
	}
	rows.Close()

	others := make([]string, 0, len(all))
	for _, id := range all {
		if id != taskID {
			others = append(others, id)
		}
	}
	oldIndex := -1
	for i, id := range all {
		if id == taskID {
			oldIndex = i
			break
		}
	}
	at := 0
	if afterID != "" && afterID != taskID {
		at = -1
		for i, id := range others {
			if id == afterID {
				at = i + 1
				break
			}
		}
		if at < 0 {
			return KanbanTask{}, fmt.Errorf("after task %q not in column %q", afterID, target)
		}
	}
	// a drop that leaves the rendered order unchanged writes nothing: no resequencing, no
	// audit event (the list above is already exactly the order the slot implies)
	if at == oldIndex {
		if err := ws.commit(); err != nil {
			return KanbanTask{}, err
		}
		return t, nil
	}
	for i, id := range others {
		p := int64(i)
		if i >= at {
			p++
		}
		if _, err := tx.ExecContext(ctx, `UPDATE tasks SET position = ? WHERE id = ? AND board_id = ?`, p, id, bid); err != nil {
			return KanbanTask{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE tasks SET position = ? WHERE id = ? AND board_id = ?`, at, taskID, bid); err != nil {
		return KanbanTask{}, err
	}
	detail := "top"
	if afterID != "" && afterID != taskID {
		detail = "after " + afterID
	}
	if err := recordEvent(ctx, tx, bid, taskID, "reordered", detail); err != nil {
		return KanbanTask{}, err
	}
	if err := ws.commit(); err != nil {
		return KanbanTask{}, err
	}
	return s.GetTask(ctx, board, taskID)
}
