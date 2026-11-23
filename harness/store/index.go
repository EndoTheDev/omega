package store

// index.go: the SQLite side, demoted from store to derived index.
//
// Everything here is reproducible from the JSONL session files: the sessions table is
// the folded metadata, the messages table is the message lines, both FTS tables are the
// search surface over them. session_files carries the per-file indexed-byte watermark,
// so boot and pre-search catch-up only read the bytes a file grew by. Delete the whole
// database and the next open rebuilds it from the files.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const indexSchema = `
CREATE TABLE IF NOT EXISTS sessions (
	id         TEXT PRIMARY KEY,
	parent_id  TEXT,
	delegate_parent TEXT,
	label      TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	prompt_tokens INTEGER NOT NULL DEFAULT 0,
	completion_tokens INTEGER NOT NULL DEFAULT 0,
	pinned INTEGER NOT NULL DEFAULT 0,
	archived INTEGER NOT NULL DEFAULT 0,
	title_state INTEGER NOT NULL DEFAULT 0,
	last_read_at TEXT NOT NULL DEFAULT '',
	agent TEXT NOT NULL DEFAULT 'default',
	last_run TEXT NOT NULL DEFAULT '',
	model TEXT NOT NULL DEFAULT '',
	provider TEXT NOT NULL DEFAULT '',
	workspace TEXT NOT NULL DEFAULT '',
	summary TEXT NOT NULL DEFAULT '',
	listed    INTEGER NOT NULL DEFAULT 1,
	context_start   TEXT NOT NULL DEFAULT '',
	context_summary TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS messages (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	session_id TEXT NOT NULL,
	seq        INTEGER NOT NULL,
	role       TEXT NOT NULL,
	payload    TEXT NOT NULL,
	created_at TEXT NOT NULL,
	UNIQUE(session_id, seq)
);
CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id, seq);

-- FTS5 full-text index over message content for /search. Column name 'payload'
-- matches the messages table column so FTS5 external content mode reads it directly.
CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
	session_id UNINDEXED,
	payload,
	content='messages',
	content_rowid='id'
);
CREATE TRIGGER IF NOT EXISTS messages_ai AFTER INSERT ON messages BEGIN
	INSERT INTO messages_fts(rowid, session_id, payload) VALUES (new.id, new.session_id, new.payload);
END;
CREATE TRIGGER IF NOT EXISTS messages_ad AFTER DELETE ON messages BEGIN
	INSERT INTO messages_fts(messages_fts, rowid, session_id, payload) VALUES ('delete', old.id, old.session_id, old.payload);
END;
CREATE TRIGGER IF NOT EXISTS messages_au AFTER UPDATE ON messages BEGIN
	INSERT INTO messages_fts(messages_fts, rowid, session_id, payload) VALUES ('delete', old.id, old.session_id, old.payload);
	INSERT INTO messages_fts(rowid, session_id, payload) VALUES (new.id, new.session_id, new.payload);
END;

-- FTS5 index over session labels and hygiene summaries - the title and summary halves
-- of the unified /sessions/search. Standalone (no external content): one row per
-- session, kept in sync by these triggers on INSERT, label UPDATE, summary UPDATE and
-- DELETE. Labels change in three places (manual rename, auto-title, creation);
-- summaries arrive from the hygiene pass - trigger coverage beats a hook at each call
-- site.
CREATE VIRTUAL TABLE IF NOT EXISTS sessions_fts USING fts5(
	session_id UNINDEXED,
	label,
	summary
);
CREATE TRIGGER IF NOT EXISTS sessions_ai AFTER INSERT ON sessions BEGIN
	INSERT INTO sessions_fts(session_id, label, summary)
	VALUES (new.id, new.label, new.summary);
END;
-- The label and summary triggers both read the OTHER column from the sessions row:
-- rewriting either field reindexes both, so a rename never drops the summary from the
-- index (and the other way around).
CREATE TRIGGER IF NOT EXISTS sessions_au AFTER UPDATE OF label ON sessions BEGIN
	DELETE FROM sessions_fts WHERE session_id = old.id;
	INSERT INTO sessions_fts(session_id, label, summary)
	VALUES (new.id, new.label, (SELECT summary FROM sessions WHERE id = new.id));
