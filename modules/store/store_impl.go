package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/modules/datadir"
)

// ErrNotFound is returned when a session does not exist.
var ErrNotFound = errors.New("session not found")

// Compile-time assertion that Store implements core.StoreProvider.
var _ core.StoreProvider = (*Store)(nil)

// Store persists sessions and its message history in SQLite.
// It is safe for concurrent use via *sql.DB.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the SQLite database at dsn and ensures
// the schema exists. Use ":memory:" for tests. A dsn inside a data/
// directory gets the boot-time data-dir migration first (legacy
// home-root databases move into data/ before the handle is opened).
func Open(dsn string) (*Store, error) {
	return openStore(dsn)
}

func (s *Store) Open(dsn string) error {
	newStore, err := openStore(dsn)
	if err != nil {
		return err
	}
	*s = *newStore
	return nil
}

func openStore(dsn string) (*Store, error) {
	// Data-dir migration runs before the handle opens: a legacy
	// home-root db (with its -wal/-shm sidecars) is moved into data/
	// first - never copy-and-leave (Endo 2026-10-20). ":memory:" and
	// non-data dsns pass through untouched.
	if err := datadir.Migrate(dsn); err != nil {
		return nil, err
	}
	// WAL mode allows concurrent readers + one writer across multiple
	// processes (e.g. parent + subagents). busy_timeout retries on lock
	// instead of returning SQLITE_BUSY immediately.
	//
	// foreign_keys rides the DSN, not a post-open PRAGMA: a SQLite
	// pragma is per-connection, so a one-shot `PRAGMA foreign_keys = ON`
	// covered a single connection out of the pool and left the rest (the
	// majority under concurrent writers) with enforcement off - orphaned
	// message rows in, missed ON DELETE CASCADE out. A DSN pragma is
	// applied by the driver at every connect.
	const pragmaFK = "_pragma=foreign_keys(1)"
	dsnParams := dsn
	switch {
	case !strings.Contains(dsn, "?"):
		dsnParams += "?" + pragmaFK + "&_journal_mode=WAL&_busy_timeout=5000"
	case strings.Contains(dsn, "_pragma=foreign_keys"):
		// the caller's DSN already carries the FK pragma
	case strings.Contains(dsn, "_journal_mode="):
		dsnParams += "&" + pragmaFK
	default:
		dsnParams += "&" + pragmaFK + "&_journal_mode=WAL&_busy_timeout=5000"
	}
	db, err := sql.Open("sqlite", dsnParams)
	if err != nil {
		return nil, err
	}
	// An in-memory database lives per connection: a pooled second
	// connection would see an empty database (migrations and data
	// gone). One connection keeps :memory: usable under pooling.
	// ponytail: serializes access on in-memory DBs only - file DSNs
	// keep the full connection pool.
	if dsn == ":memory:" {
		db.SetMaxOpenConns(1)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the underlying database.
func (s *Store) Close() error {
	return s.db.Close()
}

// migrate creates the schema if it does not exist.
func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS sessions (
	id         TEXT PRIMARY KEY,
	parent_id  TEXT REFERENCES sessions(id) ON DELETE CASCADE,
	label      TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	prompt_tokens INTEGER NOT NULL DEFAULT 0,
	completion_tokens INTEGER NOT NULL DEFAULT 0,
	pinned INTEGER NOT NULL DEFAULT 0,
	archived INTEGER NOT NULL DEFAULT 0,
	title_state INTEGER NOT NULL DEFAULT 0,
	last_read_at TEXT NOT NULL DEFAULT '',
	agent TEXT NOT NULL DEFAULT 'default',
	last_run TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS messages (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
	role       TEXT NOT NULL,
	payload    TEXT NOT NULL,
	created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id, id);

-- FTS5 full-text index over message content for /search.
-- Column name 'payload' matches the messages table column so FTS5
-- external content mode can read it directly.
CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
	session_id UNINDEXED,
	payload,
	content='messages',
	content_rowid='id'
);
-- Triggers keep the FTS index in sync with the messages table.
CREATE TRIGGER IF NOT EXISTS messages_ai AFTER INSERT ON messages BEGIN
	INSERT INTO messages_fts(rowid, session_id, payload) VALUES (new.id, new.session_id, new.payload);
END;
CREATE TRIGGER IF NOT EXISTS messages_ad AFTER DELETE ON messages BEGIN
	INSERT INTO messages_fts(messages_fts, rowid, session_id, payload) VALUES ('delete', old.id, old.session_id, old.payload);
END;
CREATE TRIGGER IF NOT EXISTS messages_au AFTER UPDATE ON messages BEGIN
	INSERT INTO messages_fts(messages_fts, rowid, session_id, payload) VALUES ('delete', old.id, old.session_id, old.payload);
	INSERT INTO messages_fts(rowid, session_id, payload) VALUES (new.id, new.session_id, new.payload);
END;

-- FTS5 index over session labels, the title half of the unified
-- /sessions/search. Standalone (no external content): one row per
-- session, kept in sync by these triggers on INSERT, label UPDATE and
-- DELETE. Labels change in three places (manual rename, auto-title,
-- creation) - trigger coverage beats a hook at each call site.
CREATE VIRTUAL TABLE IF NOT EXISTS sessions_fts USING fts5(
	session_id UNINDEXED,
	label
);
-- Image captions keyed by content hash (vision_model knob). Captions
-- are shared across sessions by design: one row per distinct image.
CREATE TABLE IF NOT EXISTS image_captions (
	image_id   TEXT PRIMARY KEY,
	caption    TEXT NOT NULL,
	created_at TEXT NOT NULL
);
CREATE TRIGGER IF NOT EXISTS sessions_ai AFTER INSERT ON sessions BEGIN
	INSERT INTO sessions_fts(session_id, label) VALUES (new.id, new.label);
END;
CREATE TRIGGER IF NOT EXISTS sessions_au AFTER UPDATE OF label ON sessions BEGIN
	DELETE FROM sessions_fts WHERE session_id = old.id;
	INSERT INTO sessions_fts(session_id, label) VALUES (new.id, new.label);
END;
CREATE TRIGGER IF NOT EXISTS sessions_ad AFTER DELETE ON sessions BEGIN
	DELETE FROM sessions_fts WHERE session_id = old.id;
END;
-- Plain DELETE FROM (not the fts5 'delete' special command): as of
-- SQLite 3.53 that command errors on regular tables - it only works
-- for contentless/external-content indexes (messages_fts uses it there
-- because its content table backs the index). A regular fts5 row
-- deletes through SQL like any table.
`)
	if err != nil {
		return err
	}
	// Rebuild FTS index to pick up any messages that existed before
	// the FTS table was created (triggers only fire on new writes).
	s.db.Exec("INSERT INTO messages_fts(messages_fts) VALUES('rebuild')")
	// Same catch-up for the label index: standalone tables have no
	// 'rebuild', so a full delete-and-reload picks up labels that
	// predate the index (and heals any drift). One row per session,
	// runs once per open - trivially cheap.
	s.db.Exec(`DELETE FROM sessions_fts;
INSERT INTO sessions_fts(session_id, label) SELECT id, label FROM sessions;`)
	// Add columns that may be missing from older databases. SQLite
	// does not support IF NOT EXISTS on ALTER TABLE, so we catch
	// the "duplicate column" error and ignore it.
	for _, stmt := range []string{
		`ALTER TABLE sessions ADD COLUMN parent_id TEXT REFERENCES sessions(id) ON DELETE CASCADE`,
		`ALTER TABLE sessions ADD COLUMN label TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE sessions ADD COLUMN prompt_tokens INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE sessions ADD COLUMN completion_tokens INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE sessions ADD COLUMN pinned INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE sessions ADD COLUMN archived INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE sessions ADD COLUMN title_state INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE sessions ADD COLUMN last_read_at TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE sessions ADD COLUMN agent TEXT NOT NULL DEFAULT 'default'`,
		`ALTER TABLE sessions ADD COLUMN last_run TEXT NOT NULL DEFAULT ''`,
	} {
		if _, err := s.db.Exec(stmt); err != nil {
			// SQLite error for duplicate column: "duplicate column name"
			if !strings.Contains(err.Error(), "duplicate column") {
				return err
			}
			continue
		}
		// The column landed on this open: backfill the data fix that
		// belongs to it. Runs once, exactly when the column is new.
		// Sessions that already carried a label before title_state
		// existed are final (2) - a manual label is never retitled.
		if strings.Contains(stmt, "title_state") {
			if _, err := s.db.Exec(`UPDATE sessions SET title_state = 2 WHERE label != ''`); err != nil {
				return err
			}
		}
		// last_read_at arrives empty for every existing row - stamp it
		// to the row's own updated_at so the whole existing history
		// counts as seen. Only output written AFTER the upgrade turns
		// sessions unread; nothing the user already saw does.
		if strings.Contains(stmt, "last_read_at") {
			if _, err := s.db.Exec(`UPDATE sessions SET last_read_at = updated_at`); err != nil {
				return err
			}
		}
	}
	return nil
}

// CreateSession creates a session with the given id. parentID links it to
// an existing session (empty for a root); label is an optional name. It
// returns an error if a session with that id already exists or the parent
// does not.
func (s *Store) CreateSession(ctx context.Context, id, parentID, label string) error {
	now := core.NowISO()
	// An empty parentID is stored as NULL so the FK constraint does not
	// try to match a session with id "".
	var parent any
	if parentID != "" {
		parent = parentID
	}
	// last_read_at = created time: new sessions are born read. Unread
	// only ever comes from output written afterwards.
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, parent_id, label, created_at, updated_at, last_read_at) VALUES (?, ?, ?, ?, ?, ?)`,
		id, parent, label, now, now, now)
	return err
}

