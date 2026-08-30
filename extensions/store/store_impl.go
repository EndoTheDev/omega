package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/ai"
)

// ErrNotFound is returned when a session does not exist.
var ErrNotFound = errors.New("session not found")

// Compile-time assertion that Store implements agent.StoreProvider.
var _ agent.StoreProvider = (*Store)(nil)

// Store persists sessions and its message history in SQLite.
// It is safe for concurrent use via *sql.DB.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the SQLite database at dsn and ensures
// the schema exists. Use ":memory:" for tests.
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
	// WAL mode allows concurrent readers + one writer across multiple
	// processes (e.g. parent + subagents). busy_timeout retries on lock
	// instead of returning SQLITE_BUSY immediately.
	dsnParams := dsn
	if !strings.Contains(dsn, "?") {
		dsnParams += "?_journal_mode=WAL&_busy_timeout=5000"
	} else if !strings.Contains(dsn, "_journal_mode=") {
		dsnParams += "&_journal_mode=WAL&_busy_timeout=5000"
	}
	db, err := sql.Open("sqlite", dsnParams)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	// SQLite disables foreign keys by default; the messages->sessions
	// cascade needs them on.
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
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
	updated_at TEXT NOT NULL
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
`)
	if err != nil {
		return err
	}
	// Rebuild FTS index to pick up any messages that existed before
	// the FTS table was created (triggers only fire on new writes).
	s.db.Exec("INSERT INTO messages_fts(messages_fts) VALUES('rebuild')")
	// Add columns that may be missing from older databases. SQLite
	// does not support IF NOT EXISTS on ALTER TABLE, so we catch
	// the "duplicate column" error and ignore it.
	for _, stmt := range []string{
		`ALTER TABLE sessions ADD COLUMN parent_id TEXT REFERENCES sessions(id) ON DELETE CASCADE`,
		`ALTER TABLE sessions ADD COLUMN label TEXT NOT NULL DEFAULT ''`,
	} {
		if _, err := s.db.Exec(stmt); err != nil {
			// SQLite error for duplicate column: "duplicate column name"
			if !strings.Contains(err.Error(), "duplicate column") {
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
	now := ai.NowISO()
	// An empty parentID is stored as NULL so the FK constraint does not
	// try to match a session with id "".
	var parent any
	if parentID != "" {
		parent = parentID
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, parent_id, label, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		id, parent, label, now, now)
	return err
}

// scanSession scans one session row into s, tolerating a NULL parent_id
// (which is how roots are stored).
func scanSession(scanner interface{ Scan(...any) error }) (agent.Session, error) {
	var sess agent.Session
	var parent sql.NullString
	if err := scanner.Scan(&sess.ID, &parent, &sess.Label, &sess.CreatedAt, &sess.UpdatedAt); err != nil {
		return agent.Session{}, err
	}
	sess.ParentID = parent.String
	return sess, nil
}

// GetSession returns the session with the given id, or ErrNotFound.
func (s *Store) GetSession(ctx context.Context, id string) (agent.Session, error) {
	sess, err := scanSession(s.db.QueryRowContext(ctx,
		`SELECT id, parent_id, label, created_at, updated_at FROM sessions WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return agent.Session{}, ErrNotFound
	}
	if err != nil {
		return agent.Session{}, err
	}
	return sess, nil
}

// ListSessions returns all sessions ordered by creation time.
func (s *Store) ListSessions(ctx context.Context) ([]agent.Session, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, parent_id, label, created_at, updated_at FROM sessions ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []agent.Session
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
		label, ai.NowISO(), id)
	return err
}

