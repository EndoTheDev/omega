package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// Ephemeral sessions: a chat that never touches the store. Close deletes the record -
// that is the contract; a persist-action is a separate follow-up card. The id prefix is
// the invisibility law's enforcement point: endpoints that read the store route `eph-`
// ids here first (transcript, session detail, delete, read-stamp) or 404 naturally
// (variants, branch, search - the store never saw the row).

// ephemeralSession is one no-persist chat in server memory.
type ephemeralSession struct {
	id        string
	createdAt time.Time
	updatedAt time.Time
	history   []core.Message
	items     []transcriptItem
	usage     streamUsage
	last      *core.LastRunStats
}

// ephRegistry is the server-lifetime map. Concurrency: /chat sends and
// SSE readers race; the mutex guards the map and each record.
type ephRegistry struct {
	mu      sync.Mutex
	records map[string]*ephemeralSession
}

// ephemeralPrefix is the id namespace of no-persist sessions. The
// namespace constant is the agent package's (agent.EphemeralIDPrefix) -
// the id creators in the roster capsule enforce the same namespace, and
// a substrate-shared constant cannot drift from the capsule's guard.
const ephemeralPrefix = agent.EphemeralIDPrefix

// isEphemeralID reports whether an id belongs to the ephemeral namespace.
// Store ids are base session ids from agent.NewSessionID or the sess- fallback; they
// can never carry the prefix.
func isEphemeralID(id string) bool {
	return agent.IsEphemeralID(id)
}

// mintEphemeralID builds a fresh namespace id. crypto/rand never fails
// on supported platforms; the timestamp fallback keeps the run alive if
// it ever does (same shape as the store id fallbacks).
func mintEphemeralID() string {
	id, err := agent.NewSessionID()
	if err != nil {
		return fmt.Sprintf("%s%d", ephemeralPrefix, time.Now().UnixNano())
	}
	return ephemeralPrefix + id
}

// create registers a record for a new ephemeral session.
func (r *ephRegistry) create(id string) *ephemeralSession {
	now := time.Now().UTC()
	s := &ephemeralSession{id: id, createdAt: now, updatedAt: now}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.records == nil {
		r.records = map[string]*ephemeralSession{}
	}
	r.records[id] = s
	return s
}

// get returns the record, or nil when it is unknown/expired (restart).
func (r *ephRegistry) get(id string) *ephemeralSession {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.records[id]
}

// drop deregisters the record.
func (r *ephRegistry) drop(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.records[id]; !ok {
		return false
	}
	delete(r.records, id)
	return true
}

// appendUser records a send's incoming messages into the wire transcript items.
func (r *ephRegistry) appendUser(id string, messages []core.Message, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.records[id]
	if !ok {
		return
	}
	for _, m := range messages {
		if u, isUser := m.(core.User); isUser {
			item := transcriptItem{Kind: "user", Content: u.Content, Time: now.UTC().Format(time.RFC3339)}
			if len(u.Images) > 0 {
				item.Images = u.Images
			}
			if len(u.Attachments) > 0 {
				item.Attachments = u.Attachments
			}
			s.items = append(s.items, item)
		}
	}
	s.updatedAt = now.UTC()
}

// appendHistory persists one loop-produced turn copy into the record's. Best effort: a
// record that vanished (close, restart) drops the write silently.
func (r *ephRegistry) appendHistory(id string, msg core.Message, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.records[id]
	if !ok {
		return
	}
	s.history = append(s.history, msg)
	s.updatedAt = now.UTC()
}

// ephTurnSink is the ephemeral session's TurnSink.
type ephTurnSink struct {
	r  *ephRegistry
	id string
}

func (s ephTurnSink) AppendTurnMessage(ctx context.Context, msg core.Message) error {
	if a, ok := msg.(core.Assistant); ok &&
		a.Content == "" &&
		(a.Thinking == nil || *a.Thinking == "") &&
		len(a.ToolCalls) == 0 {
		return nil
	}
	s.r.appendHistory(s.id, msg, time.Now())
	return nil
}