// scanSession scans one session row into s, tolerating a NULL parent_id
// (which is how roots are stored).
func scanSession(scanner interface{ Scan(...any) error }) (core.Session, error) {
	var sess core.Session
	var parent sql.NullString
	var pinned int
	var archived int
	var lastRun string
	if err := scanner.Scan(&sess.ID, &parent, &sess.Label, &sess.CreatedAt, &sess.UpdatedAt,
		&sess.PromptTokens, &sess.CompletionTokens, &pinned, &archived,
		&sess.TitleState, &sess.LastReadAt, &sess.Agent, &lastRun); err != nil {
		return core.Session{}, err
	}
	sess.ParentID = parent.String
	sess.Pinned = pinned != 0
	sess.Archived = archived != 0
	// Normalize: the row default is 'default', but a session written
	// by an older writer that left the column empty still belongs to
	// the default agent. omitempty then never hides the binding.
	if sess.Agent == "" {
		sess.Agent = agent.DefaultAgentName
	}
	// last_run is a JSON blob; empty means the session never ran. A
	// malformed blob decodes to nil - the rates just stay blank.
	if lastRun != "" {
		var stats core.LastRunStats
		if err := json.Unmarshal([]byte(lastRun), &stats); err == nil {
			sess.LastRun = &stats
		}
	}
	return sess, nil
}