// GetSessionTree returns the session forest: every root session with its
// descendants nested under Children. Sessions are ordered by creation time.
func (s *Store) GetSessionTree(ctx context.Context) ([]*agent.SessionNode, error) {
	sessions, err := s.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	// ponytail: build the tree in memory from a flat list. Fine for a
	// session store; if a session count ever grows into the thousands,
	// switch to a recursive CTE.
	byID := make(map[string]*agent.SessionNode, len(sessions))
	for i := range sessions {
		byID[sessions[i].ID] = &agent.SessionNode{Session: sessions[i]}
	}
	var roots []*agent.SessionNode
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
func (s *Store) GetAncestorMessages(ctx context.Context, id string) ([]ai.Message, error) {
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
	var out []ai.Message
	for i := len(chain) - 1; i >= 0; i-- {
		messages, err := s.GetMessages(ctx, chain[i])
		if err != nil {
			return nil, err
		}
		out = append(out, messages...)
	}
	return out, nil
}

// AppendMessage appends a message to a session's history.
func (s *Store) AppendMessage(ctx context.Context, sessionID string, msg ai.Message) error {
	role, payload, err := ai.EncodeMessage(msg)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO messages (session_id, role, payload, created_at) VALUES (?, ?, ?, ?)`,
		sessionID, role, string(payload), ai.NowISO())
	return err
}

// GetMessages returns a session's messages in append order.
func (s *Store) GetMessages(ctx context.Context, sessionID string) ([]ai.Message, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT role, payload FROM messages WHERE session_id = ? ORDER BY id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ai.Message
	for rows.Next() {
		var role, payload string
		if err := rows.Scan(&role, &payload); err != nil {
			return nil, err
		}
		msg, err := ai.DecodeMessage(role, []byte(payload))
		if err != nil {
			return nil, err
		}
		out = append(out, msg)
	}
	return out, rows.Err()
}

// CountMessages returns the number of messages in a session.
func (s *Store) CountMessages(ctx context.Context, sessionID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM messages WHERE session_id = ?`, sessionID).Scan(&n)
	return n, err
}

// SearchMessages performs a full-text search across all session messages.
// Returns matching sessions with a snippet of the matching content.
func (s *Store) SearchMessages(ctx context.Context, query string) ([]agent.SearchResult, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT session_id, snippet(messages_fts, 1, '[', ']', '...', 20)
		 FROM messages_fts WHERE messages_fts MATCH ?
		 ORDER BY rank`, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []agent.SearchResult
	for rows.Next() {
		var r agent.SearchResult
		if err := rows.Scan(&r.SessionID, &r.Snippet); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
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
// Non-conversation entries (ModelChange, ThinkingLevelChange) are skipped.
func countMessages(msgs []ai.Message, in *agent.Insights, st *sessionStats) {
	for _, msg := range msgs {
		switch msg.(type) {
		case ai.ModelChange, ai.ThinkingLevelChange:
			continue
		}
		in.Messages++
		switch m := msg.(type) {
		case ai.User:
			st.userMsgs++
		case ai.Assistant:
			st.toolCalls += len(m.ToolCalls)
			for _, tc := range m.ToolCalls {
				st.toolCounts[tc.Name]++
			}
		}
	}
}

// countTokens sums the estimated token count for all messages in a
// session (chars / 4).
func countTokens(msgs []ai.Message) int {
	tokens := 0
	for _, msg := range msgs {
		tokens += len(agent.MessageText(msg))
	}
	return tokens / 4 // charsPerToken
}

// updateNotables updates the notable stats if the current session
// exceeds the running maxima for messages, tokens, or tool calls.
func updateNotables(in *agent.Insights, st *sessionStats, nt *notableTracker, detail string) {
	if st.msgs > nt.maxMsgs {
		nt.maxMsgs = st.msgs
		in.NotableMsgs = agent.NotableStat{Value: st.msgs, Detail: detail}
	}
	if st.tokens > nt.maxTokens {
		nt.maxTokens = st.tokens
		in.NotableTokens = agent.NotableStat{Value: st.tokens, Detail: detail}
	}
	if st.toolCalls > nt.maxTools {
		nt.maxTools = st.toolCalls
		in.NotableTools = agent.NotableStat{Value: st.toolCalls, Detail: detail}
	}
}

// processSession fetches messages for one session and accumulates its
// stats into the provided Insights and sessionStats. It returns true if
// the session was processed, false if skipped due to parse or fetch error.
func (s *Store) processSession(ctx context.Context, sess agent.Session, in *agent.Insights, st *sessionStats, nt *notableTracker, dayCounts *[7]int) bool {
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
	st.tokens = countTokens(msgs)
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
func buildDailyActivity(daily *[7]agent.DayStat, dayCounts [7]int, weekdays []string) {
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
		daily[i] = agent.DayStat{Day: weekdays[i], Count: dayCounts[i], Bar: bar}
	}
}

// ComputeInsights aggregates session data over the last N days.
// If days <= 0, all sessions are included.
func (s *Store) ComputeInsights(ctx context.Context, days int) (*agent.Insights, error) {
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
	in := &agent.Insights{Days: days}
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
		in.Tools = append(in.Tools, agent.ToolStat{Name: name, Count: count})
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
