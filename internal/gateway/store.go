package gateway

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"github.com/EndoTheDev/omega-agent/internal/ai"
)

// ErrNotFound is returned when a session does not exist.
var ErrNotFound = errors.New("session not found")

// Session is a persisted conversation container.
type Session struct {
	ID        string `json:"id"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
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
	return err
}

// CreateSession creates a session with the given id. It returns an error
// if a session with that id already exists.
func (s *Store) CreateSession(ctx context.Context, id string) error {
	now := nowISO()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, created_at, updated_at) VALUES (?, ?, ?)`,
		id, now, now)
	return err
}

// GetSession returns the session with the given id, or ErrNotFound.
func (s *Store) GetSession(ctx context.Context, id string) (Session, error) {
	var sess Session
	err := s.db.QueryRowContext(ctx,
		`SELECT id, created_at, updated_at FROM sessions WHERE id = ?`, id).
		Scan(&sess.ID, &sess.CreatedAt, &sess.UpdatedAt)
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
		`SELECT id, created_at, updated_at FROM sessions ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		var sess Session
		if err := rows.Scan(&sess.ID, &sess.CreatedAt, &sess.UpdatedAt); err != nil {
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