END;
CREATE TRIGGER IF NOT EXISTS sessions_asum AFTER UPDATE OF summary ON sessions BEGIN
	DELETE FROM sessions_fts WHERE session_id = old.id;
	INSERT INTO sessions_fts(session_id, label, summary)
	VALUES (new.id, (SELECT label FROM sessions WHERE id = new.id), new.summary);
END;
CREATE TRIGGER IF NOT EXISTS sessions_ad AFTER DELETE ON sessions BEGIN
	DELETE FROM sessions_fts WHERE session_id = old.id;
END;
-- Plain DELETE FROM (not the fts5 'delete' special command): as of SQLite 3.53 that
-- command errors on regular tables - it only works for contentless/external-content
-- indexes (messages_fts uses it there because its content table backs the index). A
-- regular fts5 row deletes through SQL like any table.

-- The per-file indexed-byte watermark: catch-up reads only the bytes a file grew by.
CREATE TABLE IF NOT EXISTS session_files (
	session_id    TEXT PRIMARY KEY,
	indexed_bytes INTEGER NOT NULL DEFAULT 0
);

-- Image captions keyed by content hash (vision_model knob). Captions are shared across
-- sessions by design: one row per distinct image.
CREATE TABLE IF NOT EXISTS image_captions (
	image_id   TEXT PRIMARY KEY,
	caption    TEXT NOT NULL,
	created_at TEXT NOT NULL
);
-- The pending-calls table: yield-class tool calls dispatched and awaiting their result.
-- Durable across restarts - the serve-start boot recovery sweep reads it (All) and
-- injects a synthetic timeout result per entry. Rows are keyed (session, call id);
-- Settle deletes.
CREATE TABLE IF NOT EXISTS pending_calls (
	session_id    TEXT NOT NULL,
	call_id       TEXT NOT NULL,
	tool_name     TEXT NOT NULL,
	arguments     TEXT NOT NULL DEFAULT '',
	dispatched_at TEXT NOT NULL,
	PRIMARY KEY (session_id, call_id)
);
`

// ensureIndexSchema creates the derived index tables. Every one of them is rebuildable
// from the session files, so the schema carries no migration ladder: a database from a
// previous shape is either a legacy STORE (migrated to files, then archived) or absent.
func (s *Store) ensureIndexSchema() error {
	if _, err := s.db.Exec(indexSchema); err != nil {
		return err
	}
	// Standalone FTS tables have no 'rebuild': repopulate the label/summary index from
	// the rows on every open. One row per session, so the cost is a scan we already pay.
	// The error is NOT discarded: a silent failure here degrades label and summary search
	// with nothing in the log to say why.
	if _, err := s.db.Exec(`DELETE FROM sessions_fts;