// sessionColumns is the projection shared by GetSession and ListSessions.
const sessionColumns = "id, parent_id, label, created_at, updated_at, prompt_tokens, completion_tokens, pinned, archived, title_state, last_read_at, agent, last_run"

// SetSessionLastRun writes the last-run stats block (JSON) on the
// session's row, or clears it when stats is nil. It is a telemetry
// write, not activity - updated_at is untouched.
func (s *Store) SetSessionLastRun(ctx context.Context, sessionID string, stats *core.LastRunStats) error {
	encoded := ""
	if stats != nil {
		b, err := json.Marshal(stats)
		if err != nil {
			return err
		}
		encoded = string(b)
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET last_run = ? WHERE id = ?`, encoded, sessionID)
	return err
}

// GetSession returns the session with the given id, or ErrNotFound.
func (s *Store) GetSession(ctx context.Context, id string) (core.Session, error) {
	sess, err := scanSession(s.db.QueryRowContext(ctx,
		`SELECT `+sessionColumns+` FROM sessions WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return core.Session{}, ErrNotFound
	}
	if err != nil {
		return core.Session{}, err
	}
	return sess, nil
}

// ListSessions returns the sessions of the DEFAULT agent, ordered by
// creation time. This is the binding that keeps the pre-agent session
// list, tree, and insights exactly what they were (card t_7e2b7002):
// sessions created as another agent never appear here - they surface
// through ListAgentSessions. ComputeInsights has the same consequence
// and shares it knowingly: agent-owned sessions are v1 shells (no chat
// runs on them yet), so nothing measurable is missing from the numbers.
func (s *Store) ListSessions(ctx context.Context) ([]core.Session, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+sessionColumns+` FROM sessions WHERE agent = 'default' ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []core.Session
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

// DeleteSession removes a session. Messages and child branches cascade
// via the ON DELETE CASCADE foreign keys. It is a no-op (nil) when the
// session does not exist.
func (s *Store) DeleteSession(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	return err
}

func (s *Store) UpdateSession(ctx context.Context, id, label string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET label = ?, updated_at = ? WHERE id = ?`,
		label, core.NowISO(), id)
	return err
}

