package gateway

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
	"github.com/EndoTheDev/omega/ai"
)

// ErrNotFound is returned when a session does not exist.
var ErrNotFound = errors.New("session not found")

// Compile-time assertion that Store implements agent.SessionStore.
var _ agent.SessionStore = (*Store)(nil)

// Session is a persisted conversation container. ParentID links a session
// to its parent in the session tree (empty for roots); Label is a
// user-assigned name shown in listings.
type Session struct {
	ID        string `json:"id"`
	ParentID  string `json:"parent_id,omitempty"`
	Label     string `json:"label,omitempty"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// SessionNode is one node in the session tree returned by GetSessionTree.
type SessionNode struct {
	Session
	Children []*SessionNode
}

// Store persists sessions and their message history in SQLite.
// It is safe for concurrent use via *sql.DB.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the SQLite database at dsn and ensures
// the schema exists. Use ":memory:" for tests.
func Open(dsn string) (*Store, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// SQLite allows one writer; a single connection serializes access and
	// avoids SQLITE_BUSY. ponytail: fine for a session store; if write
	// concurrency becomes a bottleneck, switch to WAL mode plus a pool.
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	// SQLite disables foreign keys by default; the messages->sessions
	// cascade needs them on. Set per connection (single conn here).
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
`)
	if err != nil {
		return err
	}
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
func scanSession(scanner interface{ Scan(...any) error }) (Session, error) {
	var sess Session
	var parent sql.NullString
	if err := scanner.Scan(&sess.ID, &parent, &sess.Label, &sess.CreatedAt, &sess.UpdatedAt); err != nil {
		return Session{}, err
	}
	sess.ParentID = parent.String
	return sess, nil
}

