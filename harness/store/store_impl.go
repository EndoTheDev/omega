package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/datadir"
)

var ErrNotFound = errors.New("session not found")

var _ agent.StoreProvider = (*Store)(nil)

var _ agent.StorePinger = (*Store)(nil)

// Ping is the store health probe (agent.StorePinger). Deliberately NOT a PRAGMA
// quick_check - that walks every page of the file, and this fact rides the 5s health
// heartbeat, so it must stay O(1). Corruption scanning is the store's own hygiene
// pass's job; the popover asks only "is the database answering".
func (s *Store) Ping(ctx context.Context) error {
	if s.db == nil {
		return errors.New("store: not open")
	}
	var one int
	if err := s.db.QueryRowContext(ctx, `SELECT 1`).Scan(&one); err != nil {
		return err
	}
	if one != 1 {
		return fmt.Errorf("store: ping answered %d, want 1", one)
	}
	return nil
}

// Store persists sessions in append-only JSONL files (the truth) and keeps a derived
// SQLite index over them for listing and search. It is safe for concurrent use: every
// mutation - appends, catch-up, rebuild, delete, rename - serializes on writeMu, and
// index reads go through *sql.DB.
type Store struct {
	db *sql.DB
	// identity is the store's own agent binding. Empty = main; every session row the store
	// filters must be.
	identity string
	dbPath  string
	// sessionsDir is where the session files live (beside the index database).
	sessionsDir string
	// memDir marks a :memory: store, whose session files live in a temp directory that
	// Close removes - the in-memory store is still file-truth, just not durable.
	memDir bool
	// writeMu serializes every mutation of the store's two halves. The index and the files
	// must never move under two goroutines at once: a catch-up scanning a file an append is
	// writing is how a line gets lost.
	writeMu sync.Mutex
	// maxMessageID is the highest message id any session file holds, as learned at boot
	// and advanced by every line the store writes or indexes. The id space is the truth's;
	// allocating it from the index would let a failed apply hand out an id twice.
	maxMessageID int64
	hygMu        sync.RWMutex
	hyg          HygieneConfig
	home         string
	agentName    string
	routeMu      sync.Mutex
	routes       map[string]*Store
	agents       map[string]*Store
}

// SetHygieneConfig swaps the hygiene pass's knobs live.
func (s *Store) SetHygieneConfig(cfg HygieneConfig) {
	s.hygMu.Lock()
	defer s.hygMu.Unlock()
	s.hyg = cfg
}

// HygieneConfig returns a snapshot of the live hygiene config. The copy is defensive: a
// concurrent hot-apply never mutates the caller's view mid-sweep.
func (s *Store) HygieneConfig() HygieneConfig {
	s.hygMu.RLock()
	defer s.hygMu.RUnlock()
	return s.hyg
}

// Open opens (creating if needed) the index at dsn and ensures the schema. The session
// files beside it are the store; the database is rebuilt from them when absent.
func Open(dsn string) (*Store, error) {
	return openStore(dsn)
}

func (s *Store) Open(dsn string) error {
	newStore, err := openStore(dsn)
	if err != nil {
		return err
	}
	// Field-by-field: the struct now carries mutexes and a struct copy would swap live.
	s.hygMu.Lock()
	defer s.hygMu.Unlock()
	s.db = newStore.db
	s.identity = newStore.identity
	s.dbPath = newStore.dbPath
	s.sessionsDir = newStore.sessionsDir
	s.memDir = newStore.memDir
	s.maxMessageID = newStore.maxMessageID
	return nil
}

