package kanban

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	_ "modernc.org/sqlite"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/modules/datadir"
)

// ErrNotFound is returned when a task does not exist (the agent seam's
// shared sentinel, aliased here so store callers stay short).
var ErrNotFound = agent.ErrTaskNotFound

// Store implements agent.KanbanProvider over SQLite. Safe for
// concurrent use via *sql.DB (WAL + busy_timeout, same recipe as the
// session store).
//
// Multi-board: one handle per board. db is the DEFAULT board ("" calls
// land here - the pointer target resolved at mount); every other board
// opens lazily from boardsDir/<name>/kanban.db and caches in named.
// ConfigureBoards wires the registry when the host knows a home.
type Store struct {
	// db is the default board's handle (pointer-resolved at mount).
	db *sql.DB

	// board registry wiring (all optional; empty = single-board mode
	// where any named board errors).
	boardsDir  string
	defaultName string // pointer target at mount ("" = unknown)
	legacyDSN  string  // the main board's real path

	mu    sync.Mutex
	named map[string]*sql.DB

	// notifier is the finished-things seam (card t_5d9032d3), wired by
	// the kanban plugin after MountAll order places notifications
	// first. Nil-safe: a store without the seam just doesn't notify.
	notifier agent.NotificationsProvider
}

// SetNotifier wires the finished-things seam (the plugin calls this
// post-mount; exposed for tests to inject a recorder).
func (s *Store) SetNotifier(n agent.NotificationsProvider) {
	s.notifier = n
}

// emitDone records one task notification when a task ENTERED done in
// this update. Best-effort: a notification failure never fails the
// board write (the caller swallows the error). Title is metadata only.
func (s *Store) emitDone(taskID string) {
	if s.notifier == nil {
		return
	}
	_, _ = s.notifier.Emit(agent.Notification{
		Kind:     "task",
		SourceID: taskID,
		Title:    "task " + taskID + " done",
		Verdict:  "ok",
	})
}

