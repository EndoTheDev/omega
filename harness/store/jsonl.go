package store

// jsonl.go: the session files - the store's source of truth.
//
// One file per session at <data dir>/sessions/<id>.jsonl, append-only, one JSON object
// per line. A line names its session (the lineage), so a line is self-describing even
// when moved. Three kinds:
//
//   session - the header, written once at creation (parent/delegate lineage, agent
//             binding, list visibility, created_at);
//   message - one stored message: the role discriminator, the encoded payload as the
//             message codec wrote it, its per-session id and its timestamp;
//   meta    - a metadata change (label, pin, archive, title state, summary, token
//             deltas...), naming only the fields it changed.
//
// Reading is a FOLD: header, then meta lines in order, messages in file order. Nothing
// is ever rewritten, so the fold is the only reconstruction rule. A crash mid-append
// can leave a torn final line; readers drop it and boot recovery truncates it.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/EndoTheDev/omega/core"
)

// jsonlVersion marks the line format. A future format change reads the version and
// migrates; it is written on every line so a mixed file is detectable.
const jsonlVersion = 1

const (
	kindSession = "session"
	kindMessage = "message"
	kindMeta    = "meta"
)

// changes names only the session fields a meta line changed. Pointers distinguish
// "unchanged" from "set to the zero value".
type changes struct {
	Label          *string `json:"label,omitempty"`
	Pinned         *bool   `json:"pinned,omitempty"`
	Archived       *bool   `json:"archived,omitempty"`
	TitleState     *int    `json:"title_state,omitempty"`
	LastReadAt     *string `json:"last_read_at,omitempty"`
	Agent          *string `json:"agent,omitempty"`
	Model          *string `json:"model,omitempty"`
	Provider       *string `json:"provider,omitempty"`
	Workspace      *string `json:"workspace,omitempty"`
	Summary        *string `json:"summary,omitempty"`
	Listed         *bool   `json:"listed,omitempty"`
	ParentID       *string `json:"parent_id,omitempty"`
	DelegateParent *string `json:"delegate_parent,omitempty"`
	ContextStart   *string `json:"context_start,omitempty"`
	ContextSummary *string `json:"context_summary,omitempty"`
	LastRun        *string `json:"last_run,omitempty"`
	UpdatedAt      *string `json:"updated_at,omitempty"`
	// Token totals arrive as DELTAS: a fold adds them, so two writers can never
	// clobber each other's usage the way a read-modify-write of an absolute does.
	AddPrompt     int `json:"add_prompt_tokens,omitempty"`
	AddCompletion int `json:"add_completion_tokens,omitempty"`
}

