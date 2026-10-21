package store

import (
	"context"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/ai"
)

// Message tree support (t_17365247): parent pointers ride the whole-
// message JSON payloads; row ids are the message identities; selections
// persist as ai.VariantSelect bookkeeping entries in the append list.
// No schema migration.

// conversationRole reports whether a stored role takes part in the
// conversation tree.
func conversationRole(role string) bool {
	return role == "user" || role == "assistant" || role == "tool"
}

// loadRows reads a session's messages (id, role, payload, created_at)
// in append order, decoded. Shared by the active-view machinery.
func (s *Store) loadRows(ctx context.Context, sessionID string) ([]agent.StoredMessage, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, role, payload, created_at FROM messages WHERE session_id = ? ORDER BY id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []agent.StoredMessage
	for rows.Next() {
		var role, payload string
		sm := agent.StoredMessage{}
		if err := rows.Scan(&sm.ID, &role, &payload, &sm.CreatedAt); err != nil {
			return nil, err
		}
		m, err := ai.DecodeMessage(role, []byte(payload))
		if err != nil {
			return nil, err
		}
		sm.Message = m
		out = append(out, sm)
	}
	return out, rows.Err()
}

// GetActiveView returns the session's tree-derived active view: the
// active path (context assembly + render order) and the branch-point
// metadata. Sessions written without pointers derive the same linear
// path they always rendered.
func (s *Store) GetActiveView(ctx context.Context, sessionID string) (agent.ActiveView, error) {
	if _, err := s.GetSession(ctx, sessionID); err != nil {
		return agent.ActiveView{}, err
	}
	rows, err := s.loadRows(ctx, sessionID)
	if err != nil {
		return agent.ActiveView{}, err
	}
	return agent.DeriveTree(rows), nil
}

// GetActiveMessages returns the active path as plain messages - the
// context assembly and session-load seam.
func (s *Store) GetActiveMessages(ctx context.Context, sessionID string) ([]ai.Message, error) {
	view, err := s.GetActiveView(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	out := make([]ai.Message, 0, len(view.Path))
	for _, sm := range view.Path {
		out = append(out, sm.Message)
	}
	return out, nil
}

// resolveParent computes the parent pointer a conversation message
// carries into its row: an explicitly set pointer on the message wins
// (validated against the session's rows - a dangling pointer would
// truncate the chain), otherwise it derives to the active path's leaf
// (what the message follows/answers right now).
func (s *Store) resolveParent(ctx context.Context, sessionID string, msg ai.Message) (string, error) {
	if parent := agent.ParentIDOf(msg); parent != "" {
		numeric, err := agent.ParseMsgID(parent)
		if err != nil {
			return "", err
		}
		var n int
		if err := s.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM messages WHERE id = ? AND session_id = ?`, numeric, sessionID).Scan(&n); err != nil {
			return "", err
		}
		if n == 0 {
			return "", &parentMsgError{parent: parent, session: sessionID}
		}
		return parent, nil
	}
	view, err := s.GetActiveView(ctx, sessionID)
	if err != nil {
		return "", err
	}
	if len(view.Path) == 0 {
		return "", nil
	}
	return agent.MsgID(view.Path[len(view.Path)-1].ID), nil
}

// parentMsgError reports an explicit pointer the session's rows don't
// carry (dangling pointers would truncate the chain - reject loudly).
type parentMsgError struct{ parent, session string }

func (e *parentMsgError) Error() string {
	return "parent message " + e.parent + " not found in session " + e.session
}

// AppendMessageRow inserts an already-encoded row; exposed for tests
// that need to seed pre-pointer (legacy) sessions.
func (s *Store) AppendMessageRow(ctx context.Context, sessionID, role, payload, createdAt string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO messages (session_id, role, payload, created_at) VALUES (?, ?, ?, ?)`,
		sessionID, role, payload, createdAt)
	return err
}