func openStore(dsn string) (*Store, error) {
	// Data-dir migration runs before the handle opens: a legacy home-root db (with its
	// -wal/-shm sidecars) is moved into data/ first - never copy-and-leave. The session
	// files follow, or the moved database would open beside an empty sessions directory
	// and every session would vanish from the index.
	if err := datadir.Migrate(dsn); err != nil {
		return nil, err
	}
	if err := migrateSessionsDir(dsn); err != nil {
		return nil, err
	}
	sessionsDir := sessionsDirFor(dsn)
	memDir := false
	if dsn == ":memory:" {
		dir, err := os.MkdirTemp("", "omega-sessions-")
		if err != nil {
			return nil, fmt.Errorf("store: memory sessions dir: %w", err)
		}
		sessionsDir, memDir = dir, true
	}
	// A memory store's session files are a scratch directory: any failure on the way up
	// leaves nothing behind.
	fail := func(err error) (*Store, error) {
		if memDir {
			_ = os.RemoveAll(sessionsDir)
		}
		return nil, err
	}
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		return fail(fmt.Errorf("store: sessions dir: %w", err))
	}
	// A pre-JSONL database is a STORE, not an index: it migrates to files, verifies by
	// read-back, and only then gets archived aside.
	var carry legacyCarry
	if isLegacyStore(dsn) {
		migrated, err := migrateLegacyStore(dsn, sessionsDir)
		if err != nil {
			return fail(err)
		}
		if _, err := archiveLegacyStore(dsn); err != nil {
			return fail(err)
		}
		carry = migrated
	}
	db, err := openIndexDB(dsn)
	if err != nil {
		return fail(err)
	}
	// Everything past the handle closes it on the way out.
	closeFail := func(err error) (*Store, error) {
		_ = db.Close()
		return fail(err)
	}
	s := &Store{db: db, identity: storeAgentIdentity(), dbPath: dsn, sessionsDir: sessionsDir, memDir: memDir}
	if err := s.ensureIndexSchema(); err != nil {
		return closeFail(err)
	}
	if len(carry.captions) > 0 || len(carry.pending) > 0 {
		if err := s.applyCarry(carry); err != nil {
			return closeFail(err)
		}
	}
	// Boot recovery runs before the store is shared, which is the only moment a session
	// file may be cut: crash residue goes, and the id ceiling comes back from the truth.
	ceiling, err := s.recoverSessionFiles()
	if err != nil {
		return closeFail(err)
	}
	s.maxMessageID = ceiling
	// Boot catch-up: index only what the files grew by since the last open.
	if err := s.catchUp(context.Background()); err != nil {
		return closeFail(err)
	}
	return s, nil
}

