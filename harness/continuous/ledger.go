package continuous

// ledger.go: the evidence ledger. Runtime-owned: rows arrive through the run event
// spine's observer seam (core Context.AddEventObserver), never through any tool. A wake
// report cites ledger rows; the shaper strips claim sentences the rows do not back. The
// verifier never fetches, never stats a URL, never re-checks anything the tool already
// answered.

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/datadir"

	_ "modernc.org/sqlite"
)

// Ledger is the evidence store for ONE home's process.
type Ledger struct {
	db *sql.DB
}

// Open opens (creating if needed) the ledger for the process's data directory.
func OpenLedger(dataDir string) (*Ledger, error) {
	if dataDir == ":memory:" {
		dataDir = ""
	}
	dsn := "ledger.db"
	if dataDir == "" {
		dsn = ":memory:"
	} else {
		dsn = filepath.Join(dataDir, "ledger.db")
		if err := os.MkdirAll(dataDir, 0o755); err != nil {
			return nil, fmt.Errorf("ledger data dir: %w", err)
		}
	}
	db, err := sql.Open("sqlite", dsn+"?_pragma=foreign_keys(1)&_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}
	l := &Ledger{db: db}
	if err := l.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return l, nil
}

// OpenLedgerForHome opens the ledger for an omega home (datadir.AgentHome's answer).
func OpenLedgerForHome(home string) (*Ledger, error) {
	return OpenLedger(datadir.Dir(home))
}

func (l *Ledger) migrate() error {
	_, err := l.db.Exec(`CREATE TABLE IF NOT EXISTS evidence (
	id        INTEGER PRIMARY KEY AUTOINCREMENT,
	ts        TEXT    NOT NULL,
	run_id    TEXT    NOT NULL DEFAULT '',
	tool      TEXT    NOT NULL,
	args_hash TEXT    NOT NULL,
	ok        INTEGER NOT NULL,
	verifier  TEXT    NOT NULL DEFAULT ''
)`)
	return err
}

// Close closes the ledger database.
func (l *Ledger) Close() error { return l.db.Close() }

// record is one evidence row.
type record struct {
	ID       int64
	TS       string
	RunID    string
	Tool     string
	ArgsHash string
	OK       bool
	Verifier string
}

// Append writes one row.
func (l *Ledger) append(r record) (int64, error) {
	res, err := l.db.Exec(`INSERT INTO evidence (ts, run_id, tool, args_hash, ok, verifier) VALUES (?, ?, ?, ?, ?, ?)`,
		r.TS, r.RunID, r.Tool, r.ArgsHash, boolInt(r.OK), r.Verifier)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// List returns the run_id's rows (the report cites these), newest order preserved by.
func (l *Ledger) List(ctx context.Context, runID string) ([]record, error) {
	rows, err := l.db.QueryContext(ctx,
		`SELECT id, ts, run_id, tool, args_hash, ok, verifier FROM evidence WHERE run_id = ? ORDER BY id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []record
	for rows.Next() {
		var r record
		var ok int
		if err := rows.Scan(&r.ID, &r.TS, &r.RunID, &r.Tool, &r.ArgsHash, &ok, &r.Verifier); err != nil {
			return nil, err
		}
		r.OK = ok == 1
		out = append(out, r)
	}
	return out, rows.Err()
}

// Count returns every row count (the roster's evidence total).
func (l *Ledger) Count(ctx context.Context) (int, error) {
	var n int
	err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM evidence`).Scan(&n)
	return n, err
}

// All returns every ledger row, id order (append order) preserved. The caller caps what
// it renders.
func (l *Ledger) All(ctx context.Context) ([]EvidenceRow, error) {
	rows, err := l.db.QueryContext(ctx,
		`SELECT id, ts, run_id, tool, args_hash, ok, verifier FROM evidence ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EvidenceRow
	for rows.Next() {
		var r EvidenceRow
		var ok int
		if err := rows.Scan(&r.ID, &r.TS, &r.RunID, &r.Tool, &r.ArgsHash, &ok, &r.Verifier); err != nil {
			return nil, err
		}
		r.OK = ok == 1
		out = append(out, r)
	}
	return out, rows.Err()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// spawnRunID tags the ledger rows of this process's current run. A
// runtime-owned id the tools never see, never mint.
var spawnRunID = fmt.Sprintf("run-%x-%d", os.Getpid(), time.Now().UnixNano())

// pendingCall carries a tool call from its spine sighting to its result landing.
type pendingCall struct {
	name     string
	argsHash string
	argPath  string
	argURL   string
}

// argPathFor is the cheap check's target. The STAT happens at result time - a call that
// CREATES its file must read as an existing file after the call, so the verifier
// describes the end state the report's claims are about.
func argPathFor(args map[string]any) string {
	candidates := []string{"path", "file", "file_path", "dir", "directory", "folder"}
	for _, key := range candidates {
		if raw, ok := args[key]; ok {
			if path, ok := raw.(string); ok && path != "" {
				cleaned := strings.TrimSpace(path)
				if strings.ContainsAny(cleaned, "\x00") {
					return ""
				}
				return cleaned
			}
		}
	}
	return ""
}

// verify is the cheap post-check.
func verify(argPath string) string {
	if argPath == "" {
		return ""
	}
	if _, err := os.Stat(argPath); err == nil {
		return "file:exists:" + argPath
	}
	return "file:missing:" + argPath
}

// urlVerifier records a URL-carrying call's object (web.fetch's `url` arg).
func urlVerifier(argURL string) string {
	if argURL == "" {
		return ""
	}
	return "url:" + argURL
}

// urlFor picks the URL object of a call.
func urlFor(args map[string]any) string {
	if raw, ok := args["url"].(string); ok {
		if u := strings.TrimSpace(raw); u != "" && strings.IndexByte(u, 0) < 0 {
			return u
		}
	}
	for _, raw := range args {
		if s, ok := raw.(string); ok && (strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")) {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

// argsHash is the cheap stable digest of a call's arguments.
func argsHash(args map[string]any) string {
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		fmt.Fprintf(h, "%s=%v;", k, args[k])
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// observer returns the spine subscription: one row per tool call. The observer blocks
// the run's forward goroutine for one local insert - the spine contract says cheap;
// this is.
func (l *Ledger) observer() core.EventObserver {
	pending := map[string]pendingCall{}
	return func(ev core.Event) {
		switch e := ev.(type) {
		case core.StreamEvent:
			call, ok := e.Event.(core.ToolCallEvent)
			if !ok {
				return
			}
			pending[call.ToolCall.ID] = pendingCall{
				name:     call.ToolCall.Name,
				argsHash: argsHash(call.ToolCall.Arguments),
				argPath:  argPathFor(call.ToolCall.Arguments),
				argURL:   urlFor(call.ToolCall.Arguments),
			}
		case core.ToolResultEvent:
			p, ok := pending[e.Message.ToolCallID]
			if !ok {
				return // no call seen (stream shape): skip, never invent
			}
			delete(pending, e.Message.ToolCallID)
			row := record{
				TS:    core.NowISO(),
				RunID: spawnRunID,
				Tool:  p.name,
				ArgsHash: p.argsHash,
				OK:       !e.Message.IsError,
				Verifier: verify(p.argPath),
			}
			if row.Verifier == "" {
				row.Verifier = urlVerifier(p.argURL)
			}
			if p.name == "" {
				row.Tool = "unknown"
			}
			if _, err := l.append(row); err != nil {
				// Evidence never breaks the run.
				_ = err
			}
		}
	}
}
