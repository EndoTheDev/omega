package gateway

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/EndoTheDev/omega-dev/internal/ai"
)

// ErrNotFound is returned when a session does not exist.
var ErrNotFound = errors.New("session not found")

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
	now := nowISO()
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

// DeleteSession removes a session and its messages (cascade).
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
		label, nowISO(), id)
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
		sessionID, role, string(payload), nowISO())
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

// nowISO returns a UTC timestamp in ISO 8601 format.
func nowISO() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
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
	default:
		return nil, fmt.Errorf("unknown role %q", role)
	}
}
