package store

// rename.go: renaming ONE session id inside ONE store.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RenameSession moves sessionID to newID inside the store at dsn: the session file, its
// derived rows, its pending calls, and every child lineage pointer that names it.
//
// No-ops, each for its own honest reason:
//
//	the store holds no data: an agent home that never ran has nothing to rename, and
//	    opening it would create the store it lacks;
//	oldID is absent: already renamed (idempotent, so every boot may call it);
//	the target id already exists: never merge two chats into one. The caller decides
//	    what that means; this function invents nothing.
func RenameSession(dsn, oldID, newID string) error {
	if oldID == "" || newID == "" || oldID == newID {
		return nil
	}
	if err := validSessionID(newID); err != nil {
		return err
	}
	if !homeHasStore(dsn) {
		return nil
	}
	st, err := Open(dsn)
	if err != nil {
		return fmt.Errorf("rename session: %w", err)
	}
	defer st.Close()
	return st.renameSession(context.Background(), oldID, newID)
}

func (s *Store) renameSession(ctx context.Context, oldID, newID string) error {
	oldPath, err := s.sessionPath(oldID)
	if err != nil {
		return err
	}
	newPath, err := s.sessionPath(newID)
	if err != nil {
		return err
	}
	if _, err := os.Stat(oldPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("rename session: %w", err)
	}
	if _, err := os.Stat(newPath); err == nil {
		return nil
	}
	children, err := s.childrenOf(ctx, oldID)
	if err != nil {
		return err
	}
	// The file is rewritten under the new id: every line names its session, so every
	// line changes. A maintenance rewrite, not an append - the whole file is replaced.
	if err := rewriteSessionID(oldPath, newPath, oldID, newID); err != nil {
		return err
	}
	if err := os.Remove(oldPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("rename session: %w", err)
	}

	if err := s.renameIndex(ctx, oldID, newID, children); err != nil {
		return err
	}
	// The children's lineage pointers live in their own files: append the correction.
	for _, child := range children {
		if err := s.appendMeta(ctx, child.id, child.changeTo(newID)); err != nil {
			return err
		}
	}
	return nil
}

// renameIndex swaps the derived rows: the old id's rows go, the new file is indexed whole,
// and the pending calls follow the session. Children are detached BEFORE the old row is
// deleted: the index carries no foreign keys, so a detach is what keeps an in-flight row
// from pointing at an id that no longer exists.
func (s *Store) renameIndex(ctx context.Context, oldID, newID string, children []childLink) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `UPDATE sessions SET parent_id = NULL WHERE parent_id = ?`, oldID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sessions SET delegate_parent = NULL WHERE delegate_parent = ?`, oldID); err != nil {
		return err
	}
	if err := dropSessionRows(ctx, tx, oldID); err != nil {
		return err
	}
	if err := s.indexFileTx(ctx, tx, s.sessionsDir, newID); err != nil {
		return err
	}
	for _, child := range children {
		column := "delegate_parent"
		if child.parent {
			column = "parent_id"
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE sessions SET `+column+` = ? WHERE id = ?`, newID, child.id); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE pending_calls SET session_id = ? WHERE session_id = ?`, newID, oldID); err != nil {
		return err
	}
	return tx.Commit()
}

// childLink names one lineage pointer that points at the renamed session.
type childLink struct {
	id     string
	parent bool
}

func (l childLink) changeTo(newID string) *changes {
	v := newID
	if l.parent {
		return &changes{ParentID: &v}
	}
	return &changes{DelegateParent: &v}
}

// childrenOf lists the sessions naming id in either lineage column.
func (s *Store) childrenOf(ctx context.Context, id string) ([]childLink, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, parent_id FROM sessions WHERE parent_id = ? OR delegate_parent = ?`, id, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []childLink
	for rows.Next() {
		var child string
		var parent sql.NullString
		if err := rows.Scan(&child, &parent); err != nil {
			return nil, err
		}
		out = append(out, childLink{id: child, parent: parent.Valid && parent.String == id})
	}
	return out, rows.Err()
}

// rewriteSessionID writes src's lines to dst with every line's session pointer set to
// newID. Payload bytes are carried exactly as stored - the rename changes the lineage, and
// nothing else, which means it goes through the same no-escape encoder an append does.
func rewriteSessionID(src, dst, oldID, newID string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("rename session: read %s: %w", oldID, err)
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	var out strings.Builder
	for _, raw := range lines {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		var line fileLine
		if err := json.Unmarshal([]byte(raw), &line); err != nil {
			return fmt.Errorf("rename session: %s: %w", oldID, err)
		}
		line.Session = newID
		b, err := encodeLine(&line)
		if err != nil {
			return fmt.Errorf("rename session: %s: %w", oldID, err)
		}
		out.Write(b)
	}
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, []byte(out.String()), 0o600); err != nil {
		return fmt.Errorf("rename session: write %s: %w", newID, err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		return fmt.Errorf("rename session: %w", err)
	}
	return nil
}

// indexFileTx indexes one whole session file inside an open transaction.
func (s *Store) indexFileTx(ctx context.Context, tx execer, sessionsDir, id string) error {
	path := filepath.Join(sessionsDir, id+".jsonl")
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	end, torn, err := scanJSONL(f, func(raw []byte, _ int64) error {
		var line fileLine
		if jerr := json.Unmarshal(raw, &line); jerr != nil {
			return errCorruptLine
		}
		if line.Session == "" {
			line.Session = id
		}
		if line.ID > s.maxMessageID {
			s.maxMessageID = line.ID
		}
		return applyLine(ctx, tx, line)
	})
	if err != nil {
		return err
	}
	if torn {
		// rewriteSessionID wrote this file line by line, so a short tail here is not a
		// crash: it means the file we are indexing is not the one we wrote.
		return fmt.Errorf("store: renamed file %s ends mid-line", id)
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO session_files (session_id, indexed_bytes) VALUES (?, ?)
		 ON CONFLICT(session_id) DO UPDATE SET indexed_bytes = excluded.indexed_bytes`, id, end)
	return err
}
