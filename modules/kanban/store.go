package kanban

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/modules/datadir"
)

// ErrNotFound is returned when a task does not exist (the agent seam's
// shared sentinel, aliased here so store callers stay short).
var ErrNotFound = ErrTaskNotFound

// Store implements KanbanProvider over SQLite. Safe for
// concurrent use via *sql.DB (WAL + busy_timeout, same recipe as the
// session store).
//
// ONE DATABASE (t_feae19d3): db is the single <home>/data/kanban.db -
// every board is a row in its boards table and every card, comment and
// event carries a board_id FK. There is no per-board handle and no
// pointer file; a board argument is resolved to its row id at the door.
type Store struct {
	db *sql.DB

	// notifier is the finished-things seam (card t_5d9032d3), wired by
	// the kanban plugin after MountAllAPI order places notifications
	// first. Nil-safe: a store without the seam just doesn't notify.
	notifier core.NotificationsProvider
}

// SetNotifier wires the finished-things seam (the plugin calls this
// post-mount; exposed for tests to inject a recorder).
func (s *Store) SetNotifier(n core.NotificationsProvider) {
	s.notifier = n
}

// emitDone records one task notification when a task ENTERED done in
// this update. Best-effort: a notification failure never fails the
// board write (the caller swallows the error). Title is metadata only.
func (s *Store) emitDone(taskID string) {
	if s.notifier == nil {
		return
	}
	_, _ = s.notifier.Emit(core.Notification{
		Kind:     "task",
		SourceID: taskID,
		Title:    "task " + taskID + " done",
		Verdict:  "ok",
	})
}

// Compile-time assertions: *Store implements the kanban seam plus the
// optional board-registry capability.
var (
	_ KanbanProvider = (*Store)(nil)
	_ BoardLister    = (*Store)(nil)
)

// Open opens (creating if needed) the board database at dsn and
// ensures the schema exists. ":memory:" works for tests. A dsn inside
// a data/ directory gets the boot-time data-dir migration first
// (legacy home-root databases move into data/ before the handle is
// opened - see modules/datadir).
func Open(dsn string) (*Store, error) {
	if err := datadir.Migrate(dsn); err != nil {
		return nil, err
	}
	db, err := openDB(dsn)
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// openDB is the dsn-widening + sql.Open step, shared by Open and the
// migration's short-lived source handles.
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

// migrate ensures the schema on the single database. Fresh schemas
// carry the full multi-board shape (boards table + board_id FKs); a
// database from the legacy shape is rebuilt in place - new tables with
// board_id, the rows copied with main's board id backfilled.
// boardIndexes is the board_id index set. It runs AFTER the board
// tables exist - on a legacy database, board_id only arrives with
// rebuildLegacyRows, so migrate() creates the tables (IF NOT EXISTS,
// no board_id index touching), upgrades the legacy rows, and only then
// lays the board indexes.
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
	_, err := db.Exec(boardIndexes)
	return err
}

// taskTables is the three card tables in the boards-aware shape (fresh
// databases are born with board_id; legacy databases rebuild below).
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
	board_id     INTEGER NOT NULL REFERENCES boards(id) ON DELETE CASCADE
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

// rebuildLegacyRows upgrades a database from the boardless legacy
// shape: tasks/task_comments/task_events without a board_id column.
// The rows are rebuilt into new boards-aware tables in one transaction
// (all legacy rows belong to "main" - the boards table's seeded row is
// the backfill source), the old tables drop, the new ones take their
// places and the indexes are re-created. A database that already has
// board_id columns is a no-op. The table re-creation must exist in the
// same file because sqlite has no ALTER TABLE ADD CONSTRAINT.
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