// fileLine is one JSONL line. Header fields and change fields share the struct because
// a line IS one object; Kind says which half is populated.
type fileLine struct {
	V       int    `json:"v"`
	Kind    string `json:"kind"`
	Session string `json:"session"`

	// message
	ID      int64           `json:"id,omitempty"`
	Role    string          `json:"role,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
	At      string          `json:"at,omitempty"`

	// header
	ParentID       string `json:"parent_id,omitempty"`
	DelegateParent string `json:"delegate_parent,omitempty"`
	Label          string `json:"label,omitempty"`
	CreatedAt      string `json:"created_at,omitempty"`
	UpdatedAt      string `json:"updated_at,omitempty"`
	Agent          string `json:"agent,omitempty"`
	Listed         *bool  `json:"listed,omitempty"`
	LastReadAt     string `json:"last_read_at,omitempty"`

	// meta
	Changes *changes `json:"changes,omitempty"`
}

// storedMsg is one message line read back.
type storedMsg struct {
	ID        int64
	Role      string
	Payload   []byte
	CreatedAt string
}

// folded is one session file read back: metadata after the meta fold, messages in file
// order, and the byte offset where the complete lines end.
type folded struct {
	session  core.Session
	messages []storedMsg
	end      int64
	torn     bool
}

// errCorruptLine reports a complete line that is not valid JSON. Only an UNTERMINATED
// final line is tolerated as a torn append; a terminated line that does not parse is
// corruption in the truth, and the reader refuses to guess past it.
var errCorruptLine = errors.New("store: corrupt jsonl line")

// sessionsDirName is the sessions directory beside the index database.
const sessionsDirName = "sessions"

func sessionsDirFor(dsn string) string {
	return filepath.Join(filepath.Dir(dsn), sessionsDirName)
}

// migrateSessionsDir moves a home-rooted sessions directory into data/ beside its index.
// It is the file half of datadir.Migrate, and runs only when that migration applies: the
// index is inside a "data" directory, the legacy sessions directory sits one level up,
// and nothing already occupies the destination. Rename, never copy: a crash leaves the
// files where they were and the next boot re-runs it.
func migrateSessionsDir(dsn string) error {
	path := dsn
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	if path == "" || path == ":memory:" || filepath.Base(filepath.Dir(path)) != "data" {
		return nil
	}
	dataDir := filepath.Dir(path)
	legacy := filepath.Join(filepath.Dir(dataDir), sessionsDirName)
	target := filepath.Join(dataDir, sessionsDirName)
	if _, err := os.Stat(legacy); err != nil {
		return nil
	}
	if _, err := os.Stat(target); err == nil {
		return nil // both exist: the data-dir copy wins, the legacy one is never touched
	}
	if err := os.Rename(legacy, target); err != nil {
		return fmt.Errorf("store: move sessions dir into data: %w", err)
	}
	return nil
}

// validSessionID refuses anything that would escape the sessions directory. Session ids
// are the file's base name, so a separator here is a path traversal.
func validSessionID(id string) error {
	if id == "" {
		return errors.New("store: empty session id")
	}
	if strings.ContainsAny(id, `/\`) || id == "." || id == ".." || strings.ContainsAny(id, "\x00") {
		return fmt.Errorf("store: invalid session id %q", id)
	}
	return nil
}

// sessionPath is the file backing one session.
func (s *Store) sessionPath(id string) (string, error) {
	if err := validSessionID(id); err != nil {
		return "", err
	}
	return filepath.Join(s.sessionsDir, id+".jsonl"), nil
}

// appendLines writes complete lines in ONE write. A single write keeps a concurrent
// reader from seeing half a line interleaved with somebody else's; the trailing newline
// is what makes a line complete. A short write is rolled back to the pre-append size, so
// a failed append can never leave a fragment for the next one to merge into.
func appendLines(path string, lines ...fileLine) (int64, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	// Off: a payload's bytes are the message codec's, and HTML escaping would rewrite
	// stored content (the snippet verbatim contract).
	enc.SetEscapeHTML(false)
	for i := range lines {
		if lines[i].V == 0 {
			lines[i].V = jsonlVersion
		}
		if err := enc.Encode(&lines[i]); err != nil {
			return 0, err
		}
	}
	var before int64
	if info, err := os.Stat(path); err == nil {
		before = info.Size()
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return 0, err
	}
	n, werr := f.Write(buf.Bytes())
	cerr := f.Close()
	if werr == nil {
		werr = cerr
	}
	if werr != nil {
		if terr := os.Truncate(path, before); terr != nil {
			return int64(n), fmt.Errorf("store: append to %s: %v (and the partial line could not be dropped: %v)", path, werr, terr)
		}
		return int64(n), werr
	}
	return int64(n), nil
}

// scanJSONL hands every COMPLETE line to fn with the byte offset just past it. A final
// line without its newline is a torn append: it is reported, never handed over, and the
// returned offset ends at the last complete line so recovery truncates exactly it.
func scanJSONL(r io.Reader, fn func(line []byte, end int64) error) (end int64, torn bool, err error) {
	br := bufio.NewReaderSize(r, 64<<10)
	for {
		line, rerr := br.ReadBytes('\n')
		if len(line) > 0 {
			if line[len(line)-1] != '\n' {
				// EOF with no terminator: the writer never finished this line.
				return end, true, nil
			}
			body := line[:len(line)-1]
			if len(body) > 0 {
				if err := fn(body, end+int64(len(line))); err != nil {
					return end, false, err
				}
			}
			end += int64(len(line))
		}
		if rerr != nil {
			if errors.Is(rerr, io.EOF) {
				return end, false, nil
			}
			return end, false, rerr
		}
	}
}

// foldFile reads one session file back. A missing file is empty, not an error: a
// session whose file is gone has no messages, and the caller decides what that means.
func foldFile(path string) (folded, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return folded{}, nil
		}
		return folded{}, err
	}
	defer f.Close()
	return foldReader(f)
}

// foldReader is the fold itself: header, meta changes in order, messages in file order.
func foldReader(r io.Reader) (folded, error) {
	var out folded
	var updated time.Time
	var updatedStr string
	seenHeader := false
	end, torn, err := scanJSONL(r, func(raw []byte, _ int64) error {
		var line fileLine
		if jerr := json.Unmarshal(raw, &line); jerr != nil {
			return errCorruptLine
		}
		switch line.Kind {
		case kindSession:
			if seenHeader {
				return fmt.Errorf("store: duplicate session header for %q", line.Session)
			}
			seenHeader = true
			out.session = core.Session{
				ID:             line.Session,
				ParentID:       line.ParentID,
				DelegateParent: line.DelegateParent,
				Label:          line.Label,
				CreatedAt:      line.CreatedAt,
				UpdatedAt:      line.UpdatedAt,
				Agent:          line.Agent,
				LastReadAt:     line.LastReadAt,
				MachineOwned:   line.Listed != nil && !*line.Listed,
			}
			if out.session.Agent == "" {
				out.session.Agent = "default"
			}
			updated, updatedStr = laterTime(updated, updatedStr, line.UpdatedAt)
		case kindMessage:
			out.messages = append(out.messages, storedMsg{
				ID:        line.ID,
				Role:      line.Role,
				Payload:   append([]byte(nil), line.Payload...),
				CreatedAt: line.At,
			})
			updated, updatedStr = laterTime(updated, updatedStr, line.At)
		case kindMeta:
			if line.Changes == nil {
				break
			}
			applyChanges(&out.session, line.Changes)
			if line.Changes.UpdatedAt != nil {
				updated, updatedStr = laterTime(updated, updatedStr, *line.Changes.UpdatedAt)
			}
		}
		return nil
	})
	if err != nil {
		return folded{}, err
	}
	out.end = end
	out.torn = torn
	if updatedStr != "" {
		out.session.UpdatedAt = updatedStr
	}
	return out, nil
}

// laterTime keeps the max of two RFC 3339 stamps. RFC3339Nano drops trailing zeros, so
// the comparison is parsed, never lexicographic.
func laterTime(cur time.Time, curStr, next string) (time.Time, string) {
	if next == "" {
		return cur, curStr
	}
	t, err := time.Parse(time.RFC3339Nano, next)
	if err != nil {
		return cur, curStr
	}
	if curStr == "" || t.After(cur) {
		return t, next
	}
	return cur, curStr
}

// applyChanges folds one meta line onto a session.
func applyChanges(sess *core.Session, ch *changes) {
	if ch.Label != nil {
		sess.Label = *ch.Label
	}
	if ch.Pinned != nil {
		sess.Pinned = *ch.Pinned
	}
	if ch.Archived != nil {
		sess.Archived = *ch.Archived
	}
	if ch.TitleState != nil {
		sess.TitleState = *ch.TitleState
	}
	if ch.LastReadAt != nil {
		sess.LastReadAt = *ch.LastReadAt
	}
	if ch.Agent != nil {
		sess.Agent = *ch.Agent
	}
	if ch.Model != nil {
		sess.Model = *ch.Model
	}
	if ch.Provider != nil {
		sess.ProviderName = *ch.Provider
	}
	if ch.Workspace != nil {
		sess.Workspace = *ch.Workspace
	}
	if ch.Summary != nil {
		sess.Summary = *ch.Summary
	}
	if ch.Listed != nil {
		sess.MachineOwned = !*ch.Listed
	}
	if ch.ParentID != nil {
		sess.ParentID = *ch.ParentID
	}
	if ch.DelegateParent != nil {
		sess.DelegateParent = *ch.DelegateParent
	}
	if ch.ContextStart != nil {
		sess.ContextStart = *ch.ContextStart
	}
	if ch.ContextSummary != nil {
		sess.ContextSummary = *ch.ContextSummary
	}
	if ch.LastRun != nil {
		if *ch.LastRun == "" {
			sess.LastRun = nil
		} else {
			var stats core.LastRunStats
			if json.Unmarshal([]byte(*ch.LastRun), &stats) == nil {
				sess.LastRun = &stats
			}
		}
	}
	sess.PromptTokens += ch.AddPrompt
	sess.CompletionTokens += ch.AddCompletion
}

// truncateTorn cuts a file back to its last complete line. Only a torn tail (a crash
// mid-append) is ever cut; a healthy file is never touched.
func truncateTorn(path string, end int64) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Size() == end {
		return nil
	}
	return os.Truncate(path, end)
}

// readSessionFile reads one session's truth from disk.
func (s *Store) readSessionFile(id string) (folded, error) {
	path, err := s.sessionPath(id)
	if err != nil {
		return folded{}, err
	}
	f, err := foldFile(path)
	if err != nil {
		return folded{}, fmt.Errorf("store: read session %q: %w", id, err)
	}
	if f.session.ID == "" {
		f.session.ID = id
	}
	return f, nil
}
