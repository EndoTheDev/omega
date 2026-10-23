package http_channel

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/ai"
	"github.com/EndoTheDev/omega/core"
)

// Ephemeral sessions (card t_d96a5cd5): a chat that never touches the
// store - no session row, no messages on disk, invisible to every
// store-derived surface (lists, search, export, counts, the TUI's
// /sessions). The conversation's whole life is THIS registry: an
// in-memory record keyed by an `eph-` id, reattachable by id on reload
// (server memory) and gone at restart. Close deletes the record - that
// is the contract; a persist-action is a separate follow-up card.
//
// The id prefix is the invisibility law's enforcement point: endpoints
// that read the store route `eph-` ids here first (transcript, session
// detail, delete, read-stamp) or 404 naturally (variants, branch,
// search - the store never saw the row).

// ephemeralSession is one no-persist chat in server memory. context is
// the provider-facing history (user turns + assistant records + tool
// results, appended on every /chat); items is the wire transcript the
// restore path serves; usage/last mirror what the session row would
// carry (the status bar's durable telemetry).
type ephemeralSession struct {
	id        string
	createdAt time.Time
	updatedAt time.Time
	history   []ai.Message
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

func newEphRegistry() *ephRegistry {
	return &ephRegistry{records: map[string]*ephemeralSession{}}
}

// ephemeralPrefix is the id namespace of no-persist sessions.
const ephemeralPrefix = "eph-"

// isEphemeralID reports whether an id belongs to the ephemeral
// namespace. Store ids are base session ids from agent.NewSessionID or
// the sess- fallback; they can never carry the prefix.
func isEphemeralID(id string) bool {
	return len(id) > len(ephemeralPrefix) && id[:len(ephemeralPrefix)] == ephemeralPrefix
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
		// lazy: a zero-value registry (a Server built without the
		// constructor path) self-initializes
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

// drop deregisters the record. ok reports whether it existed.
func (r *ephRegistry) drop(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.records[id]; !ok {
		return false
	}
	delete(r.records, id)
	return true
}

// appendUser records a send's incoming messages into the wire
// transcript items (the same user item shape persisted history
// produces, minus the store row id - the variants surface addresses
// ids, and an ephemeral session has none). The provider-facing history
// copy lands through the loop's eph sink at run start (t_471f6d4a).
func (r *ephRegistry) appendUser(id string, messages []ai.Message, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.records[id]
	if !ok {
		return
	}
	for _, m := range messages {
		if u, isUser := m.(ai.User); isUser {
			item := transcriptItem{Kind: "user", Content: u.Content, Time: now.UTC().Format(time.RFC3339)}
			if len(u.Images) > 0 {
				item.Images = u.Images
			}
			s.items = append(s.items, item)
		}
	}
	s.updatedAt = now.UTC()
}

// appendHistory persists one loop-produced turn copy into the record's
// provider-facing history - the ephemeral conversation's store
// equivalent. Best effort: a record that vanished (close, restart)
// drops the write silently.
func (r *ephRegistry) appendHistory(id string, msg ai.Message, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.records[id]
	if !ok {
		return
	}
	s.history = append(s.history, msg)
	s.updatedAt = now.UTC()
}

// ephTurnSink is the ephemeral session's TurnSink (t_471f6d4a): the
// loop is the only writer of turn copies, and this registry is the
// ephemeral conversation's store. Empty assistants write nothing - the
// wire items commit applies the same guard on the rendering side.
type ephTurnSink struct {
	r  *ephRegistry
	id string
}

func (s ephTurnSink) AppendTurnMessage(ctx context.Context, msg ai.Message) error {
	if a, ok := msg.(ai.Assistant); ok &&
		a.Content == "" &&
		(a.Thinking == nil || *a.Thinking == "") &&
		len(a.ToolCalls) == 0 {
		return nil
	}
	s.r.appendHistory(s.id, msg, time.Now())
	return nil
}

// commit is the run-end epilogue: flattened assistant records go into
// the transcript items via the same appendRecordItems flattening the
// live tail uses (closed: no running flags, durations attached). The
// provider history itself was written turn-by-turn through the eph
// sink while the run streamed. Usage/last mirror the session-row
// writes.
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
// Label stays empty: the ephemeral chat is a tab, not a sidebar row;
// the webui names its tab client-side.
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
func (r *ephRegistry) history(id string) []ai.Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.records[id]
	if !ok {
		return nil
	}
	out := make([]ai.Message, len(s.history))
	copy(out, s.history)
	return out
}

// snapshot copies the record's items (REPLACE semantics like the run
// hub's tail: the reader gets a coherent slice even mid-commit).
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

// withoutMemory copies the tool registry minus the memory tool - the
// ephemeral session's run sees every tool but memory (the deny rides
// the run context too; this copy is the registry the /chat loop passes
// in, so the two layers agree even before the loop filter). The name
// matches modules/memory's registered tool (targetMemory).
func withoutMemory(tools map[string]core.Tool) map[string]core.Tool {
	if _, has := tools["memory"]; !has {
		return tools
	}
	out := make(map[string]core.Tool, len(tools)-1)
	for name, t := range tools {
		if name != "memory" {
			out[name] = t
		}
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
		// Close = gone, even mid-run: a live run is cancelled first so
		// nothing keeps writing into a dropped record. The hub's finish
		// path is a no-op for the gone id (cur==run guard misses).
		s.runs.stop(id)
		s.eph.drop(id)
		w.WriteHeader(http.StatusNoContent)
	case http.MethodPatch:
		// List concerns (read/pin/archive) are no-ops: there is no row.
		// A 204 keeps the webui's open-equals-read call silent.
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
	return true
}

// serveEphemeralTranscript synthesizes GET /sessions/{id}/transcript:
// the record's items plus the live run's tail (identical merge to the
// store path), the running flag, no variants key. ok=false leaves the
// request to the caller.
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
	if tail, live := s.runs.tailItems(id); live {
		items = append(items, tail...)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"session": eph.session(),
		"items":   items,
		"running": s.runs.active(id),
	})
	return true
}