// GetSessionTree returns the session forest: every root session with its
// descendants nested under Children. Sessions are ordered by creation time.
func (s *Store) GetSessionTree(ctx context.Context) ([]*core.SessionNode, error) {
	sessions, err := s.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	// ponytail: build the tree in memory from a flat list. Fine for a
	// session store; if a session count ever grows into the thousands,
	// switch to a recursive CTE.
	byID := make(map[string]*core.SessionNode, len(sessions))
	for i := range sessions {
		byID[sessions[i].ID] = &core.SessionNode{Session: sessions[i]}
	}
	var roots []*core.SessionNode
	for _, sess := range sessions {
		node := byID[sess.ID]
		if node.ParentID == "" {
			roots = append(roots, node)
			continue
		}
		if parent, ok := byID[node.ParentID]; ok {
			parent.Children = append(parent.Children, node)
		} else {
			// Orphan (parent deleted without cascade): treat as a root.
			roots = append(roots, node)
		}
	}
	return roots, nil
}

// GetAncestorMessages returns the messages of a session and all its
// ancestors up to the root, in root-to-leaf order. A branch therefore
// inherits its parent's history as a prefix.
func (s *Store) GetAncestorMessages(ctx context.Context, id string) ([]core.Message, error) {
	// ponytail: walk the parent chain one query per hop. Fine for a
	// session store; a recursive CTE would collapse it to one query if
	// deep trees ever matter.
	var chain []string
	cur := id
	for cur != "" {
		chain = append(chain, cur)
		sess, err := s.GetSession(ctx, cur)
		if err != nil {
			return nil, err
		}
		cur = sess.ParentID
	}
	// chain is leaf-to-root; reverse to root-to-leaf.
	var out []core.Message
	for i := len(chain) - 1; i >= 0; i-- {
		messages, err := s.GetMessages(ctx, chain[i])
		if err != nil {
			return nil, err
		}
		out = append(out, messages...)
	}
	return out, nil
}