// GetSession returns the session with the given id, or ErrNotFound.
func (s *Store) GetSession(ctx context.Context, id string) (Session, error) {
	sess, err := scanSession(s.db.QueryRowContext(ctx,
		`SELECT id, parent_id, label, created_at, updated_at FROM sessions WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, err
	}
	return sess, nil
}

// ListSessions returns all sessions ordered by creation time.
func (s *Store) ListSessions(ctx context.Context) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, parent_id, label, created_at, updated_at FROM sessions ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
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

// BranchSession creates a child session under parentID. The child starts
// empty; its history is the parent's via GetAncestorMessages. It returns
// an error if the parent does not exist.
func (s *Store) BranchSession(ctx context.Context, parentID, id string) error {
	return s.CreateSession(ctx, id, parentID, "")
}

// SetLabel sets (or clears, with an empty label) a session's label.
func (s *Store) SetLabel(ctx context.Context, id, label string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET label = ?, updated_at = ? WHERE id = ?`,
		label, ai.NowISO(), id)
	return err
}

// GetSessionTree returns the session forest: every root session with its
// descendants nested under Children. Sessions are ordered by creation time.
func (s *Store) GetSessionTree(ctx context.Context) ([]*SessionNode, error) {
	sessions, err := s.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	// ponytail: build the tree in memory from a flat list. Fine for a
	// session store; if a session count ever grows into the thousands,
	// switch to a recursive CTE.
	byID := make(map[string]*SessionNode, len(sessions))
	for i := range sessions {
		byID[sessions[i].ID] = &SessionNode{Session: sessions[i]}
	}
	var roots []*SessionNode
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
	role, payload, err := encodeMessage(msg)
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
		msg, err := decodeMessage(role, []byte(payload))
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

// encodeMessage serializes an ai.Message to its role discriminator and
// JSON payload, mirroring the /chat wire format.
func encodeMessage(msg ai.Message) (string, []byte, error) {
	var role string
	switch msg.(type) {
	case ai.System:
		role = "system"
	case ai.User:
		role = "user"
	case ai.Assistant:
		role = "assistant"
	case ai.ToolResult:
		role = "tool"
	case ai.ModelChange:
		role = "model_change"
	case ai.ThinkingLevelChange:
		role = "thinking_level_change"
	default:
		return "", nil, fmt.Errorf("unknown message type %T", msg)
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		return "", nil, err
	}
	return role, payload, nil
}

// decodeMessage reconstructs an ai.Message from a stored role and payload.
func decodeMessage(role string, payload []byte) (ai.Message, error) {
	switch role {
	case "system":
		var m ai.System
		if err := json.Unmarshal(payload, &m); err != nil {
			return nil, err
		}
		return m, nil
	case "user":
		var m ai.User
		if err := json.Unmarshal(payload, &m); err != nil {
			return nil, err
		}
		return m, nil
	case "assistant":
		var m ai.Assistant
		if err := json.Unmarshal(payload, &m); err != nil {
			return nil, err
		}
		return m, nil
	case "tool":
		var m ai.ToolResult
		if err := json.Unmarshal(payload, &m); err != nil {
			return nil, err
		}
		return m, nil
	case "model_change":
		var m ai.ModelChange
		if err := json.Unmarshal(payload, &m); err != nil {
			return nil, err
		}
		return m, nil
	case "thinking_level_change":
		var m ai.ThinkingLevelChange
		if err := json.Unmarshal(payload, &m); err != nil {
			return nil, err
		}
		return m, nil
	default:
		return nil, fmt.Errorf("unknown role %q", role)
	}
}

// ToolStat is one row in the tool breakdown.
type ToolStat struct {
	Name  string
	Count int
}

// DayStat is one row in the daily activity breakdown.
type DayStat struct {
	Day     string // "Mon", "Tue", etc.
	Count   int
	Bar     string // visual bar string
}

// NotableStat holds the most extreme session for a given metric.
type NotableStat struct {
	Value  int
	Detail string // date or session label
}

// Insights is the aggregated cross-session analytics result.
type Insights struct {
	Period         string
	PeriodStart    string
	PeriodEnd      string
	Days           int
	Sessions       int
	Messages       int
	UserMessages   int
	ToolCalls      int
	TotalTokens    int
	AvgSessionMsgs float64
	Tools          []ToolStat
	Daily          [7]DayStat
	NotableMsgs    NotableStat
	NotableTokens  NotableStat
	NotableTools   NotableStat
}

// ComputeInsights aggregates session data over the last N days.
// If days <= 0, all sessions are included.
func (s *Store) ComputeInsights(ctx context.Context, days int) (*Insights, error) {
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
	in := &Insights{Days: days}
	if days > 0 {
		in.Period = fmt.Sprintf("Last %d days", days)
	} else {
		in.Period = "All time"
	}
	in.PeriodEnd = now.Format("2006-01-02")

	maxMsgs, maxTokens, maxTools := 0, 0, 0

	for _, sess := range sessions {
		t, err := time.Parse(time.RFC3339, sess.CreatedAt)
		if err != nil {
			continue
		}
		if !t.Before(cutoff) || days <= 0 {
			in.Sessions++
			msgs, err := s.GetMessages(ctx, sess.ID)
			if err != nil {
				continue
			}
			sessMsgs := len(msgs)
			sessUserMsgs := 0
			sessToolCalls := 0
			for _, msg := range msgs {
				// Skip non-conversation entries (metadata, not messages).
				switch msg.(type) {
				case ai.ModelChange, ai.ThinkingLevelChange:
					continue
				}
				in.Messages++
				switch m := msg.(type) {
				case ai.User:
					sessUserMsgs++
				case ai.Assistant:
					sessToolCalls += len(m.ToolCalls)
					for _, tc := range m.ToolCalls {
						toolCounts[tc.Name]++
					}
				}
			}
			in.UserMessages += sessUserMsgs
			in.ToolCalls += sessToolCalls
			sessTokens := 0
			for _, msg := range msgs {
				sessTokens += len(agent.MessageText(msg))
			}
			sessTokens /= 4 // charsPerToken
			in.TotalTokens += sessTokens

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
			if sessMsgs > maxMsgs {
				maxMsgs = sessMsgs
				in.NotableMsgs = NotableStat{Value: sessMsgs, Detail: detail}
			}
			if sessTokens > maxTokens {
				maxTokens = sessTokens
				in.NotableTokens = NotableStat{Value: sessTokens, Detail: detail}
			}
			if sessToolCalls > maxTools {
				maxTools = sessToolCalls
				in.NotableTools = NotableStat{Value: sessToolCalls, Detail: detail}
			}
		}
	}

	if in.Sessions > 0 {
		in.AvgSessionMsgs = float64(in.Messages) / float64(in.Sessions)
	}

	// Build tool breakdown sorted by count desc.
	for name, count := range toolCounts {
		in.Tools = append(in.Tools, ToolStat{Name: name, Count: count})
	}
	sort.Slice(in.Tools, func(i, j int) bool {
		return in.Tools[i].Count > in.Tools[j].Count
	})

	// Build daily activity with bars.
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
		in.Daily[i] = DayStat{Day: weekdays[i], Count: dayCounts[i], Bar: bar}
	}

	if days > 0 {
		in.PeriodStart = cutoff.Format("2006-01-02")
	}

	return in, nil
}
