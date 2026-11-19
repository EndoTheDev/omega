package store

import (
	"context"

	"github.com/EndoTheDev/omega/core"
)

// Message tree support: parent pointers ride the whole- message JSON payloads.

// conversationRole reports whether a stored role takes part in the conversation tree.
func conversationRole(role string) bool {
	return role == "user" || role == "assistant" || role == "tool"
}

// loadRows reads a session's messages (id, role, payload, created_at) in append.
func (s *Store) loadRows(ctx context.Context, sessionID string) ([]core.StoredMessage, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, role, payload, created_at FROM messages WHERE session_id = ? ORDER BY id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []core.StoredMessage
	for rows.Next() {
		var role, payload string
		sm := core.StoredMessage{}
		if err := rows.Scan(&sm.ID, &role, &payload, &sm.CreatedAt); err != nil {
			return nil, err
		}
		m, err := core.DecodeMessage(role, []byte(payload))
		if err != nil {
			return nil, err
		}
		sm.Message = m
		out = append(out, sm)
	}
	return out, rows.Err()
}

// loadViewRows reads the rows a PARENTED session's tree reads. Pre-split delegate
// children still carry parent_id and keep rendering merged (historical rows, never
// rewritten).
func (s *Store) loadViewRows(ctx context.Context, sessionID string) ([]core.StoredMessage, error) {
	own, err := s.loadRows(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	sess, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if sess.ParentID == "" {
		return own, nil
	}
	var chain []string
	seen := map[string]bool{sessionID: true}
	cur := sess.ParentID
	for cur != "" && !seen[cur] {
		seen[cur] = true
		chain = append(chain, cur)
		parent, err := s.GetSession(ctx, cur)
		if err != nil {
			break
		}
		cur = parent.ParentID
	}
	out := own
	for i := len(chain) - 1; i >= 0; i-- {
		rows, err := s.loadRows(ctx, chain[i])
		if err != nil {
			return nil, err
		}
		out = append(rows, out...)
	}
	return out, nil
}

// GetActiveView returns the session's tree-derived active view.
func (s *Store) GetActiveView(ctx context.Context, sessionID string) (core.ActiveView, error) {
	if o := s.owner(ctx, sessionID); o != nil {
		return o.GetActiveView(ctx, sessionID)
	}
	if _, err := s.GetSession(ctx, sessionID); err != nil {
		return core.ActiveView{}, err
	}
	rows, err := s.loadViewRows(ctx, sessionID)
	if err != nil {
		return core.ActiveView{}, err
	}
	return core.DeriveTree(rows), nil
}

// GetActiveMessages returns the active path as plain messages.
func (s *Store) GetActiveMessages(ctx context.Context, sessionID string) ([]core.Message, error) {
	if o := s.owner(ctx, sessionID); o != nil {
		return o.GetActiveMessages(ctx, sessionID)
	}
	view, err := s.GetActiveView(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	out := make([]core.Message, 0, len(view.Path))
	for _, sm := range view.Path {
		out = append(out, sm.Message)
	}
	return out, nil
}

// resolveParent computes the parent pointer a conversation message carries.
func (s *Store) resolveParent(ctx context.Context, sessionID string, msg core.Message) (string, error) {
	view, err := s.GetActiveView(ctx, sessionID)
	if err != nil {
		return "", err
	}
	if parent := core.ParentIDOf(msg); parent != "" {
		numeric, err := core.ParseMsgID(parent)
		if err != nil {
			return "", err
		}
		for _, sm := range view.Path {
			if sm.ID == numeric {
				return parent, nil
			}
		}
		return "", &parentMsgError{parent: parent, session: sessionID}
	}
	if len(view.Path) == 0 {
		return "", nil
	}
	return core.MsgID(view.Path[len(view.Path)-1].ID), nil
}

// parentMsgError reports an explicit pointer the session's rows don't carry.
type parentMsgError struct{ parent, session string }

func (e *parentMsgError) Error() string {
	return "parent message " + e.parent + " not found in session " + e.session
}

// AppendMessageRow inserts an already-encoded row.
func (s *Store) AppendMessageRow(ctx context.Context, sessionID, role, payload, createdAt string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO messages (session_id, role, payload, created_at) VALUES (?, ?, ?, ?)`,
		sessionID, role, payload, createdAt)
	return err
}
