package store

// write.go: the store's write side. Every write lands in the session file FIRST - the
// files are the truth - and then folds the same line into the derived index inside one
// transaction. A crash between the two leaves the index behind the file, which is
// exactly what the per-file watermark's catch-up repairs.

import (
	"context"
	"fmt"
	"os"

	"github.com/EndoTheDev/omega/core"
)

// createSessionFile writes a session's header and indexes it. A session id that already
// exists is refused: creation is not an upsert.
func (s *Store) createSessionFile(ctx context.Context, id, parentID, delegateParent, label, agentName string, listed bool) error {
	if err := validSessionID(id); err != nil {
		return err
	}
	exists := s.sessionFileExists(id) || s.hasSession(ctx, id)
	if exists {
		return fmt.Errorf("store: session %q already exists", id)
	}
	if agentName == "" {
		agentName = "default"
	}
	// Lineage is a promise about a session that exists. The old schema enforced this with
	// a foreign key; the files carry it as a write-time check.
	for _, parent := range []string{parentID, delegateParent} {
		if parent != "" && !s.sessionFileExists(parent) {
			return fmt.Errorf("store: session %q: lineage parent %q does not exist", id, parent)
		}
	}
	now := core.NowISO()
	line := fileLine{
		V:              jsonlVersion,
		Kind:           kindSession,
		Session:        id,
		ParentID:       parentID,
		DelegateParent: delegateParent,
		Label:          label,
		CreatedAt:      now,
		UpdatedAt:      now,
		Agent:          agentName,
		Listed:         &listed,
		LastReadAt:     now,
	}
	return s.appendAndApply(ctx, id, line)
}

// appendMeta writes one metadata change and folds it into the index. An unknown session
// is a no-op, exactly as the old UPDATE that matched no row was.
func (s *Store) appendMeta(ctx context.Context, sessionID string, ch *changes) error {
	if ch == nil {
		return nil
	}
	if !s.sessionFileExists(sessionID) {
		return nil
	}
	line := fileLine{
		V:       jsonlVersion,
		Kind:    kindMeta,
		Session: sessionID,
		At:      core.NowISO(),
		Changes: ch,
	}
	return s.appendAndApply(ctx, sessionID, line)
}

// sessionFileExists reports whether the session has a file - the store's own existence
// test. The index is a cache and cannot be asked.
func (s *Store) sessionFileExists(id string) bool {
	path, err := s.sessionPath(id)
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

// appendMessageLine writes one message line with the next id of the store.
func (s *Store) appendMessageLine(ctx context.Context, sessionID, role string, payload []byte, at string) error {
	if !s.sessionFileExists(sessionID) {
		return fmt.Errorf("store: session %q does not exist", sessionID)
	}
	s.appendMu.Lock()
	defer s.appendMu.Unlock()
	seq, err := s.nextMessageSeq(ctx)
	if err != nil {
		return err
	}
	line := fileLine{
		V:       jsonlVersion,
		Kind:    kindMessage,
		Session: sessionID,
		ID:      seq,
		Role:    role,
		Payload: payload,
		At:      at,
	}
	path, err := s.sessionPath(sessionID)
	if err != nil {
		return err
	}
	wrote, err := appendLines(path, line)
	if err != nil {
		return err
	}
	return s.applyAndWatermark(ctx, path, sessionID, line, wrote)
}

// appendAndApply is the metadata path's shared tail.
func (s *Store) appendAndApply(ctx context.Context, sessionID string, line fileLine) error {
	s.appendMu.Lock()
	defer s.appendMu.Unlock()
	path, err := s.sessionPath(sessionID)
	if err != nil {
		return err
	}
	wrote, err := appendLines(path, line)
	if err != nil {
		return err
	}
	return s.applyAndWatermark(ctx, path, sessionID, line, wrote)
}

// applyAndWatermark folds one already-appended line into the index and advances the
// file's watermark when - and only when - the file is exactly where our append left it.
// A foreign append in between leaves the watermark alone; the next catch-up re-reads
// those bytes, and applyLine is idempotent.
func (s *Store) applyAndWatermark(ctx context.Context, path, id string, line fileLine, wrote int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := applyLine(ctx, tx, line); err != nil {
		return err
	}
	var from int64
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE((SELECT indexed_bytes FROM session_files WHERE session_id = ?), 0)`, id).
		Scan(&from); err != nil {
		return err
	}
	target := from + wrote
	if info, statErr := os.Stat(path); statErr == nil && info.Size() == target {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO session_files (session_id, indexed_bytes) VALUES (?, ?)
			 ON CONFLICT(session_id) DO UPDATE SET indexed_bytes = excluded.indexed_bytes`,
			id, target); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// deleteSessionFiles removes the session's file and every descendant's file. The old
// schema cascaded a delete through parent_id and delegate_parent; the files have to
// follow, or the next catch-up would resurrect the children as orphans.
func (s *Store) deleteSessionFiles(ctx context.Context, id string) error {
	victims, err := s.descendantIDs(ctx, id)
	if err != nil {
		return err
	}
	victims = append(victims, id)
	var firstErr error
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, victim := range victims {
		path, perr := s.sessionPath(victim)
		if perr != nil {
			if firstErr == nil {
				firstErr = perr
			}
			continue
		}
		if rerr := os.Remove(path); rerr != nil && !os.IsNotExist(rerr) {
			if firstErr == nil {
				firstErr = rerr
			}
			continue
		}
		if derr := dropSessionRows(ctx, tx, victim); derr != nil {
			return derr
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return firstErr
}

// descendantIDs walks the fork and delegate lineage below id.
func (s *Store) descendantIDs(ctx context.Context, id string) ([]string, error) {
	var out []string
	seen := map[string]bool{id: true}
	frontier := []string{id}
	for len(frontier) > 0 {
		var next []string
		for _, parent := range frontier {
			rows, err := s.db.QueryContext(ctx,
				`SELECT id FROM sessions WHERE parent_id = ? OR delegate_parent = ?`, parent, parent)
			if err != nil {
				return nil, err
			}
			for rows.Next() {
				var child string
				if err := rows.Scan(&child); err != nil {
					rows.Close()
					return nil, err
				}
				if seen[child] {
					continue
				}
				seen[child] = true
				out = append(out, child)
				next = append(next, child)
			}
			if err := rows.Err(); err != nil {
				rows.Close()
				return nil, err
			}
			rows.Close()
		}
		frontier = next
	}
	return out, nil
}
