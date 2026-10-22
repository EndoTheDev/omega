// Package notifications is the finished-things store (card
// t_5d9032d3): one small SQLite record per finished thing - a kanban
// task completed, a cron fire finished, a dispatched chat run ended -
// served to the webui bell (badge, dropdown panel, live toasts) and
// the TUI /notifications command.
//
// Records are metadata only: source kind (task | cron | chat), source
// id, a fixed title line, the verdict (ok | failed) and the read flag.
// Prompt and session content never enter a notification (privacy law).
// Retention keeps the 200 newest records, oldest pruned on insert.
package notifications

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/modules/datadir"
)

// retention is the record cap: inserts beyond it prune the oldest.
const retention = 200

// subscriberBuf is the per-subscriber channel buffer. A full buffer
// drops the frame (best-effort delivery) instead of blocking Emit -
// a wedged SSE client must never stall a task completion.
const subscriberBuf = 8

// Store implements agent.NotificationsProvider over SQLite. Safe for
// concurrent use via *sql.DB (WAL + busy_timeout, the board/session
// recipe). Live delivery fans out through a mutexed subscriber set.
type Store struct {
	db *sql.DB

	mu    sync.Mutex
	subs  map[int64]chan agent.Notification
	subID int64
}

// Compile-time assertion: *Store implements the notifications seam.
var _ agent.NotificationsProvider = (*Store)(nil)

// Open opens (creating if needed) the database at dsn and ensures the
// schema. ":memory:" works for tests. A dsn inside a data/ directory
// gets the boot-time data-dir migration first, same as the board.
func Open(dsn string) (*Store, error) {
	if err := datadir.Migrate(dsn); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dsnParams(dsn))
	if err != nil {
		return nil, err
	}
	// An in-memory database lives per connection: a pooled second
	// connection would see an empty database (the session store's
	// recipe). One connection keeps :memory: usable under pooling; a
	// file DSN keeps the full connection pool.
	if dsn == ":memory:" {
		db.SetMaxOpenConns(1)
	}
	s := &Store{db: db, subs: map[int64]chan agent.Notification{}}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// dsnParams widens a bare file dsn with the WAL + busy_timeout
// parameters (the same recipe as the board store).
func dsnParams(dsn string) string {
	if !strings.Contains(dsn, "?") {
		return dsn + "?_journal_mode=WAL&_busy_timeout=5000"
	}
	if !strings.Contains(dsn, "_journal_mode=") {
		return dsn + "&_journal_mode=WAL&_busy_timeout=5000"
	}
	return dsn
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS notifications (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	kind       TEXT NOT NULL,
	source_id  TEXT NOT NULL DEFAULT '',
	title      TEXT NOT NULL DEFAULT '',
	verdict    TEXT NOT NULL DEFAULT 'ok',
	created_at TEXT NOT NULL,
	read       INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_notifications_read ON notifications(read, id);
`)
	return err
}

// Close releases the handle; subscribers are dropped first so nothing
// writes into closed channels.
func (s *Store) Close() error {
	s.mu.Lock()
	for id, ch := range s.subs {
		close(ch)
		delete(s.subs, id)
	}
	s.mu.Unlock()
	return s.db.Close()
}

// Emit inserts one record, prunes beyond retention, and broadcasts the
// stored record to subscribers. Kind must be task, cron or chat;
// verdict "" normalizes to ok, "failed" stays, anything else errors.
func (s *Store) Emit(n agent.Notification) (agent.Notification, error) {
	switch n.Kind {
	case "task", "cron", "chat":
	default:
		return agent.Notification{}, fmt.Errorf("notification kind must be task, cron or chat, got %q", n.Kind)
	}
	switch n.Verdict {
	case "":
		n.Verdict = "ok"
	case "ok", "failed":
	default:
		return agent.Notification{}, fmt.Errorf("notification verdict must be ok or failed, got %q", n.Verdict)
	}
	if strings.TrimSpace(n.SourceID) == "" {
		return agent.Notification{}, fmt.Errorf("notification source_id is required")
	}
	n.CreatedAt = now()
	res, err := s.db.Exec(`INSERT INTO notifications (kind, source_id, title, verdict, created_at, read)
VALUES (?, ?, ?, ?, ?, 0)`, n.Kind, n.SourceID, n.Title, n.Verdict, n.CreatedAt)
	if err != nil {
		return agent.Notification{}, err
	}
	n.ID, err = res.LastInsertId()
	if err != nil {
		return agent.Notification{}, err
	}
	// Prune beyond retention: keep the retention-row-newest set. The
	// boundary is the retention-th newest row's id (ids only ever
	// grow, so id order is time order even across pruned gaps).
	if _, err := s.db.Exec(`DELETE FROM notifications WHERE id < (
SELECT id FROM notifications ORDER BY id DESC LIMIT 1 OFFSET ?)`, retention-1); err != nil {
		return agent.Notification{}, fmt.Errorf("prune: %w", err)
	}
	s.broadcast(n)
	return n, nil
}

// broadcast fans one record out to every subscriber. Non-blocking: a
// full buffer drops the frame for that subscriber only.
func (s *Store) broadcast(n agent.Notification) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ch := range s.subs {
		select {
		case ch <- n:
		default:
		}
	}
}

// Subscribe registers a live channel of emitted records (webui SSE
// toasts). Cancel removes the subscription and closes the channel;
// idempotent, safe to call from a deferred or SSE-close path.
func (s *Store) Subscribe() (<-chan agent.Notification, func()) {
	ch := make(chan agent.Notification, subscriberBuf)
	s.mu.Lock()
	s.subID++
	id := s.subID
	s.subs[id] = ch
	s.mu.Unlock()
	var once sync.Once
	cancel := func() {
		once.Do(func() {
			s.mu.Lock()
			if _, ok := s.subs[id]; ok {
				delete(s.subs, id)
				close(ch)
			}
			s.mu.Unlock()
		})
	}
	return ch, cancel
}

// List returns records newest-first. unreadOnly filters to unread
// rows; limit <= 0 takes the server default (50); offset pages deeper.
// unread counts over the whole set, not the page.
func (s *Store) List(ctx context.Context, unreadOnly bool, limit, offset int) ([]agent.Notification, int, error) {
	unread := 0
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications WHERE read = 0`).Scan(&unread); err != nil {
		return nil, 0, err
	}
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	query := `SELECT id, kind, source_id, title, verdict, created_at, read FROM notifications`
	if unreadOnly {
		query += ` WHERE read = 0`
	}
	query += ` ORDER BY id DESC LIMIT ? OFFSET ?`
	rows, err := s.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []agent.Notification{}
	for rows.Next() {
		var n agent.Notification
		var read int
		if err := rows.Scan(&n.ID, &n.Kind, &n.SourceID, &n.Title, &n.Verdict, &n.CreatedAt, &read); err != nil {
			return nil, 0, err
		}
		n.Read = read != 0
		out = append(out, n)
	}
	return out, unread, rows.Err()
}

// MarkRead flags one record read. An unknown id errors.
func (s *Store) MarkRead(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE notifications SET read = 1 WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("no such notification %d", id)
	}
	return nil
}

// MarkAllRead flags every record read.
func (s *Store) MarkAllRead(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE notifications SET read = 1`)
	return err
}

// now is the UTC RFC3339 timestamp stamp (the module family's form).
func now() string {
	return time.Now().UTC().Format(time.RFC3339)
}