INSERT INTO sessions_fts(session_id, label, summary) SELECT id, label, summary FROM sessions;`); err != nil {
		return fmt.Errorf("store: repopulate label index: %w", err)
	}
	return nil
}

// catchUp indexes what the files grew by. Per file: a size equal to the watermark is
// skipped; a size beyond it reads from the watermark; a size BELOW it means the file was
// rewritten (or truncated), so the whole file is re-read. A file that vanished takes its
// derived rows with it. The pass holds writeMu, the same lock every append holds, so no
// file is read while a line is landing in it.
func (s *Store) catchUp(ctx context.Context) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.catchUpLocked(ctx)
}

func (s *Store) catchUpLocked(ctx context.Context) error {
	entries, err := os.ReadDir(s.sessionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("store: read sessions dir: %w", err)
	}
	present := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".jsonl")
		present[id] = true
		info, ierr := entry.Info()
		if ierr != nil {
			continue // raced away between ReadDir and Info: the next pass prunes it
		}
		from, wmErr := s.watermark(ctx, id)
		if wmErr != nil {
			return wmErr
		}
		switch {
		case info.Size() == from:
			continue
		case info.Size() < from:
			from = 0
		}
		if err := s.indexFile(ctx, id, filepath.Join(s.sessionsDir, entry.Name()), from); err != nil {
			return err
		}
	}
	return s.pruneVanished(ctx, present)
}

// watermark reports the bytes already indexed for one session file.
func (s *Store) watermark(ctx context.Context, id string) (int64, error) {
	var from int64
	err := s.db.QueryRowContext(ctx,
		`SELECT indexed_bytes FROM session_files WHERE session_id = ?`, id).Scan(&from)
	if err != nil && err != sql.ErrNoRows {
		return 0, err
	}
	return from, nil
}

// indexFile applies one file's lines from offset `from`, then records the new watermark.
// A re-read from 0 drops the session's derived rows first, so the re-fold can never
// double-count.
func (s *Store) indexFile(ctx context.Context, id, path string, from int64) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()
	if _, err := f.Seek(from, 0); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if from == 0 {
		if err := dropSessionRows(ctx, tx, id); err != nil {
			return err
		}
	}
	end, _, err := scanJSONL(f, func(raw []byte, _ int64) error {
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
		return fmt.Errorf("store: index %s: %w", id, err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO session_files (session_id, indexed_bytes) VALUES (?, ?)
		 ON CONFLICT(session_id) DO UPDATE SET indexed_bytes = excluded.indexed_bytes`,
		id, from+end); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	// A tail seen here is never cut. The files are only written under writeMu, which this
	// pass holds, so an unterminated tail is residue from a crash - and the boot pass
	// already dropped every one of those before the store was shared. Cutting a file from
	// the index path is how an acknowledged line gets deleted.
	return nil
}

// execer is what both *sql.DB and *sql.Tx offer: the index apply needs nothing else.
type execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// dropSessionRows clears one session's derived rows: its messages, its row, and its
// watermark.
func dropSessionRows(ctx context.Context, exec execer, id string) error {
	if _, err := exec.ExecContext(ctx, `DELETE FROM messages WHERE session_id = ?`, id); err != nil {
		return err
	}
	if _, err := exec.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id); err != nil {
		return err
	}
	_, err := exec.ExecContext(ctx, `DELETE FROM session_files WHERE session_id = ?`, id)
	return err
}

