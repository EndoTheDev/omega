package store

// legacy.go: the one-way migration out of the SQLite STORE and into the JSONL files.
//
// Guarded by read-back: a session is migrated only once the files read back through the
// store's own reader, byte-identical to the rows they came from. Nothing is renamed, and
// the old database is never touched, until every session in it verifies. The archived
// copy is ONE dated .bak beside the index - kept for the rollback window, deleted at the
// next stable tag, never kept forever and never deleted on the day it was made.

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/EndoTheDev/omega/core"
)

// legacyColumns are the optional session columns a pre-JSONL store may or may not carry,
// oldest shape first. A column absent from the file reads as its zero value, which is
// exactly what the old ALTER-and-backfill ladder produced.
var legacyColumns = []struct {
	name string
	typ  string
}{
	{"parent_id", "TEXT"},
	{"delegate_parent", "TEXT"},
	{"label", "TEXT"},
	{"prompt_tokens", "INTEGER"},
	{"completion_tokens", "INTEGER"},
	{"pinned", "INTEGER"},
	{"archived", "INTEGER"},
	{"title_state", "INTEGER"},
	{"last_read_at", "TEXT"},
	{"agent", "TEXT"},
	{"last_run", "TEXT"},
	{"model", "TEXT"},
	{"provider", "TEXT"},
	{"workspace", "TEXT"},
	{"summary", "TEXT"},
	{"listed", "INTEGER"},
	{"context_start", "TEXT"},
	{"context_summary", "TEXT"},
}

// legacyCarry is the non-session state a legacy store held. Captions and pending calls
// are not session content: they move forward into the fresh index as-is.
type legacyCarry struct {
	captions []legacyCaption
	pending  []legacyPending
}

type legacyCaption struct {
	imageID   string
	caption   string
	createdAt string
}

type legacyPending struct {
	sessionID    string
	callID       string
	toolName     string
	arguments    string
	dispatchedAt string
}

// isLegacyStore reports whether dsn is a pre-JSONL store: it has a sessions table and no
// session_files watermark table. An index-era database answers false, an absent file
// answers false, and a brand-new empty database answers false.
func isLegacyStore(dsn string) bool {
	if dsn == "" || dsn == ":memory:" {
		return false
	}
	if _, err := os.Stat(dsn); err != nil {
		return false
	}
	db, err := sql.Open("sqlite", dsn+"?mode=ro&_busy_timeout=5000")
	if err != nil {
		return false
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='sessions'`).Scan(&n); err != nil || n == 0 {
		return false
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='session_files'`).Scan(&n); err == nil && n > 0 {
		return false
	}
	return true
}

// migrateLegacyStore writes every session in the legacy store to its JSONL file, reads
// each one back, and only then archives the store. A verification failure deletes what
// it wrote and returns the error: a half-migrated store is worse than an unmigrated one.
func migrateLegacyStore(dsn, sessionsDir string) (legacyCarry, error) {
	var carry legacyCarry
	db, err := sql.Open("sqlite", dsn+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return carry, fmt.Errorf("legacy migration: open %s: %w", dsn, err)
	}
	defer db.Close()
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		return carry, fmt.Errorf("legacy migration: sessions dir: %w", err)
	}

	written := []string{}
	fail := func(err error) (legacyCarry, error) {
		for _, path := range written {
			_ = os.Remove(path)
		}
		return carry, err
	}

	ids, err := legacySessionIDs(db)
	if err != nil {
		return fail(err)
	}
	for _, id := range ids {
		path, perr := legacyWriteSession(db, sessionsDir, id)
		if perr != nil {
			return fail(perr)
		}
		written = append(written, path)
	}
	// Read back through the same reader the store uses. Nothing counts as migrated until
	// this passes for EVERY session.
	for _, id := range ids {
		if err := legacyVerifySession(db, sessionsDir, id); err != nil {
			return fail(err)
		}
	}
	carry, err = legacyReadCarry(db)
	if err != nil {
		return fail(err)
	}
	return carry, nil
}