// openIndexDB opens the derived index with the pragmas every pooled connection needs: WAL
// mode for concurrent readers plus one writer across processes, and a busy timeout so a
// second writer waits instead of failing. The index declares no foreign keys - lineage
// lives in the session files and the store enforces it on the write path - so no FK
// pragma rides here.
func openIndexDB(dsn string) (*sql.DB, error) {
	dsnParams := dsn
	switch {
	case !strings.Contains(dsn, "?"):
		dsnParams += "?_journal_mode=WAL&_busy_timeout=5000"
	case !strings.Contains(dsn, "_journal_mode="):
		dsnParams += "&_journal_mode=WAL&_busy_timeout=5000"
	}
	db, err := sql.Open("sqlite", dsnParams)
	if err != nil {
		return nil, err
	}
	if dsn == ":memory:" {
		db.SetMaxOpenConns(1)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// Close closes the underlying database and every agent home store this one opened.
func (s *Store) Close() error {
	homesErr := s.closeHomes()
	err := s.db.Close()
	if s.memDir && s.sessionsDir != "" {
		_ = os.RemoveAll(s.sessionsDir)
	}
	return cmpErr(err, homesErr)
}

func cmpErr(first, second error) error {
	if first != nil {
		return first
	}
	return second
}


// DBPath reports the dsn this store opened with - the session database file a child
// process must open to see the rows this store wrote.
func (s *Store) DBPath() string {
	return s.dbPath
}


func (s *Store) CreateSession(ctx context.Context, id, parentID, label string) error {
	return s.createSessionFile(ctx, id, parentID, "", label, "", true)
}

func (s *Store) CreateDelegateSession(ctx context.Context, id, delegateParent, label string) error {
	return s.createSessionFile(ctx, id, "", delegateParent, label, "", true)
}

func scanSession(scanner interface{ Scan(...any) error }) (core.Session, error) {
	var sess core.Session
	var parent, delegate sql.NullString
	var pinned int
	var archived int
	var listed int
	var lastRun string
	if err := scanner.Scan(&sess.ID, &parent, &delegate, &sess.Label, &sess.CreatedAt, &sess.UpdatedAt,
		&sess.PromptTokens, &sess.CompletionTokens, &pinned, &archived,
		&sess.TitleState, &sess.LastReadAt, &sess.Agent, &lastRun, &sess.Model, &sess.ProviderName, &sess.Workspace,
		&sess.Summary, &listed, &sess.ContextStart, &sess.ContextSummary); err != nil {
		return core.Session{}, err
	}
	sess.ParentID = parent.String
	sess.DelegateParent = delegate.String
	sess.Pinned = pinned != 0
	sess.Archived = archived != 0
	// The `listed` column is the human's session list, so its zero is the MACHINERY's row.
	sess.MachineOwned = listed == 0
	// Normalize: the row default is 'default', but a session written by an older writer.
	// omitempty then never hides the binding.
	if sess.Agent == "" {
		sess.Agent = agent.DefaultAgentName
	}
	// last_run is a JSON blob; empty means the session never ran. A
	// malformed blob decodes to nil - the rates just stay blank.
	if lastRun != "" {
		var stats core.LastRunStats
		if err := json.Unmarshal([]byte(lastRun), &stats); err == nil {
			sess.LastRun = &stats
		}
	}
	return sess, nil
}

// sessionColumns is the projection shared by GetSession and ListSessions.
const sessionColumns = "id, parent_id, delegate_parent, label, created_at, updated_at, prompt_tokens, completion_tokens, pinned, archived, title_state, last_read_at, agent, last_run, model, provider, workspace, summary, listed, context_start, context_summary"

// SetSessionLastRun writes the last-run stats block (JSON) on the session's row, or
// clears it when stats is nil.
func (s *Store) SetSessionLastRun(ctx context.Context, sessionID string, stats *core.LastRunStats) error {
	if o := s.owner(ctx, sessionID); o != nil {
		return o.SetSessionLastRun(ctx, sessionID, stats)
	}
	encoded := ""
	if stats != nil {
		b, err := json.Marshal(stats)
		if err != nil {
			return err
		}
		encoded = string(b)
	}
	return s.appendMeta(ctx, sessionID, &changes{LastRun: &encoded})
}

// SetSessionModel pins the session's model_name.
func (s *Store) SetSessionModel(ctx context.Context, sessionID, model string) error {
	if o := s.owner(ctx, sessionID); o != nil {
		return o.SetSessionModel(ctx, sessionID, model)
	}
	return s.appendMeta(ctx, sessionID, &changes{Model: &model})
}

// SetSessionProvider pins the session's provider INSTANCE name.
func (s *Store) SetSessionProvider(ctx context.Context, sessionID, provider string) error {
	if o := s.owner(ctx, sessionID); o != nil {
		return o.SetSessionProvider(ctx, sessionID, provider)
	}
	return s.appendMeta(ctx, sessionID, &changes{Provider: &provider})
}

// SetSessionWorkspace roots the session's file operations at an absolute working.
func (s *Store) SetSessionWorkspace(ctx context.Context, sessionID, workspace string) error {
	if o := s.owner(ctx, sessionID); o != nil {
		return o.SetSessionWorkspace(ctx, sessionID, workspace)
	}
	return s.appendMeta(ctx, sessionID, &changes{Workspace: &workspace})
}

// GetSession returns the session with the given id, or ErrNotFound.
func (s *Store) GetSession(ctx context.Context, id string) (core.Session, error) {
	if o := s.owner(ctx, id); o != nil {
		return o.GetSession(ctx, id)
	}
	sess, err := scanSession(s.db.QueryRowContext(ctx,
		`SELECT `+sessionColumns+` FROM sessions WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return core.Session{}, ErrNotFound
	}
	if err != nil {
		return core.Session{}, err
	}
	return sess, nil
}

func storeAgentIdentity() string {
	name := strings.TrimSpace(os.Getenv("OMEGA_AGENT"))
	if name == "default" {
		return ""
	}
	return name
}

// ListSessions returns the sessions visible to this store, ordered by creation time.
// For main (no OMEGA_AGENT) that is the default agent's LISTED sessions only - the
// binding that keeps the pre-agent session list, tree, and insights exactly what they
// were. `listed = 0` rows are machinery-owned conversations that are never the
// maintainer's own: they open from the agents roster and have no sidebar row.
func (s *Store) ListSessions(ctx context.Context) ([]core.Session, error) {
	query := `SELECT ` + sessionColumns + ` FROM sessions WHERE agent = 'default' AND listed = 1 ORDER BY created_at`
	if s.identity != "" {
		query = `SELECT ` + sessionColumns + ` FROM sessions ORDER BY created_at`
	}
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []core.Session
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

// DeleteSession removes a session, its descendants and their files. It is a no-op (nil)
// when the session does not exist.
func (s *Store) DeleteSession(ctx context.Context, id string) error {
	if o := s.owner(ctx, id); o != nil {
		return o.DeleteSession(ctx, id)
	}
	return s.deleteSessions(ctx, []string{id})
}

func (s *Store) UpdateSession(ctx context.Context, id, label string) error {
	if o := s.owner(ctx, id); o != nil {
		return o.UpdateSession(ctx, id, label)
	}
	// A manual label IS activity: the sidebar's buckets follow it.
	now := core.NowISO()
	return s.appendMeta(ctx, id, &changes{Label: &label, UpdatedAt: &now})
}

// GetSessionTree returns the session forest.
func (s *Store) GetSessionTree(ctx context.Context) ([]*core.SessionNode, error) {
	sessions, err := s.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]*core.SessionNode, len(sessions))
	for i := range sessions {
		byID[sessions[i].ID] = &core.SessionNode{Session: sessions[i]}
	}
	var roots []*core.SessionNode
	for _, sess := range sessions {
		node := byID[sess.ID]
		if node.ParentID == "" {
			roots = append(roots, node)
			continue
		}
		if parent, ok := byID[node.ParentID]; ok {
			parent.Children = append(parent.Children, node)
		} else {
			roots = append(roots, node)
		}
	}
	return roots, nil
}

func (s *Store) GetAncestorMessages(ctx context.Context, id string) ([]core.Message, error) {
	if o := s.owner(ctx, id); o != nil {
		return o.GetAncestorMessages(ctx, id)
	}
	var chain []string
	cur := id
	for cur != "" {
		chain = append(chain, cur)
		sess, err := s.GetSession(ctx, cur)
		if err != nil {
			return nil, err
		}
		cur = sess.ParentID
	}
	var out []core.Message
	for i := len(chain) - 1; i >= 0; i-- {
		messages, err := s.GetMessages(ctx, chain[i])
		if err != nil {
			return nil, err
		}
		out = append(out, messages...)
	}
	return out, nil
}

// AppendMessage appends a message to a session's file and touches the index row.
// Without the touch, updated_at froze after the title's last write - the sidebar's time
// buckets silently drifted off the real activity, and no unread state could ever be
// derived from the row.
func (s *Store) AppendMessage(ctx context.Context, sessionID string, msg core.Message) error {
	if o := s.owner(ctx, sessionID); o != nil {
		return o.AppendMessage(ctx, sessionID, msg)
	}
	role, payload, err := core.EncodeMessage(msg)
	if err != nil {
		return err
	}
	if conversationRole(role) {
		parent, err := s.resolveParent(ctx, sessionID, msg)
		if err != nil {
			return err
		}
		if parent != "" {
			role, payload, err = core.EncodeMessage(core.WithParentID(msg, parent))
			if err != nil {
				return err
			}
		}
	}
	return s.appendMessageLine(ctx, sessionID, role, payload, core.NowISO())
}

// GetMessages returns a session's messages in append order, read from the session file -
// the truth, never the index.
func (s *Store) GetMessages(ctx context.Context, sessionID string) ([]core.Message, error) {
	if o := s.owner(ctx, sessionID); o != nil {
		return o.GetMessages(ctx, sessionID)
	}
	rows, err := s.loadRows(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	var out []core.Message
	for _, sm := range rows {
		out = append(out, sm.Message)
	}
	return out, nil
}

func (s *Store) GetMessagesWithTimes(ctx context.Context, sessionID string) ([]core.StoredMessage, error) {
	if o := s.owner(ctx, sessionID); o != nil {
		return o.GetMessagesWithTimes(ctx, sessionID)
	}
	return s.loadRows(ctx, sessionID)
}

func (s *Store) CountMessages(ctx context.Context, sessionID string) (int, error) {
	if o := s.owner(ctx, sessionID); o != nil {
		return o.CountMessages(ctx, sessionID)
	}
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM messages WHERE session_id = ?`, sessionID).Scan(&n)
	return n, err
}

// SessionCounts computes per-session message and tool-result counts at read time.
func (s *Store) SessionCounts(ctx context.Context) (map[string]core.SessionCounts, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT session_id,
		        SUM(role NOT IN ('model_change', 'thinking_level_change', 'variant_select', 'verdict_card', 'verdict', 'system')),
		        SUM(role = 'tool')
		 FROM messages GROUP BY session_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]core.SessionCounts)
	for rows.Next() {
		var id string
		var c core.SessionCounts
		if err := rows.Scan(&id, &c.MessageCount, &c.ToolCount); err != nil {
			return nil, err
		}
		out[id] = c
	}
	return out, rows.Err()
}

func (s *Store) SetSessionPinned(ctx context.Context, sessionID string, pinned bool) error {
	if o := s.owner(ctx, sessionID); o != nil {
		return o.SetSessionPinned(ctx, sessionID, pinned)
	}
	return s.appendMeta(ctx, sessionID, &changes{Pinned: &pinned})
}

func (s *Store) SetSessionArchived(ctx context.Context, sessionID string, archived bool) error {
	if o := s.owner(ctx, sessionID); o != nil {
		return o.SetSessionArchived(ctx, sessionID, archived)
	}
	return s.appendMeta(ctx, sessionID, &changes{Archived: &archived})
}

func (s *Store) SetSessionRead(ctx context.Context, sessionID string) error {
	if o := s.owner(ctx, sessionID); o != nil {
		return o.SetSessionRead(ctx, sessionID)
	}
	now := core.NowISO()
	return s.appendMeta(ctx, sessionID, &changes{LastReadAt: &now})
}

// SetSessionTitle writes a generated label and its title_state transition in one. The
// state machine: provisional only from 0 (moves to 1), final from 0 or 1 (moves to 2) -
// a session at 2 is never touched, including the pre-title-state sessions that were
// migrated to 2 because they carried a label. updated_at is untouched: a generated label
// is not activity, and a title landing after a read stamp must not turn the session
// unread.
func (s *Store) SetSessionTitle(ctx context.Context, sessionID, title string, final bool) error {
	if o := s.owner(ctx, sessionID); o != nil {
		return o.SetSessionTitle(ctx, sessionID, title, final)
	}
	current, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}
	next := 1
	if final {
		next = 2
	}
	if current.TitleState >= 2 || (!final && current.TitleState > 0) {
		return nil // the state machine refuses the write; nothing is appended
	}
	return s.appendMeta(ctx, sessionID, &changes{Label: &title, TitleState: &next})
}