// AppendMessage appends a message to a session's history and touches
// the session's updated_at to the message's own write time. Without
// the touch, updated_at froze after the title's last write - the
// sidebar's time buckets silently drifted off the real activity, and
// no unread state could ever be derived from the row.
//
// Conversation messages get their tree pointer materialized here
// (t_17365247): an explicitly set ai.*.ParentID wins (validated against
// this session's rows); a missing pointer resolves to the active path
// leaf - the message follows (or answers) whatever the active
// conversation currently ends at. Bookkeeping entries
// (model_change/thinking_level_change/variant_select) persist
// pointerless; they live outside the tree.
func (s *Store) AppendMessage(ctx context.Context, sessionID string, msg core.Message) error {
	role, payload, err := core.EncodeMessage(msg)
	if err != nil {
		return err
	}
	if conversationRole(role) {
		parent, err := s.resolveParent(ctx, sessionID, msg)
		if err != nil {
			return err
		}
		if parent != "" {
			role, payload, err = core.EncodeMessage(core.WithParentID(msg, parent))
			if err != nil {
				return err
			}
		}
	}
	now := core.NowISO()
	if _, err = s.db.ExecContext(ctx,
		`INSERT INTO messages (session_id, role, payload, created_at) VALUES (?, ?, ?, ?)`,
		sessionID, role, string(payload), now); err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE sessions SET updated_at = ? WHERE id = ?`, now, sessionID)
	return err
}

// GetMessages returns a session's messages in append order.
func (s *Store) GetMessages(ctx context.Context, sessionID string) ([]core.Message, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT role, payload FROM messages WHERE session_id = ? ORDER BY id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []core.Message
	for rows.Next() {
		var role, payload string
		if err := rows.Scan(&role, &payload); err != nil {
			return nil, err
		}
		msg, err := core.DecodeMessage(role, []byte(payload))
		if err != nil {
			return nil, err
		}
		out = append(out, msg)
	}
	return out, rows.Err()
}

// GetMessagesWithTimes returns a session's messages in append order,
// each paired with its row id (the tree identity, core.MsgID renders
// it for the wire and pointers) and store-level created_at.
func (s *Store) GetMessagesWithTimes(ctx context.Context, sessionID string) ([]core.StoredMessage, error) {
	return s.loadRows(ctx, sessionID)
}

// CountMessages returns the number of messages in a session.
func (s *Store) CountMessages(ctx context.Context, sessionID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM messages WHERE session_id = ?`, sessionID).Scan(&n)
	return n, err
}

// SessionCounts computes per-session message and tool-result counts at
// read time. ponytail: one GROUP BY over the messages table, no
// maintained counters; fine for hundreds of sessions - counter columns
// are the upgrade if a session list ever gets slow. message_count is
// every stored message; tool_count is the tool-role subset.
// Bookkeeping entries (model_change, thinking_level_change,
// variant_select, verdict_card, verdict) and the branch summary row
// (role 'system') are
// excluded from message_count, matching the insights convention for
// non-conversation messages.
func (s *Store) SessionCounts(ctx context.Context) (map[string]core.SessionCounts, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT session_id,
		        SUM(role NOT IN ('model_change', 'thinking_level_change', 'variant_select', 'verdict_card', 'verdict', 'system')),
		        SUM(role = 'tool')
		 FROM messages GROUP BY session_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]core.SessionCounts)
	for rows.Next() {
		var id string
		var c core.SessionCounts
		if err := rows.Scan(&id, &c.MessageCount, &c.ToolCount); err != nil {
			return nil, err
		}
		out[id] = c
	}
	return out, rows.Err()
}

// SetSessionPinned sets the pinned flag on a session row. It is a
// user-list concern: updated_at (the time bucketing key) is untouched.
func (s *Store) SetSessionPinned(ctx context.Context, sessionID string, pinned bool) error {
	n := boolToInt(pinned)
	_, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET pinned = ? WHERE id = ?`, n, sessionID)
	return err
}

// SetSessionArchived sets the archived flag on a session row. Like
// pinning, archiving is a user-list concern: updated_at (the time
// bucketing key) is untouched.
func (s *Store) SetSessionArchived(ctx context.Context, sessionID string, archived bool) error {
	n := boolToInt(archived)
	_, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET archived = ? WHERE id = ?`, n, sessionID)
	return err
}