// Compile-time assertions: *Store implements the kanban seam plus the
// optional board-registry capability.
var (
	_ agent.KanbanProvider = (*Store)(nil)
	_ agent.BoardLister    = (*Store)(nil)
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
// lazy per-board open.
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

// ConfigureBoards wires the multi-board registry. Called by the plugin
// Mount with the home-derived values; tests may call it directly. The
// default board ("" board arg) is the handle Open made; defaultName
// names it for Boards(), legacyDSN lets "main" resolve even when the
// pointer points elsewhere.
func (s *Store) ConfigureBoards(boardsDir, defaultName, legacyDSN string) {
	s.boardsDir = boardsDir
	s.defaultName = defaultName
	s.legacyDSN = legacyDSN
	s.mu.Lock()
	s.named = map[string]*sql.DB{}
	s.mu.Unlock()
}

// handleFor resolves a board name to its store handle. "" and the
// mount-time pointer target land on the default handle; "main" opens
// the legacy dsn when the pointer points elsewhere; other valid names
// open lazily from the registry directory. Unknown or illegal boards
// error at the door - the store never auto-creates a named board.
func (s *Store) handleFor(board string) (*sql.DB, error) {
	if board == "" || board == s.defaultName {
		return s.db, nil
	}
	if !ValidBoardName(board) {
		return nil, fmt.Errorf("illegal board name %q", board)
	}
	s.mu.Lock()
	if s.named == nil {
		s.named = map[string]*sql.DB{}
	}
	if db, ok := s.named[board]; ok {
		s.mu.Unlock()
		return db, nil
	}
	s.mu.Unlock()

	var dsn string
	switch {
	case board == DefaultBoardName:
		if s.legacyDSN == "" {
			return nil, fmt.Errorf("board %q is not available", board)
		}
		dsn = s.legacyDSN
	case s.boardsDir == "":
		return nil, fmt.Errorf("named boards not configured")
	default:
		dsn = filepath.Join(s.boardsDir, board, "kanban.db")
		if _, err := os.Stat(dsn); err != nil {
			return nil, fmt.Errorf("no such board %q", board)
		}
	}

	db, err := openDB(dsn)
	if err != nil {
		return nil, err
	}
	if err := s.migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	s.mu.Lock()
	if s.named == nil {
		s.named = map[string]*sql.DB{}
	}
	if existing, ok := s.named[board]; ok { // raced to the same draw
		s.mu.Unlock()
		db.Close()
		return existing, nil
	}
	s.named[board] = db
	s.mu.Unlock()
	return db, nil
}

// Boards implements agent.BoardLister: the default board first, then
// the registry directories that hold a kanban.db.
func (s *Store) Boards(ctx context.Context) ([]agent.BoardInfo, error) {
	out := []agent.BoardInfo{{Name: DefaultBoardName, Current: s.currentName() == DefaultBoardName || s.defaultName == ""}}
	if s.boardsDir == "" {
		return out, nil
	}
	entries, err := os.ReadDir(s.boardsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() || e.Name() == DefaultBoardName {
			continue
		}
		if _, err := os.Stat(filepath.Join(s.boardsDir, e.Name(), "kanban.db")); err != nil {
			continue
		}
		out = append(out, agent.BoardInfo{Name: e.Name(), Current: e.Name() == s.defaultName})
	}
	return out, nil
}

// currentName reports the pointer name the default handle represents
// (empty when the host never wired a registry).
func (s *Store) currentName() string {
	if s.defaultName == "" {
		return DefaultBoardName
	}
	return s.defaultName
}

// Close releases the database handles.
func (s *Store) Close() error {
	s.mu.Lock()
	named := make([]*sql.DB, 0, len(s.named))
	for _, db := range s.named {
		named = append(named, db)
	}
	s.named = nil
	s.mu.Unlock()
	var firstErr error
	for _, db := range named {
		if err := db.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if err := s.db.Close(); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

// migrate ensures the schema on any board's handle. Task, comment and
// event tables are identical on every board (isolation is by handle,
// not by column).
func (s *Store) migrate(db *sql.DB) error {
	_, err := db.Exec(`
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
	completed_at TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
CREATE TABLE IF NOT EXISTS task_comments (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	task_id    TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
	author     TEXT NOT NULL,
	body       TEXT NOT NULL,
	created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_task_comments_task ON task_comments(task_id, id);
CREATE TABLE IF NOT EXISTS task_events (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	task_id    TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
	kind       TEXT NOT NULL,
	detail     TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_task_events_task ON task_events(task_id, id);
`)
	return err
}

// CreateTask inserts a task on the named board ("" = default). ID,
// Status (default triage) and CreatedAt are filled by the store;
// title must be non-empty.
func (s *Store) CreateTask(ctx context.Context, board string, in agent.KanbanTask) (agent.KanbanTask, error) {
	if strings.TrimSpace(in.Title) == "" {
		return agent.KanbanTask{}, fmt.Errorf("task title is required")
	}
	status := in.Status
	if status == "" {
		status = StatusTriage
	}
	if !IsStatus(status) {
		return agent.KanbanTask{}, fmt.Errorf("illegal status %q", status)
	}
	db, err := s.handleFor(board)
	if err != nil {
		return agent.KanbanTask{}, err
	}
	for range 5 {
		in.ID = newTaskID()
		in.Status = status
		in.CreatedAt = now()
		_, err := db.ExecContext(ctx, `
INSERT INTO tasks (id, title, body, assignee, status, session_id, source, created_at, started_at, completed_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			in.ID, in.Title, in.Body, in.Assignee, in.Status,
			in.SessionID, in.Source, in.CreatedAt, in.StartedAt, in.CompletedAt)
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				continue // id collision: retry with a fresh draw
			}
			return agent.KanbanTask{}, err
		}
		if err := recordEvent(ctx, db, in.ID, "created", describeNew(in)); err != nil {
			return agent.KanbanTask{}, err
		}
		return in, nil
	}
	return agent.KanbanTask{}, fmt.Errorf("could not allocate a task id after 5 tries")
}

// describeNew renders the created-event detail line.
func describeNew(t agent.KanbanTask) string {
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
// status lists every task.
func (s *Store) ListTasks(ctx context.Context, board, status string) ([]agent.KanbanTask, error) {
	db, err := s.handleFor(board)
	if err != nil {
		return nil, err
	}
	query := `SELECT id, title, body, assignee, status, session_id, source, created_at, started_at, completed_at FROM tasks`
	args := []any{}
	if status != "" {
		if !IsStatus(status) {
			return nil, fmt.Errorf("illegal status %q", status)
		}
		query += " WHERE status = ?"
		args = append(args, status)
	}
	query += " ORDER BY created_at DESC, id DESC"
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []agent.KanbanTask{}
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

func scanTask(row interface{ Scan(...any) error }) (*agent.KanbanTask, error) {
	var t agent.KanbanTask
	err := row.Scan(&t.ID, &t.Title, &t.Body, &t.Assignee, &t.Status,
		&t.SessionID, &t.Source, &t.CreatedAt, &t.StartedAt, &t.CompletedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// GetTask returns one task by id on the named board.
func (s *Store) GetTask(ctx context.Context, board, id string) (agent.KanbanTask, error) {
	db, err := s.handleFor(board)
	if err != nil {
		return agent.KanbanTask{}, err
	}
	row := db.QueryRowContext(ctx, `SELECT id, title, body, assignee, status, session_id, source, created_at, started_at, completed_at FROM tasks WHERE id = ?`, id)
	t, err := scanTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return agent.KanbanTask{}, ErrNotFound
	}
	if err != nil {
		return agent.KanbanTask{}, err
	}
	return *t, nil
}

// GetTaskComments returns the comment thread, oldest first.
func (s *Store) GetTaskComments(ctx context.Context, board, taskID string) ([]agent.KanbanComment, error) {
	db, err := s.handleFor(board)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `
SELECT id, task_id, author, body, created_at FROM task_comments WHERE task_id = ? ORDER BY id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []agent.KanbanComment{}
	for rows.Next() {
		var c agent.KanbanComment
		if err := rows.Scan(&c.ID, &c.TaskID, &c.Author, &c.Body, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetTaskEvents returns the audit trail, oldest first.
func (s *Store) GetTaskEvents(ctx context.Context, board, taskID string) ([]agent.KanbanEvent, error) {
	db, err := s.handleFor(board)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `
SELECT id, task_id, kind, detail, created_at FROM task_events WHERE task_id = ? ORDER BY id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []agent.KanbanEvent{}
	for rows.Next() {
		var e agent.KanbanEvent
		if err := rows.Scan(&e.ID, &e.TaskID, &e.Kind, &e.Detail, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// AddComment appends to the thread and writes the comment event.
func (s *Store) AddComment(ctx context.Context, board, taskID, author, body string) (agent.KanbanComment, error) {
	if strings.TrimSpace(body) == "" {
		return agent.KanbanComment{}, fmt.Errorf("comment body is required")
	}
	if author == "" {
		author = "user"
	}
	db, err := s.handleFor(board)
	if err != nil {
		return agent.KanbanComment{}, err
	}
	// the FK needs a live task; GetTask also answers ErrNotFound cleanly
	if _, err := s.GetTask(ctx, board, taskID); err != nil {
		return agent.KanbanComment{}, err
	}
	res, err := db.ExecContext(ctx, `
INSERT INTO task_comments (task_id, author, body, created_at) VALUES (?, ?, ?, ?)`,
		taskID, author, body, now())
	if err != nil {
		return agent.KanbanComment{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return agent.KanbanComment{}, err
	}
	c := agent.KanbanComment{ID: id, TaskID: taskID, Author: author, Body: body, CreatedAt: now()}
	err = recordEvent(ctx, db, taskID, "comment", "author "+author)
	return c, err
}

// UpdateTask applies the patch and writes the audit events. Any status
// may move to any other (legal) status; moving INTO done stamps
// completed_at, moving OUT of a settled status clears it; moving INTO
// running stamps started_at once. The board argument selects which
// board holds the task - patches cannot move a task across boards.
func (s *Store) UpdateTask(ctx context.Context, board, taskID string, patch agent.KanbanPatch) (agent.KanbanTask, error) {
	db, err := s.handleFor(board)
	if err != nil {
		return agent.KanbanTask{}, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return agent.KanbanTask{}, err
	}
	defer tx.Rollback()

	t, err := getTaskTx(ctx, tx, taskID)
	if err != nil {
		return agent.KanbanTask{}, err
	}

	// card t_5d9032d3: a task that ENTERS done this update emits one
	// finished-things record (fired after the commit below).
	enteredDone := false
	events := [][2]string{}
	edits := []string{}

	if patch.Status != "" {
		if !IsStatus(patch.Status) {
			return agent.KanbanTask{}, fmt.Errorf("illegal status %q", patch.Status)
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
			return agent.KanbanTask{}, fmt.Errorf("task title cannot be empty")
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
	started_at=?, completed_at=? WHERE id=?`,
		t.Title, t.Body, t.Assignee, t.Status, t.SessionID, t.Source,
		t.StartedAt, t.CompletedAt, t.ID); err != nil {
		return agent.KanbanTask{}, err
	}
	for _, e := range events {
		if err := recordEventTx(ctx, tx, taskID, e[0], e[1]); err != nil {
			return agent.KanbanTask{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return agent.KanbanTask{}, err
	}
	if enteredDone {
		s.emitDone(taskID)
	}
	return t, nil
}