// tableMissingColumn reports whether table lacks column (false for a
// table that does not exist at all - the CREATE above then makes it).
func tableMissingColumn(db *sql.DB, table, column string) bool {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return false
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var (
			cid       int
			name      string
			ctype     string
			notNull   int
			dflt      any
			pk        int
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

// CreateTask inserts a task on the named board ("" = the current
// board). ID, Status (default triage) and CreatedAt are filled by the
// store; title must be non-empty.
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
	bid, err := s.boardID(board)
	if err != nil {
		return KanbanTask{}, err
	}
	for range 5 {
		in.ID = newTaskID()
		in.Status = status
		in.CreatedAt = now()
		_, err := s.db.ExecContext(ctx, `
INSERT INTO tasks (id, title, body, assignee, status, session_id, source, created_at, started_at, completed_at, board_id)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			in.ID, in.Title, in.Body, in.Assignee, in.Status,
			in.SessionID, in.Source, in.CreatedAt, in.StartedAt, in.CompletedAt, bid)
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				continue // id collision: retry with a fresh draw
			}
			return KanbanTask{}, err
		}
		if err := recordEvent(ctx, s.db, bid, in.ID, "created", describeNew(in)); err != nil {
			return KanbanTask{}, err
		}
		return in, nil
	}
	return KanbanTask{}, fmt.Errorf("could not allocate a task id after 5 tries")
}

// describeNew renders the created-event detail line.
func describeNew(t KanbanTask) string {
	detail := "status " + t.Status
	if t.SessionID != "" {
		detail += ", session " + t.SessionID
	}
	if t.Source != "" {
		detail += ", source " + t.Source
	}
	return detail
}

// ListTasks returns tasks on the named board, newest first. Empty
// status lists every task. The board filter is ALWAYS applied - the
// board_id column carries the isolation the per-board files used to.
func (s *Store) ListTasks(ctx context.Context, board, status string) ([]KanbanTask, error) {
	bid, err := s.boardID(board)
	if err != nil {
		return nil, err
	}
	query := `SELECT id, title, body, assignee, status, session_id, source, created_at, started_at, completed_at FROM tasks WHERE board_id = ?`
	args := []any{bid}
	if status != "" {
		if !IsStatus(status) {
			return nil, fmt.Errorf("illegal status %q", status)
		}
		query += " AND status = ?"
		args = append(args, status)
	}
	query += " ORDER BY created_at DESC, id DESC"
	rows, err := s.db.QueryContext(ctx, query, args...)
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

func scanTask(row interface{ Scan(...any) error }) (*KanbanTask, error) {
	var t KanbanTask
	err := row.Scan(&t.ID, &t.Title, &t.Body, &t.Assignee, &t.Status,
		&t.SessionID, &t.Source, &t.CreatedAt, &t.StartedAt, &t.CompletedAt)
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
	row := s.db.QueryRowContext(ctx, `SELECT id, title, body, assignee, status, session_id, source, created_at, started_at, completed_at FROM tasks WHERE id = ? AND board_id = ?`, id, bid)
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
	rows, err := s.db.QueryContext(ctx, `
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
	rows, err := s.db.QueryContext(ctx, `
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
	res, err := s.db.ExecContext(ctx, `
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
	err = recordEvent(ctx, s.db, bid, taskID, "comment", "author "+author)
	return c, err
}

// UpdateTask applies the patch and writes the audit events. Any status
// may move to any other (legal) status; moving INTO done stamps
// completed_at, moving OUT of a settled status clears it; moving INTO
// running stamps started_at once. The board argument selects which
// board holds the task - patches cannot move a task across boards.
func (s *Store) UpdateTask(ctx context.Context, board, taskID string, patch KanbanPatch) (KanbanTask, error) {
	bid, err := s.boardID(board)
	if err != nil {
		return KanbanTask{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return KanbanTask{}, err
	}
	defer tx.Rollback()

	t, err := getTaskTx(ctx, tx, bid, taskID)
	if err != nil {
		return KanbanTask{}, err
	}

	// card t_5d9032d3: a task that ENTERS done this update emits one
	// finished-things record (fired after the commit below).
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
UPDATE tasks SET title=?, body=?, assignee=?, status=?, session_id=?, source=?,
	started_at=?, completed_at=? WHERE id=? AND board_id=?`,
		t.Title, t.Body, t.Assignee, t.Status, t.SessionID, t.Source,
		t.StartedAt, t.CompletedAt, t.ID, bid); err != nil {
		return KanbanTask{}, err
	}
	for _, e := range events {
		if err := recordEvent(ctx, tx, bid, taskID, e[0], e[1]); err != nil {
			return KanbanTask{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return KanbanTask{}, err
	}
	if enteredDone {
		s.emitDone(taskID)
	}
	return t, nil
}