// pruneVanished drops derived rows for files that no longer exist on disk.
func (s *Store) pruneVanished(ctx context.Context, present map[string]bool) error {
	rows, err := s.db.QueryContext(ctx, `SELECT session_id FROM session_files`)
	if err != nil {
		return err
	}
	var gone []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		if !present[id] {
			gone = append(gone, id)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, id := range gone {
		if err := dropSessionRows(ctx, tx, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// RebuildIndex is the corruption-recovery verb: throw the whole derived index away and
// build it again from the session files. Search results after a rebuild are identical to
// the ones before it, because both are the same fold of the same files.
func (s *Store) RebuildIndex(ctx context.Context) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	for _, stmt := range []string{
		`DELETE FROM messages`,
		`DELETE FROM sessions`,
		`DELETE FROM session_files`,
	} {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	// The label/summary index is trigger-maintained; clear the leftovers explicitly.
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions_fts`); err != nil {
		return err
	}
	return s.catchUpLocked(ctx)
}

// applyLine folds one JSONL line into the index. Every statement is idempotent: a
// catch-up that re-reads bytes the live path already applied must not double-count.
func applyLine(ctx context.Context, exec execer, line fileLine) error {
	switch line.Kind {
	case kindSession:
		listed := 1
		if line.Listed != nil && !*line.Listed {
			listed = 0
		}
		agentName := line.Agent
		if agentName == "" {
			agentName = "default"
		}
		_, err := exec.ExecContext(ctx,
			`INSERT INTO sessions (id, parent_id, delegate_parent, label, created_at, updated_at, last_read_at, agent, listed)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(id) DO NOTHING`,
			line.Session, nullIfEmpty(line.ParentID), nullIfEmpty(line.DelegateParent), line.Label,
			line.CreatedAt, line.UpdatedAt, line.LastReadAt, agentName, listed)
		return err
	case kindMessage:
		if _, err := exec.ExecContext(ctx,
			`INSERT INTO messages (session_id, seq, role, payload, created_at)
			 VALUES (?, ?, ?, ?, ?) ON CONFLICT(session_id, seq) DO NOTHING`,
			line.Session, line.ID, line.Role, string(line.Payload), line.At); err != nil {
			return err
		}
		// An append IS activity; the line's stamp is the message's own created_at.
		_, err := exec.ExecContext(ctx,
			`UPDATE sessions SET updated_at = ? WHERE id = ?`, line.At, line.Session)
		return err
	case kindMeta:
		return applyChangesToIndex(ctx, exec, line)
	case kindBlock:
		// The tier structure lives in the files only; the index serves lists and search,
		// which never read blocks. Nothing to apply.
		return nil
	}
	return nil
}

// applyChangesToIndex writes one meta line's fields onto the session row.
func applyChangesToIndex(ctx context.Context, exec execer, line fileLine) error {
	ch := line.Changes
	if ch == nil {
		return nil
	}
	var (
		sets []string
		args []any
	)
	set := func(col string, v any) {
		sets = append(sets, col+" = ?")
		args = append(args, v)
	}
	if ch.Label != nil {
		set("label", *ch.Label)
	}
	if ch.Pinned != nil {
		set("pinned", boolToInt(*ch.Pinned))
	}
	if ch.Archived != nil {
		set("archived", boolToInt(*ch.Archived))
	}
	if ch.TitleState != nil {
		set("title_state", *ch.TitleState)
	}
	if ch.LastReadAt != nil {
		set("last_read_at", *ch.LastReadAt)
	}
	if ch.Agent != nil {
		set("agent", *ch.Agent)
	}
	if ch.Model != nil {
		set("model", *ch.Model)
	}
	if ch.Provider != nil {
		set("provider", *ch.Provider)
	}
	if ch.Workspace != nil {
		set("workspace", *ch.Workspace)
	}
	if ch.Summary != nil {
		set("summary", *ch.Summary)
	}
	if ch.Listed != nil {
		set("listed", boolToInt(*ch.Listed))
	}
	if ch.ParentID != nil {
		set("parent_id", nullIfEmpty(*ch.ParentID))
	}
	if ch.DelegateParent != nil {
		set("delegate_parent", nullIfEmpty(*ch.DelegateParent))
	}
	if ch.ContextStart != nil {
		set("context_start", *ch.ContextStart)
	}
	if ch.ContextSummary != nil {
		set("context_summary", *ch.ContextSummary)
	}
	if ch.LastRun != nil {
		set("last_run", *ch.LastRun)
	}
	if ch.UpdatedAt != nil {
		set("updated_at", *ch.UpdatedAt)
	}
	if ch.AddPrompt != 0 {
		sets = append(sets, "prompt_tokens = prompt_tokens + ?")
		args = append(args, ch.AddPrompt)
	}
	if ch.AddCompletion != 0 {
		sets = append(sets, "completion_tokens = completion_tokens + ?")
		args = append(args, ch.AddCompletion)
	}
	if len(sets) == 0 {
		return nil
	}
	args = append(args, line.Session)
	_, err := exec.ExecContext(ctx,
		`UPDATE sessions SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...)
	return err
}

func nullIfEmpty(v string) any {
	if v == "" {
		return nil
	}
	return v
}

// nextMessageSeq is the id a new message line takes. The ceiling is the truth's, learned
// from the session files at boot and only ever moving forward. The index cannot answer
// for it: an apply that failed after the line landed would otherwise let the next append
// reuse an id a file already holds - a silent drop in one session's index rows, or an
// ancestor-merge collision across two. Callers hold writeMu.
func (s *Store) nextMessageSeq() int64 {
	return s.maxMessageID + 1
}

// sessionsOnDisk lists the session files, newest name last in directory order.
func (s *Store) sessionsOnDisk() ([]string, error) {
	entries, err := os.ReadDir(s.sessionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		out = append(out, strings.TrimSuffix(entry.Name(), ".jsonl"))
	}
	return out, nil
}