// SetSessionSummary writes the hygiene pass's digest on the session row. Like a
// generated title, a summary is not activity - updated_at is untouched, so a summary
// landing after a read stamp must not turn the session unread.
func (s *Store) SetSessionSummary(ctx context.Context, sessionID, summary string) error {
	if o := s.owner(ctx, sessionID); o != nil {
		return o.SetSessionSummary(ctx, sessionID, summary)
	}
	return s.appendMeta(ctx, sessionID, &changes{Summary: &summary})
}

// SessionModels returns the last recorded model per session.
func (s *Store) SessionModels(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT m.session_id, m.payload FROM messages m
		 WHERE m.role = 'model_change'
		   AND m.seq = (SELECT MAX(seq) FROM messages
		               WHERE session_id = m.session_id AND role = 'model_change')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]string)
	for rows.Next() {
		var id, payload string
		if err := rows.Scan(&id, &payload); err != nil {
			return nil, err
		}
		var mc core.ModelChange
		if json.Unmarshal([]byte(payload), &mc) != nil || mc.Model == "" {
			continue
		}
		out[id] = mc.Model
	}
	return out, rows.Err()
}

func (s *Store) GetImageCaption(ctx context.Context, imageID string) (caption string, ok bool, err error) {
	err = s.db.QueryRowContext(ctx,
		`SELECT caption FROM image_captions WHERE image_id = ?`, imageID).
		Scan(&caption)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return caption, true, nil
}