// SetSessionRead stamps the session's last-read time to now, so unread
// (updated_at after last_read_at) clears. Reading is not activity:
// updated_at is untouched.
func (s *Store) SetSessionRead(ctx context.Context, sessionID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET last_read_at = ? WHERE id = ?`, core.NowISO(), sessionID)
	return err
}

// SetSessionTitle writes a generated label and its title_state
// transition in one update. The state machine is enforced by the WHERE
// clause: provisional only from 0 (moves to 1), final from 0 or 1
// (moves to 2) - a session at 2 is never touched, including the
// pre-title-state rows that were migrated to 2 because they carried a
// label. A losing update (session already advanced) writes nothing.
// updated_at is untouched: a generated label is not activity, and a
// title landing after a read stamp must not turn the session unread.
func (s *Store) SetSessionTitle(ctx context.Context, sessionID string, title string, final bool) error {
	stmt := `UPDATE sessions SET label = ?, title_state = 1 WHERE id = ? AND title_state = 0`
	if final {
		stmt = `UPDATE sessions SET label = ?, title_state = 2 WHERE id = ? AND title_state <= 1`
	}
	_, err := s.db.ExecContext(ctx, stmt, title, sessionID)
	return err
}

// SessionModels returns the last recorded model per session from the
// session's model_change entries, read at read time (no schema or
// counters). Sessions with no model entry are absent from the map.
// ponytail: one pass over the model_change rows via MAX(id) per session;
// fine for hundreds of sessions.
func (s *Store) SessionModels(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT m.session_id, m.payload FROM messages m
		 WHERE m.role = 'model_change'
		   AND m.id = (SELECT MAX(id) FROM messages
		               WHERE session_id = m.session_id AND role = 'model_change')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]string)
	for rows.Next() {
		var id, payload string
		if err := rows.Scan(&id, &payload); err != nil {
			return nil, err
		}
		var mc core.ModelChange
		if json.Unmarshal([]byte(payload), &mc) != nil || mc.Model == "" {
			continue
		}
		out[id] = mc.Model
	}
	return out, rows.Err()
}

// GetImageCaption returns the persisted caption for an image id
// (media type + sha256 of the payload, set by the captioner).
// Implements agent.CaptionStore.
func (s *Store) GetImageCaption(ctx context.Context, imageID string) (caption string, ok bool, err error) {
	err = s.db.QueryRowContext(ctx,
		`SELECT caption FROM image_captions WHERE image_id = ?`, imageID).
		Scan(&caption)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return caption, true, nil
}

// SaveImageCaption persists one image caption. Re-saving the same image
// id keeps the first caption - a cached caption is never rewritten
// (nothing benefits from the churn; the id is content-addressed).
// Implements agent.CaptionStore.
func (s *Store) SaveImageCaption(ctx context.Context, imageID, caption string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO image_captions (image_id, caption, created_at) VALUES (?, ?, ?)
		 ON CONFLICT(image_id) DO NOTHING`,
		imageID, caption, core.NowISO())
	return err
}

// Compile-time assertion that Store implements agent.CaptionStore.
var _ agent.CaptionStore = (*Store)(nil)

