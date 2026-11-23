package store

// write.go: the store's write side. Every write lands in the session file FIRST - the
// files are the truth - and then folds the same line into the derived index inside one
// transaction. A crash between the two leaves the index behind the file, which is
// exactly what the per-file watermark's catch-up repairs. To a caller the two halves are
// one write: an index that cannot take the line hands an error back and the line comes
// back out of the file.
//
// Everything here runs under writeMu, and so does every other mutation of the store -
// catch-up, rebuild, delete, rename. One lock, one writer: no goroutine reads a session
// file while another is landing a line in it, which is what makes a file scan safe.

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
	if agentName == "" {
		agentName = "default"
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	// The existence and lineage checks belong inside the lock: two creations of one id, or
	// a creation racing a delete, would otherwise both pass a check the other invalidated.
	exists := s.sessionFileExists(id) || s.hasSession(ctx, id)
	if exists {
		return fmt.Errorf("store: session %q already exists", id)
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
	return s.writeLine(ctx, id, line)
}

// appendMeta writes one metadata change and folds it into the index. An unknown session
// is a no-op, exactly as the old UPDATE that matched no row was.
func (s *Store) appendMeta(ctx context.Context, sessionID string, ch *changes) error {
	if ch == nil {
		return nil
	}
	line := fileLine{
		V:       jsonlVersion,
		Kind:    kindMeta,
		Session: sessionID,
		Changes: ch,
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	// Under the lock: a session deleted between the check and the append must not come back
	// as a stray file holding only a meta line.
	if !s.sessionFileExists(sessionID) {
		return nil
	}
	line.At = core.NowISO()
	return s.writeLine(ctx, sessionID, line)
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
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if !s.sessionFileExists(sessionID) {
		return fmt.Errorf("store: session %q does not exist", sessionID)
	}
	seq := s.nextMessageSeq()
	// The id is burned before the write. A failed append leaves a gap in the id space,
	// which no reader can see; a reused id is a collision every reader can.
	s.maxMessageID = seq
	line := fileLine{
		V:       jsonlVersion,
		Kind:    kindMessage,
		Session: sessionID,
		ID:      seq,
		Role:    role,
		Payload: payload,
		At:      at,
	}
	return s.writeLine(ctx, sessionID, line)
}

// writeLine appends one line to its session file and folds it into the index. Callers hold
// writeMu.
func (s *Store) writeLine(ctx context.Context, sessionID string, line fileLine) error {
	return s.writeLines(ctx, sessionID, []fileLine{line})
}

// writeLines appends a group of lines in ONE write and folds them into the index in one
// transaction. A group is a compact's carrier pair plus its block line: the file and the
// index learn them together or not at all. Callers hold writeMu.
func (s *Store) writeLines(ctx context.Context, sessionID string, lines []fileLine) error {
	path, err := s.sessionPath(sessionID)
	if err != nil {
		return err
	}
	before, wrote, err := appendLines(path, lines...)
	if err != nil {
		return err
	}
	if err := s.applyLinesAndWatermark(ctx, path, sessionID, lines, wrote); err != nil {
		return rollbackAppend(path, before, wrote, err)
	}
	return nil
}

// applyAndWatermark folds one already-appended line into the index and advances the
// file's watermark when - and only when - the file is exactly where our append left it.
// A foreign append in between leaves the watermark alone; the next catch-up re-reads
// those bytes, and applyLine is idempotent.
func (s *Store) applyAndWatermark(ctx context.Context, path, id string, line fileLine, wrote int64) error {
	return s.applyLinesAndWatermark(ctx, path, id, []fileLine{line}, wrote)
}

// applyLinesAndWatermark is applyAndWatermark for an appended group: every line is applied
// in one transaction, and the watermark moves by the group's total bytes.
func (s *Store) applyLinesAndWatermark(ctx context.Context, path, id string, lines []fileLine, wrote int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, line := range lines {
		if err := applyLine(ctx, tx, line); err != nil {
			return err
		}
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

// rollbackAppend takes back the bytes of a line whose index apply failed, so one append is
// all-or-nothing. The file is cut only while it still ends exactly where our write left
// it: anything else that grew it in between keeps its bytes and the next catch-up replays
// the whole span.
func rollbackAppend(path string, before, wrote int64, cause error) error {
	info, err := os.Stat(path)
	if err != nil || info.Size() != before+wrote {
		return cause
	}
	if terr := os.Truncate(path, before); terr != nil {
		return fmt.Errorf("store: append to %s: %w (and the line could not be dropped: %v)", path, cause, terr)
	}
	return cause
}

// deleteSessions removes the named sessions and every descendant's file. The old schema
// cascaded a delete through parent_id and delegate_parent; the files have to follow, or
// the next catch-up would resurrect the children as orphans.
func (s *Store) deleteSessions(ctx context.Context, ids []string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	lineage, err := s.sessionLineage()
	if err != nil {
		return err
	}
	victims := descendantSet(lineage, ids)
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

// descendantSet expands the roots to every session below them in either lineage.
func descendantSet(lineage map[string]lineageRow, roots []string) []string {
	children := make(map[string][]string, len(lineage))
	for id, row := range lineage {
		if row.parentID != "" {
			children[row.parentID] = append(children[row.parentID], id)
		}
		if row.delegateParent != "" {
			children[row.delegateParent] = append(children[row.delegateParent], id)
		}
	}
	seen := make(map[string]bool, len(lineage))
	queue := make([]string, 0, len(roots))
	for _, root := range roots {
		if root == "" || seen[root] {
			continue
		}
		seen[root] = true
		queue = append(queue, root)
	}
	out := make([]string, 0, len(queue))
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		out = append(out, id)
		for _, child := range children[id] {
			if seen[child] {
				continue
			}
			seen[child] = true
			queue = append(queue, child)
		}
	}
	return out
}