// legacySessionIDs lists every session id in the store, oldest first.
func legacySessionIDs(db *sql.DB) ([]string, error) {
	rows, err := db.Query(`SELECT id FROM sessions ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("legacy migration: list sessions: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("legacy migration: scan session id: %w", err)
		}
		if err := validSessionID(id); err != nil {
			return nil, fmt.Errorf("legacy migration: session %q cannot become a file name: %w", id, err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// legacySessionRow is one migrated session, in the store's own terms.
type legacySessionRow struct {
	session  core.Session
	messages []storedMsg
}

// legacyReadSession reads one session and its messages out of the legacy store, filling
// absent columns with their zero value.
func legacyReadSession(db *sql.DB, id string) (legacySessionRow, error) {
	var row legacySessionRow
	cols, err := legacyPresentColumns(db)
	if err != nil {
		return row, err
	}
	scan := make([]any, 0, len(cols))
	vals := make([]sql.NullString, len(cols))
	for i := range cols {
		scan = append(scan, &vals[i])
	}
	query := `SELECT ` + strings.Join(cols, ", ") + ` FROM sessions WHERE id = ?`
	if err := db.QueryRow(query, id).Scan(scan...); err != nil {
		return row, fmt.Errorf("legacy migration: read session %s: %w", id, err)
	}
	get := func(name string) string {
		for i, c := range cols {
			if c == name {
				return vals[i].String
			}
		}
		return ""
	}
	getInt := func(name string) int {
		var n int
		_, _ = fmt.Sscanf(get(name), "%d", &n)
		return n
	}
	sess := core.Session{
		ID:               id,
		ParentID:         get("parent_id"),
		DelegateParent:   get("delegate_parent"),
		Label:            get("label"),
		CreatedAt:        get("created_at"),
		UpdatedAt:        get("updated_at"),
		PromptTokens:     getInt("prompt_tokens"),
		CompletionTokens: getInt("completion_tokens"),
		Pinned:           getInt("pinned") != 0,
		Archived:         getInt("archived") != 0,
		TitleState:       getInt("title_state"),
		LastReadAt:       get("last_read_at"),
		Agent:            get("agent"),
		Model:            get("model"),
		ProviderName:     get("provider"),
		Workspace:        get("workspace"),
		Summary:          get("summary"),
		ContextStart:     get("context_start"),
		ContextSummary:   get("context_summary"),
	}
	// The pre-agent rows and every writer that omitted the binding read as the default,
	// exactly as scanSession normalizes them.
	if sess.Agent == "" {
		sess.Agent = "default"
	}
	// Two columns arrived with backfills, and a store older than them must migrate to the
	// same values the old ALTER ladder wrote: a labeled row is a FINAL title (2, never
	// retitled), and an absent read stamp is the last activity.
	if get("title_state") == "" && sess.Label != "" {
		sess.TitleState = 2
	}
	if get("last_read_at") == "" {
		sess.LastReadAt = sess.UpdatedAt
	}
	if listed := get("listed"); listed != "" {
		var n int
		_, _ = fmt.Sscanf(listed, "%d", &n)
		sess.MachineOwned = n == 0
	}
	if lastRun := get("last_run"); lastRun != "" {
		var stats core.LastRunStats
		if json.Unmarshal([]byte(lastRun), &stats) == nil {
			sess.LastRun = &stats
		}
	}
	row.session = sess

	if !legacyTableExists(db, "messages") {
		return row, nil // a store that never wrote a message has no message table to read
	}
	rows, err := db.Query(`SELECT id, role, payload, created_at FROM messages WHERE session_id = ? ORDER BY id`, id)
	if err != nil {
		return row, fmt.Errorf("legacy migration: read messages of %s: %w", id, err)
	}
	defer rows.Close()
	for rows.Next() {
		var m storedMsg
		var payload string
		if err := rows.Scan(&m.ID, &m.Role, &payload, &m.CreatedAt); err != nil {
			return row, fmt.Errorf("legacy migration: scan message of %s: %w", id, err)
		}
		m.Payload = []byte(payload)
		row.messages = append(row.messages, m)
	}
	return row, rows.Err()
}

// legacyPresentColumns is the optional session column set this database actually carries.
func legacyPresentColumns(db *sql.DB) ([]string, error) {
	rows, err := db.Query(`SELECT name FROM pragma_table_info('sessions')`)
	if err != nil {
		return nil, fmt.Errorf("legacy migration: session columns: %w", err)
	}
	defer rows.Close()
	have := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		have[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	cols := []string{"id", "created_at", "updated_at"}
	for _, c := range legacyColumns {
		if have[c.name] {
			cols = append(cols, c.name)
		}
	}
	return cols, nil
}

// legacyWriteSession writes one session's file from scratch and returns its path.
func legacyWriteSession(db *sql.DB, sessionsDir, id string) (string, error) {
	row, err := legacyReadSession(db, id)
	if err != nil {
		return "", err
	}
	path := filepath.Join(sessionsDir, id+".jsonl")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return "", err
	}
	listed := !row.session.MachineOwned
	lines := []fileLine{{
		V:              jsonlVersion,
		Kind:           kindSession,
		Session:        id,
		ParentID:       row.session.ParentID,
		DelegateParent: row.session.DelegateParent,
		Label:          row.session.Label,
		CreatedAt:      row.session.CreatedAt,
		UpdatedAt:      row.session.UpdatedAt,
		Agent:          row.session.Agent,
		Listed:         &listed,
		LastReadAt:     row.session.LastReadAt,
	}}
	for _, m := range row.messages {
		lines = append(lines, fileLine{
			V:       jsonlVersion,
			Kind:    kindMessage,
			Session: id,
			ID:      m.ID,
			Role:    m.Role,
			Payload: json.RawMessage(m.Payload),
			At:      m.CreatedAt,
		})
	}
	// The rest of the row rides one meta line: it is the same fold a live store produces
	// from its setters, so the read-back compares like for like.
	ch := migratedChanges(row.session)
	if ch != nil {
		lines = append(lines, fileLine{
			V: jsonlVersion, Kind: kindMeta, Session: id, At: row.session.UpdatedAt, Changes: ch,
		})
	}
	if _, err := appendLines(path, lines...); err != nil {
		return path, fmt.Errorf("legacy migration: write %s: %w", id, err)
	}
	return path, nil
}

// migratedChanges is the meta line carrying every field the header does not hold.
func migratedChanges(sess core.Session) *changes {
	ch := &changes{}
	if sess.PromptTokens != 0 || sess.CompletionTokens != 0 {
		ch.AddPrompt = sess.PromptTokens
		ch.AddCompletion = sess.CompletionTokens
	}
	if sess.Pinned {
		v := true
		ch.Pinned = &v
	}
	if sess.Archived {
		v := true
		ch.Archived = &v
	}
	if sess.TitleState != 0 {
		v := sess.TitleState
		ch.TitleState = &v
	}
	if sess.Model != "" {
		ch.Model = &sess.Model
	}
	if sess.ProviderName != "" {
		ch.Provider = &sess.ProviderName
	}
	if sess.Workspace != "" {
		ch.Workspace = &sess.Workspace
	}
	if sess.Summary != "" {
		ch.Summary = &sess.Summary
	}
	if sess.ContextStart != "" {
		ch.ContextStart = &sess.ContextStart
	}
	if sess.ContextSummary != "" {
		ch.ContextSummary = &sess.ContextSummary
	}
	if sess.LastRun != nil {
		if b, err := json.Marshal(sess.LastRun); err == nil {
			v := string(b)
			ch.LastRun = &v
		}
	}
	if *ch == (changes{}) {
		return nil
	}
	return ch
}

// legacyVerifySession reads the written file back and compares it to the source rows,
// field by field and byte for byte. This is the migration's gate: a session that does
// not read back exactly is not migrated.
func legacyVerifySession(db *sql.DB, sessionsDir, id string) error {
	want, err := legacyReadSession(db, id)
	if err != nil {
		return err
	}
	got, err := foldFile(filepath.Join(sessionsDir, id+".jsonl"))
	if err != nil {
		return fmt.Errorf("legacy migration: read back %s: %w", id, err)
	}
	if got.torn {
		return fmt.Errorf("legacy migration: %s reads back with a torn tail", id)
	}
	if err := compareSession(want.session, got.session); err != nil {
		return fmt.Errorf("legacy migration: read back %s: %w", id, err)
	}
	if len(want.messages) != len(got.messages) {
		return fmt.Errorf("legacy migration: read back %s: %d messages, source has %d",
			id, len(got.messages), len(want.messages))
	}
	for i := range want.messages {
		w, g := want.messages[i], got.messages[i]
		if w.ID != g.ID {
			return fmt.Errorf("legacy migration: read back %s message %d: id %d, want %d", id, i, g.ID, w.ID)
		}
		if w.Role != g.Role {
			return fmt.Errorf("legacy migration: read back %s message %d: role %q, want %q", id, i, g.Role, w.Role)
		}
		if w.CreatedAt != g.CreatedAt {
			return fmt.Errorf("legacy migration: read back %s message %d: created_at %q, want %q", id, i, g.CreatedAt, w.CreatedAt)
		}
		if !bytes.Equal(w.Payload, g.Payload) {
			return fmt.Errorf("legacy migration: read back %s message %d: payload differs", id, i)
		}
	}
	return nil
}

// compareSession diffs the metadata a migration must carry across untouched.
func compareSession(want, got core.Session) error {
	type field struct {
		name      string
		want, got any
	}
	for _, f := range []field{
		{"id", want.ID, got.ID},
		{"parent_id", want.ParentID, got.ParentID},
		{"delegate_parent", want.DelegateParent, got.DelegateParent},
		{"label", want.Label, got.Label},
		{"created_at", want.CreatedAt, got.CreatedAt},
		{"updated_at", want.UpdatedAt, got.UpdatedAt},
		{"prompt_tokens", want.PromptTokens, got.PromptTokens},
		{"completion_tokens", want.CompletionTokens, got.CompletionTokens},
		{"pinned", want.Pinned, got.Pinned},
		{"archived", want.Archived, got.Archived},
		{"title_state", want.TitleState, got.TitleState},
		{"last_read_at", want.LastReadAt, got.LastReadAt},
		{"agent", want.Agent, got.Agent},
		{"model", want.Model, got.Model},
		{"provider", want.ProviderName, got.ProviderName},
		{"workspace", want.Workspace, got.Workspace},
		{"summary", want.Summary, got.Summary},
		{"machine_owned", want.MachineOwned, got.MachineOwned},
		{"context_start", want.ContextStart, got.ContextStart},
		{"context_summary", want.ContextSummary, got.ContextSummary},
	} {
		if fmt.Sprint(f.want) != fmt.Sprint(f.got) {
			return fmt.Errorf("%s = %v, want %v", f.name, f.got, f.want)
		}
	}
	if fmt.Sprint(lastRunKey(want.LastRun)) != fmt.Sprint(lastRunKey(got.LastRun)) {
		return fmt.Errorf("last_run = %v, want %v", got.LastRun, want.LastRun)
	}
	return nil
}

func lastRunKey(s *core.LastRunStats) string {
	if s == nil {
		return ""
	}
	return fmt.Sprintf("%d/%d", s.TokensPerSecond, s.ChunksPerSecond)
}

// legacyReadCarry lifts the non-session tables the fresh index keeps.
func legacyReadCarry(db *sql.DB) (legacyCarry, error) {
	var carry legacyCarry
	if legacyTableExists(db, "image_captions") {
		rows, err := db.Query(`SELECT image_id, caption, created_at FROM image_captions`)
		if err != nil {
			return carry, fmt.Errorf("legacy migration: read captions: %w", err)
		}
		for rows.Next() {
			var c legacyCaption
			if err := rows.Scan(&c.imageID, &c.caption, &c.createdAt); err != nil {
				rows.Close()
				return carry, err
			}
			carry.captions = append(carry.captions, c)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return carry, err
		}
		rows.Close()
	}
	if legacyTableExists(db, "pending_calls") {
		rows, err := db.Query(`SELECT session_id, call_id, tool_name, arguments, dispatched_at FROM pending_calls`)
		if err != nil {
			return carry, fmt.Errorf("legacy migration: read pending calls: %w", err)
		}
		for rows.Next() {
			var p legacyPending
			if err := rows.Scan(&p.sessionID, &p.callID, &p.toolName, &p.arguments, &p.dispatchedAt); err != nil {
				rows.Close()
				return carry, err
			}
			carry.pending = append(carry.pending, p)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return carry, err
		}
		rows.Close()
	}
	return carry, nil
}

func legacyTableExists(db *sql.DB, name string) bool {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&n)
	return err == nil && n > 0
}

// archiveLegacyStore renames the migrated store aside, dated. One archive, never a
// growing pile: the .bak is the rollback window's copy, deleted at the next stable tag.
func archiveLegacyStore(dsn string) (string, error) {
	base := fmt.Sprintf("%s.%s.bak", dsn, time.Now().UTC().Format("2006-01-02"))
	target := base
	for i := 2; ; i++ {
		if _, err := os.Stat(target); os.IsNotExist(err) {
			break
		}
		target = fmt.Sprintf("%s-%d", base, i)
	}
	// Sidecars are process-local index state; the archived copy keeps them beside it so
	// the .bak is a whole store, not a main file missing its WAL.
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(dsn + suffix); err == nil {
			_ = os.Rename(dsn+suffix, target+suffix)
		}
	}
	if err := os.Rename(dsn, target); err != nil {
		return "", fmt.Errorf("legacy migration: archive %s: %w", dsn, err)
	}
	return target, nil
}

// applyCarry copies the non-session rows into the fresh index.
func (s *Store) applyCarry(carry legacyCarry) error {
	for _, c := range carry.captions {
		if _, err := s.db.Exec(
			`INSERT INTO image_captions (image_id, caption, created_at) VALUES (?, ?, ?)
			 ON CONFLICT(image_id) DO NOTHING`, c.imageID, c.caption, c.createdAt); err != nil {
			return fmt.Errorf("legacy migration: carry caption %s: %w", c.imageID, err)
		}
	}
	for _, p := range carry.pending {
		if _, err := s.db.Exec(
			`INSERT INTO pending_calls (session_id, call_id, tool_name, arguments, dispatched_at)
			 VALUES (?, ?, ?, ?, ?) ON CONFLICT(session_id, call_id) DO NOTHING`,
			p.sessionID, p.callID, p.toolName, p.arguments, p.dispatchedAt); err != nil {
			return fmt.Errorf("legacy migration: carry pending call %s/%s: %w", p.sessionID, p.callID, err)
		}
	}
	return nil
}
