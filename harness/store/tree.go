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

// loadRows reads a session's messages from its file: the truth, in append order. A
// session with no file and no messages answers nil, never an empty slice.
func (s *Store) loadRows(ctx context.Context, sessionID string) ([]core.StoredMessage, error) {
	folded, err := s.readSessionFile(sessionID)
	if err != nil {
		return nil, err
	}
	var out []core.StoredMessage
	for _, line := range folded.messages {
		msg, err := core.DecodeMessage(line.Role, line.Payload)
		if err != nil {
			return nil, err
		}
		out = append(out, core.StoredMessage{ID: line.ID, Message: msg, CreatedAt: line.CreatedAt})
	}
	return out, nil
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

// AppendMessageRow appends an already-encoded message line.
func (s *Store) AppendMessageRow(ctx context.Context, sessionID, role, payload, createdAt string) error {
	return s.appendMessageLine(ctx, sessionID, role, []byte(payload), createdAt)
}