// boolToInt maps a bool to its SQLite 0/1 integer representation.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// AddSessionTokens adds a turn's real provider-reported usage to the
// session's accumulated totals on the sessions row.
func (s *Store) AddSessionTokens(ctx context.Context, sessionID string, prompt, completion int) error {
	if prompt == 0 && completion == 0 {
		return nil
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET prompt_tokens = prompt_tokens + ?,
		 completion_tokens = completion_tokens + ? WHERE id = ?`,
		prompt, completion, sessionID)
	return err
}

// quoteFTS wraps a user-supplied search term in FTS5 double quotes so
// it matches as a literal phrase instead of being interpreted as query
// syntax (apostrophes, AND/OR/NEAR, prefixes, etc.), with the FTS5
// trailing-star phrase form `"term"*` so the LAST token also prefix-
// matches (a search box narrows as you type: "fibo" finds "fibonacci",
// in-app comment 5 2026-10-18 - exact-token-only made progressive
// typing empty the results). Embedded double quotes are doubled per
// FTS5 string-literal rules.
func quoteFTS(term string) string {
	return `"` + strings.ReplaceAll(term, `"`, `""`) + `"*`
}

// SearchMessages performs a full-text search across all session messages.
// The term is matched literally (phrase-quoted); returns matching sessions
// with a snippet of the matching content.
func (s *Store) SearchMessages(ctx context.Context, query string) ([]core.SearchResult, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT session_id, snippet(messages_fts, 1, '[', ']', '...', 20)
		 FROM messages_fts WHERE messages_fts MATCH ?
		 ORDER BY rank`, quoteFTS(query))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []core.SearchResult
	for rows.Next() {
		var r core.SearchResult
		if err := rows.Scan(&r.SessionID, &r.Snippet); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SearchSessions performs the unified search behind /sessions/search:
// one phrase-quoted term (quoteFTS, `"term"*`) queries BOTH the
// session-label index (sessions_fts) and the message-content index
// (messages_fts), merged to ONE hit per session. A label match
// outranks any content match for the same session; label-matching
// sessions lead, content-only sessions follow in their best rank.
// Progressive typing is preserved end to end: "fib", "fibo",
// "fibonacci" each still hit (quoteFTS trailing star, in-app comment
// 5 2026-10-18).
func (s *Store) SearchSessions(ctx context.Context, query string) ([]core.SearchResult, error) {
	fts := quoteFTS(query)
	out := []core.SearchResult{}
	seen := make(map[string]bool)

	// Label hits first. The snippet is the label itself - there is no
	// content context to bracket-mark.
	trows, err := s.db.QueryContext(ctx,
		`SELECT session_id, label FROM sessions_fts
		 WHERE sessions_fts MATCH ? ORDER BY rank`, fts)
	if err != nil {
		return nil, err
	}
	for trows.Next() {
		var id, label string
		if err := trows.Scan(&id, &label); err != nil {
			trows.Close()
			return nil, err
		}
		// The label index holds one row per session, but belt-and-
		// braces on scans keeps the one-hit-per-session contract
		// local to this method.
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, core.SearchResult{SessionID: id, Snippet: label, Title: true})
	}
	if err := trows.Err(); err != nil {
		trows.Close()
		return nil, err
	}
	trows.Close()

	// Content hits, best rank first; label-matched sessions skip.
	crows, err := s.db.QueryContext(ctx,
		`SELECT session_id, snippet(messages_fts, 1, '[', ']', '...', 20)
		 FROM messages_fts WHERE messages_fts MATCH ? ORDER BY rank`, fts)
	if err != nil {
		return nil, err
	}
	defer crows.Close()
	for crows.Next() {
		var id, snippet string
		if err := crows.Scan(&id, &snippet); err != nil {
			return nil, err
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, core.SearchResult{SessionID: id, Snippet: snippet})
	}
	return out, crows.Err()
}

// sessionStats accumulates per-session counters that feed into Insights.
type sessionStats struct {
	msgs       int
	userMsgs   int
	toolCalls  int
	tokens     int
	toolCounts map[string]int
}

// notableTracker tracks the running maximum for each notable stat.
type notableTracker struct {
	maxMsgs   int
	maxTokens int
	maxTools  int
}

// countMessages iterates over a session's messages, incrementing the
// global message counter and accumulating per-session user/tool stats.
// Non-conversation entries (ModelChange, ThinkingLevelChange,
// VariantSelect) are skipped.
func countMessages(msgs []core.Message, in *core.Insights, st *sessionStats) {
	for _, msg := range msgs {
		switch msg.(type) {
		case core.ModelChange, core.ThinkingLevelChange, core.VariantSelect:
			continue
		}
		in.Messages++
		switch m := msg.(type) {
		case core.User:
			st.userMsgs++
		case core.Assistant:
			st.toolCalls += len(m.ToolCalls)
			for _, tc := range m.ToolCalls {
				st.toolCounts[tc.Name]++
			}
		}
	}
}

// updateNotables updates the notable stats if the current session
// exceeds the running maxima for messages, tokens, or tool calls.
func updateNotables(in *core.Insights, st *sessionStats, nt *notableTracker, detail string) {
	if st.msgs > nt.maxMsgs {
		nt.maxMsgs = st.msgs
		in.NotableMsgs = core.NotableStat{Value: st.msgs, Detail: detail}
	}
	if st.tokens > nt.maxTokens {
		nt.maxTokens = st.tokens
		in.NotableTokens = core.NotableStat{Value: st.tokens, Detail: detail}
	}
	if st.toolCalls > nt.maxTools {
		nt.maxTools = st.toolCalls
		in.NotableTools = core.NotableStat{Value: st.toolCalls, Detail: detail}
	}
}

// processSession fetches messages for one session and accumulates its
// stats into the provided Insights and sessionStats. It returns true if
// the session was processed, false if skipped due to parse or fetch error.
func (s *Store) processSession(ctx context.Context, sess core.Session, in *core.Insights, st *sessionStats, nt *notableTracker, dayCounts *[7]int) bool {
	t, err := time.Parse(time.RFC3339, sess.CreatedAt)
	if err != nil {
		return false
	}
	msgs, err := s.GetMessages(ctx, sess.ID)
	if err != nil {
		return false
	}
	st.msgs = len(msgs)
	st.userMsgs = 0
	st.toolCalls = 0
	countMessages(msgs, in, st)
	in.UserMessages += st.userMsgs
	in.ToolCalls += st.toolCalls
	st.tokens = agent.EstimateTokens(msgs)
	in.TotalTokens += st.tokens

	// Daily activity by weekday.
	wd := int(t.Weekday())
	if wd == 0 {
		wd = 6 // Sunday -> 6, Mon -> 0
	}
	dayCounts[wd]++

	// Notable sessions.
	label := sess.Label
	if label == "" {
		label = sess.ID
	}
	detail := t.Format("Jan 2") + ", " + label
	updateNotables(in, st, nt, detail)
	return true
}

// buildDailyActivity fills the Daily [7]DayStat array from dayCounts,
// computing visual bar widths proportional to the maximum day count.
func buildDailyActivity(daily *[7]core.DayStat, dayCounts [7]int, weekdays []string) {
	maxDay := 0
	for _, c := range dayCounts {
		if c > maxDay {
			maxDay = c
		}
	}
	for i := 0; i < 7; i++ {
		bar := ""
		if maxDay > 0 {
			bars := int(float64(dayCounts[i]) / float64(maxDay) * 14)
			for j := 0; j < bars; j++ {
				bar += "█"
			}
		}
		daily[i] = core.DayStat{Day: weekdays[i], Count: dayCounts[i], Bar: bar}
	}
}

// ComputeInsights aggregates session data over the last N days.
// If days <= 0, all sessions are included.
func (s *Store) ComputeInsights(ctx context.Context, days int) (*core.Insights, error) {
	sessions, err := s.ListSessions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}

	now := time.Now()
	cutoff := time.Time{}
	if days > 0 {
		cutoff = now.AddDate(0, 0, -days)
	}

	weekdays := []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}
	toolCounts := map[string]int{}
	dayCounts := [7]int{}
	in := &core.Insights{Days: days}
	if days > 0 {
		in.Period = fmt.Sprintf("Last %d days", days)
	} else {
		in.Period = "All time"
	}
	in.PeriodEnd = now.Format("2006-01-02")

	nt := &notableTracker{}
	st := &sessionStats{toolCounts: toolCounts}

	for _, sess := range sessions {
		t, err := time.Parse(time.RFC3339, sess.CreatedAt)
		if err != nil {
			continue
		}
		if !t.Before(cutoff) || days <= 0 {
			in.Sessions++
			s.processSession(ctx, sess, in, st, nt, &dayCounts)
		}
	}

	if in.Sessions > 0 {
		in.AvgSessionMsgs = float64(in.Messages) / float64(in.Sessions)
	}

	// Build tool breakdown sorted by count desc.
	for name, count := range toolCounts {
		in.Tools = append(in.Tools, core.ToolStat{Name: name, Count: count})
	}
	sort.Slice(in.Tools, func(i, j int) bool {
		return in.Tools[i].Count > in.Tools[j].Count
	})

	buildDailyActivity(&in.Daily, dayCounts, weekdays)

	if days > 0 {
		in.PeriodStart = cutoff.Format("2006-01-02")
	}

	return in, nil
}
