package store

import (
	"context"

	"github.com/EndoTheDev/omega/core"
)

// Message tree support (t_17365247): parent pointers ride the whole-
// message JSON payloads; row ids are the message identities; selections
// persist as core.VariantSelect bookkeeping entries in the append list.
// No schema migration.

// conversationRole reports whether a stored role takes part in the
// conversation tree.
func conversationRole(role string) bool {
	return role == "user" || role == "assistant" || role == "tool"
}

// loadRows reads a session's messages (id, role, payload, created_at)
// in append order, decoded. Shared by the active-view machinery.
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

// loadViewRows reads the rows a PARENTED session's tree reads: every
// ancestor's rows (root first) precede the session's own rows - a branch
// inherits its parent's history as a prefix, the same view the TUI's
// /branch load renders (GetAncestorMessages). Sessions without a parent
// load just their own rows, unchanged. A missing ancestor (orphan chain
// after a hard delete) ends the walk - the visible part reads, matching
// SessionTree's orphan-root handling.
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
	// chain is leaf-to-root; ancestor rows prepend root-to-leaf.
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

// GetActiveView returns the session's tree-derived active view: the
// active path (context assembly + render order) and the branch-point
// metadata. Sessions written without pointers derive the same linear
// path they always rendered. Parented sessions (branch children,
// t_17365247's session fork) derive over the merged ancestor+own rows,
// so the child's active path IS the whole lineage.
func (s *Store) GetActiveView(ctx context.Context, sessionID string) (core.ActiveView, error) {
	if _, err := s.GetSession(ctx, sessionID); err != nil {
		return core.ActiveView{}, err
	}
	rows, err := s.loadViewRows(ctx, sessionID)
	if err != nil {
		return core.ActiveView{}, err
	}
	return core.DeriveTree(rows), nil
}

// GetActiveMessages returns the active path as plain messages - the
// context assembly and session-load seam.
func (s *Store) GetActiveMessages(ctx context.Context, sessionID string) ([]core.Message, error) {
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

// resolveParent computes the parent pointer a conversation message
// carries into its row: an explicitly set pointer on the message wins
// (validated against the session's merged view rows - a dangling pointer
// would truncate the chain), otherwise it derives to the active path's
// leaf (what the message follows/answers right now). The merged view
// (loadViewRows) includes ancestor rows for parented sessions, so a
// branch child's first message chains onto the ancestor leaf correctly.
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