// SaveImageCaption persists one image caption. Re-saving the same image id keeps the
// first caption - a cached caption is never rewritten (nothing benefits from the churn;
// the id is content-addressed).
func (s *Store) SaveImageCaption(ctx context.Context, imageID, caption string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO image_captions (image_id, caption, created_at) VALUES (?, ?, ?)
		 ON CONFLICT(image_id) DO NOTHING`,
		imageID, caption, core.NowISO())
	return err
}

var _ agent.CaptionStore = (*Store)(nil)

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// AddSessionTokens adds a turn's real provider-reported usage.
func (s *Store) AddSessionTokens(ctx context.Context, sessionID string, prompt, completion int) error {
	if o := s.owner(ctx, sessionID); o != nil {
		return o.AddSessionTokens(ctx, sessionID, prompt, completion)
	}
	if prompt == 0 && completion == 0 {
		return nil
	}
	return s.appendMeta(ctx, sessionID, &changes{AddPrompt: prompt, AddCompletion: completion})
}

func quoteFTS(term string) string {
	return `"` + strings.ReplaceAll(term, `"`, `""`) + `"*`
}

func (s *Store) SearchMessages(ctx context.Context, query string) ([]core.SearchResult, error) {
	if err := s.catchUp(ctx); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT session_id, `+snippetCols("messages_fts", 1)+`
		 FROM messages_fts WHERE messages_fts MATCH ?
		 ORDER BY rank`, quoteFTS(query))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []core.SearchResult
	for rows.Next() {
		var r core.SearchResult
		var marked string
		if err := rows.Scan(&r.SessionID, &marked, &r.Snippet); err != nil {
			return nil, err
		}
		r.Snippet, r.Matches = resolveSnippet(marked, r.Snippet)
		out = append(out, r)
	}
	return out, rows.Err()
}

// ftsMatchOpen and ftsMatchClose are the one-byte sentinel pair FTS5 snippet wraps.
// Control bytes, never part of conversation content: the snippet must stay
// byte-for-byte stored content, so match markers ride a character space disjoint from
// content and are parsed into spans at read, never shipped inside the string.
const (
	ftsMatchOpen  = "\x01"
	ftsMatchClose = "\x02"
	ftsSentinels = ftsMatchOpen + ftsMatchClose
)

// snippetCols builds the SELECT pair for one FTS column. The plain copy is the
// ambiguity referee - when the stored content itself carries a sentinel byte inside the
// window, the marked parse is unreliable and the row ships the plain excerpt with no
// spans.
func snippetCols(table string, col int) string {
	return fmt.Sprintf(
		"snippet(%s, %d, '%s', '%s', '', 20), snippet(%s, %d, '', '', '', 20)",
		table, col, ftsMatchOpen, ftsMatchClose, table, col)
}

// resolveSnippet converts one marked+plain snippet row pair into the verbatim snippet
// plus the matched-term spans (byte offsets). The plain copy referees: equal strings
// mean no matched term landed in the window; a sentinel byte inside the plain copy
// means the stored content itself holds one and the marker parse would be ambiguous, so
// the plain excerpt ships with no spans (exactness beats metadata).
func resolveSnippet(marked, plain string) (string, []core.SearchMatch) {
	if marked == plain {
		return plain, nil
	}
	if strings.ContainsAny(plain, ftsSentinels) {
		return plain, nil
	}
	var sb strings.Builder
	sb.Grow(len(marked))
	spans := []core.SearchMatch{}
	open := -1
	for i := 0; i < len(marked); i++ {
		switch c := marked[i]; c {
		case ftsMatchOpen[0]:
			open = sb.Len()
		case ftsMatchClose[0]:
			if open >= 0 {
				spans = append(spans, core.SearchMatch{Start: open, End: sb.Len()})
				// The plain referee above proved the marked string holds exactly snippet's own
				// balanced markers, so an open never stays dangling here.
				open = -1
			}
		default:
			sb.WriteByte(c)
		}
	}
	return sb.String(), spans
}

// SearchSessions performs the unified search behind /sessions/search. Column filters
// scope each index query to its own column so a summary match is never misread as a
// label match.
func (s *Store) SearchSessions(ctx context.Context, query string) ([]core.SearchResult, error) {
	if err := s.catchUp(ctx); err != nil {
		return nil, err
	}
	fts := quoteFTS(query)
	labelScoped := `{label} : ` + fts
	summaryScoped := `{summary} : ` + fts
	out := []core.SearchResult{}
	seen := make(map[string]bool)

	trows, err := s.db.QueryContext(ctx,
		`SELECT session_id, label FROM sessions_fts
		 WHERE sessions_fts MATCH ? ORDER BY rank`, labelScoped)
	if err != nil {
		return nil, err
	}
	for trows.Next() {
		var id, label string
		if err := trows.Scan(&id, &label); err != nil {
			trows.Close()
			return nil, err
		}
		// The label index holds one row per session, but belt-and- braces on scans keeps the
		// one-hit-per-session contract local to this method.
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, core.SearchResult{SessionID: id, Snippet: label, Title: true})
	}
	if err := trows.Err(); err != nil {
		trows.Close()
		return nil, err
	}
	trows.Close()

	srows, err := s.db.QueryContext(ctx,
		`SELECT session_id, `+snippetCols("sessions_fts", 2)+`
		 FROM sessions_fts WHERE sessions_fts MATCH ? ORDER BY rank`, summaryScoped)
	if err != nil {
		return nil, err
	}
	for srows.Next() {
		var id, snippet, marked string
		if err := srows.Scan(&id, &marked, &snippet); err != nil {
			srows.Close()
			return nil, err
		}
		snippet, matchSpans := resolveSnippet(marked, snippet)
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, core.SearchResult{SessionID: id, Snippet: snippet, Matches: matchSpans, Summary: true})
	}
	if err := srows.Err(); err != nil {
		srows.Close()
		return nil, err
	}
	srows.Close()

	crows, err := s.db.QueryContext(ctx,
		`SELECT session_id, `+snippetCols("messages_fts", 1)+`
		 FROM messages_fts WHERE messages_fts MATCH ? ORDER BY rank`, fts)
	if err != nil {
		return nil, err
	}
	defer crows.Close()
	for crows.Next() {
		var id, snippet, marked string
		if err := crows.Scan(&id, &marked, &snippet); err != nil {
			return nil, err
		}
		snippet, matchSpans := resolveSnippet(marked, snippet)
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, core.SearchResult{SessionID: id, Snippet: snippet, Matches: matchSpans})
	}
	return out, crows.Err()
}

type sessionStats struct {
	msgs       int
	userMsgs   int
	toolCalls  int
	tokens     int
	toolCounts map[string]int
}

type notableTracker struct {
	maxMsgs   int
	maxTokens int
	maxTools  int
}

func countMessages(msgs []core.Message, in *core.Insights, st *sessionStats) {
	for _, msg := range msgs {
		switch msg.(type) {
		case core.ModelChange, core.ThinkingLevelChange, core.VariantSelect:
			continue
		}
		in.Messages++
		switch m := msg.(type) {
		case core.User:
			st.userMsgs++
		case core.Assistant:
			st.toolCalls += len(m.ToolCalls)
			for _, tc := range m.ToolCalls {
				st.toolCounts[tc.Name]++
			}
		}
	}
}

func updateNotables(in *core.Insights, st *sessionStats, nt *notableTracker, detail string) {
	if st.msgs > nt.maxMsgs {
		nt.maxMsgs = st.msgs
		in.NotableMsgs = core.NotableStat{Value: st.msgs, Detail: detail}
	}
	if st.tokens > nt.maxTokens {
		nt.maxTokens = st.tokens
		in.NotableTokens = core.NotableStat{Value: st.tokens, Detail: detail}
	}
	if st.toolCalls > nt.maxTools {
		nt.maxTools = st.toolCalls
		in.NotableTools = core.NotableStat{Value: st.toolCalls, Detail: detail}
	}
}

func (s *Store) processSession(ctx context.Context, sess core.Session, in *core.Insights, st *sessionStats, nt *notableTracker, dayCounts *[7]int) bool {
	t, err := time.Parse(time.RFC3339, sess.CreatedAt)
	if err != nil {
		return false
	}
	msgs, err := s.GetMessages(ctx, sess.ID)
	if err != nil {
		return false
	}
	st.msgs = len(msgs)
	st.userMsgs = 0
	st.toolCalls = 0
	countMessages(msgs, in, st)
	in.UserMessages += st.userMsgs
	in.ToolCalls += st.toolCalls
	st.tokens = agent.EstimateTokens(msgs)
	in.TotalTokens += st.tokens

	wd := int(t.Weekday())
	if wd == 0 {
		wd = 6 // Sunday -> 6, Mon -> 0
	}
	dayCounts[wd]++

	label := sess.Label
	if label == "" {
		label = sess.ID
	}
	detail := t.Format("Jan 2") + ", " + label
	updateNotables(in, st, nt, detail)
	return true
}

func buildDailyActivity(daily *[7]core.DayStat, dayCounts [7]int, weekdays []string) {
	maxDay := 0
	for _, c := range dayCounts {
		if c > maxDay {
			maxDay = c
		}
	}
	for i := 0; i < 7; i++ {
		bar := ""
		if maxDay > 0 {
			bars := int(float64(dayCounts[i]) / float64(maxDay) * 14)
			for j := 0; j < bars; j++ {
				bar += "█"
			}
		}
		daily[i] = core.DayStat{Day: weekdays[i], Count: dayCounts[i], Bar: bar}
	}
}

func (s *Store) ComputeInsights(ctx context.Context, days int) (*core.Insights, error) {
	sessions, err := s.ListSessions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}

	now := time.Now()
	cutoff := time.Time{}
	if days > 0 {
		cutoff = now.AddDate(0, 0, -days)
	}

	weekdays := []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}
	toolCounts := map[string]int{}
	dayCounts := [7]int{}
	in := &core.Insights{Days: days}
	if days > 0 {
		in.Period = fmt.Sprintf("Last %d days", days)
	} else {
		in.Period = "All time"
	}
	in.PeriodEnd = now.Format("2006-01-02")

	nt := &notableTracker{}
	st := &sessionStats{toolCounts: toolCounts}

	for _, sess := range sessions {
		t, err := time.Parse(time.RFC3339, sess.CreatedAt)
		if err != nil {
			continue
		}
		if !t.Before(cutoff) || days <= 0 {
			in.Sessions++
			s.processSession(ctx, sess, in, st, nt, &dayCounts)
		}
	}

	if in.Sessions > 0 {
		in.AvgSessionMsgs = float64(in.Messages) / float64(in.Sessions)
	}

	for name, count := range toolCounts {
		in.Tools = append(in.Tools, core.ToolStat{Name: name, Count: count})
	}
	sort.Slice(in.Tools, func(i, j int) bool {
		return in.Tools[i].Count > in.Tools[j].Count
	})

	buildDailyActivity(&in.Daily, dayCounts, weekdays)

	in.DBSizeBytes, in.WALSizeBytes = s.DBFileSizes()

	if days > 0 {
		in.PeriodStart = cutoff.Format("2006-01-02")
	}

	return in, nil
}