// commit is the run-end epilogue.
func (r *ephRegistry) commit(id string, recs []assistantRecord, usage streamUsage, last *core.LastRunStats, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.records[id]
	if !ok {
		return
	}
	for _, rec := range recs {
		if rec.content == "" && rec.thinking == "" && len(rec.toolCalls) == 0 {
			continue
		}
		s.items = appendRecordItems(s.items, rec, false, now)
	}
	s.usage.prompt += usage.prompt
	s.usage.completion += usage.completion
	if last != nil {
		s.last = last
	}
	s.updatedAt = now.UTC()
}

// session synthesizes what a session row would say about the record.
func (s *ephemeralSession) session() core.Session {
	return core.Session{
		ID:               s.id,
		CreatedAt:        s.createdAt.Format(time.RFC3339),
		UpdatedAt:        s.updatedAt.Format(time.RFC3339),
		PromptTokens:     s.usage.prompt,
		CompletionTokens: s.usage.completion,
		LastRun:          s.last,
	}
}

// history returns a copy of the record's provider-facing history (the
// context the next /chat run replays) - a fresh slice per read, so the
// run's slice can never alias a concurrent commit's append.
func (r *ephRegistry) history(id string) []core.Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.records[id]
	if !ok {
		return nil
	}
	out := make([]core.Message, len(s.history))
	copy(out, s.history)
	return out
}

// snapshot copies the record's items.
func (r *ephRegistry) snapshotItems(id string) []transcriptItem {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.records[id]
	if !ok {
		return nil
	}
	out := make([]transcriptItem, len(s.items))
	copy(out, s.items)
	return out
}

// withoutMemoryTools copies the tool registry minus the memory domain.
func withoutMemoryTools(tools map[string]core.Tool) map[string]core.Tool {
	out := make(map[string]core.Tool, len(tools))
	stripped := false
	for name, t := range tools {
		if name == "memory" || strings.HasPrefix(name, "memory.") {
			stripped = true
			continue
		}
		out[name] = t
	}
	if !stripped {
		return tools
	}
	return out
}

// serveEphemeralDetail synthesizes GET /sessions/{id} for an eph id:
// the session payload, the unread stamp (never unread: nothing persists)
// and the live-run flag. ok=false leaves the request to the caller.
func (s *Server) serveEphemeralDetail(w http.ResponseWriter, r *http.Request, id string) bool {
	if !isEphemeralID(id) {
		return false
	}
	switch r.Method {
	case http.MethodGet:
		eph := s.eph.get(id)
		if eph == nil {
			http.Error(w, "session not found: ephemeral session expired", http.StatusNotFound)
			return true
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"session":   eph.session(),
			"messages":  []map[string]any{},
			"unread":    false,
			"running":   s.runs.active(id),
			"ephemeral": true,
		})
	case http.MethodDelete:
		s.stopRun(id)
		s.eph.drop(id)
		w.WriteHeader(http.StatusNoContent)
	case http.MethodPatch:
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
	return true
}

// serveEphemeralTranscript synthesizes GET /sessions/{id}/transcript. ok=false leaves
// the request to the caller.
func (s *Server) serveEphemeralTranscript(w http.ResponseWriter, id string) bool {
	if !isEphemeralID(id) {
		return false
	}
	eph := s.eph.get(id)
	if eph == nil {
		http.Error(w, "session not found: ephemeral session expired", http.StatusNotFound)
		return true
	}
	items := s.eph.snapshotItems(id)
	if tail, builtAt, live := s.runs.tailSnapshot(id); live {
		items = composeItems(items, freshenRunningCalls(tail, time.Since(builtAt)))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"session": eph.session(),
		"items":   items,
		"running": s.runs.active(id),
	})
	return true
}
