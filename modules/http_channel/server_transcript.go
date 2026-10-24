package http_channel

import (
	"net/http"
	"time"

	"github.com/EndoTheDev/omega/core"
)

// transcriptItem is one render-ready item of GET /sessions/{id}/transcript:
// the backend's final answer to "what did this session look like on the
// wire", so the client's restore path is 'render items' - no reload-side
// reconstruction (order, thinking merge, durations were three bug classes
// that reconstruction produced). kinds:
//
//	user        - a user turn: content + images + time
//	message     - an assistant text segment: content + time + response_ms
//	thinking    - an assistant reasoning segment: content + thinking_ms
//	tool_call   - tool meta (tool_call: {id, name, arguments}) - the same
//	              nested shape the live SSE tool_call frame carries
//	tool_result - the outcome of a call: tool_call_id + content + is_error +
//	              elapsed_ms, delivered where history placed it (after the
//	              calls) so the caller merges it like a live tool_result
//	              frame
//
// The shape is the client transcript's component contract: items restore
// and live stream render through the same components. ID (t_17365247)
// is the message's store row id as a string - user items need it to
// address branch points on the variants surface; live-tail items (the
// run still holds the row) carry none.
type transcriptItem struct {
	Kind       string              `json:"kind"`
	ID         string              `json:"id,omitempty"`
	Content    string              `json:"content,omitempty"`
	Time       string              `json:"time,omitempty"` // user/message: store created_at, message timestamp as fallback
	ResponseMs int64               `json:"response_ms,omitempty"`
	ThinkingMs int64               `json:"thinking_ms,omitempty"`
	ToolCall   *core.ToolCall      `json:"tool_call,omitempty"`
	ToolCallID string              `json:"tool_call_id,omitempty"`
	IsError    bool                `json:"is_error,omitempty"`
	ElapsedMs  int64               `json:"elapsed_ms,omitempty"`
	Images     []core.ImageContent `json:"images,omitempty"` // user: core.User.Images rides the restore
	// running marks the LIVE tail's in-flight items (the transcript-refs
	// merge): an open thinking/response block streams from the restored
	// item, and a tool call without its result yet renders as the
	// running line with elapsed-so-far - the same shapes the live SSE
	// path paints. Persisted items never carry it.
	Running bool `json:"running,omitempty"`
}

// handleSessionTranscript serves GET /sessions/{id}/transcript: the
// render-ready item stream for the webui restore path (one request on
// session open replaces both the history fetch and its reconstruction
// loop). Same store gates as the other session endpoints: 501 without a
// store, 404 for an unknown or deleted session, 500 on a store fault.
// Items come from the ACTIVE PATH (t_17365247): GetActiveView derives
// the tree from the persisted parent pointers and returns the render
// path - sessions without pointers derive the linear append order and
// render exactly as they always did - plus the variants block (branch
// points with >=2 sibling runs; absent for sessions without variants).
// The active path IS the final order (assistant segments carry thinking
// -> calls -> content in message order; tool results sit where history
// placed them), so timing and ordering are the store's facts,
// recomputed on every read: old sessions written before a persistence
// fix keep whatever shape they were saved with.
func (s *Server) handleSessionTranscript(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing session id", http.StatusBadRequest)
		return
	}
	// Ephemeral transcripts come from the registry (card t_d96a5cd5);
	// ok=false hands non-eph ids to the store path below.
	if s.serveEphemeralTranscript(w, id) {
		return
	}
	if s.store == nil {
		http.Error(w, "session store not configured", http.StatusNotImplemented)
		return
	}
	sess, err := s.store.GetSession(r.Context(), id)
	if err != nil {
		http.Error(w, "session not found: "+err.Error(), http.StatusNotFound)
		return
	}
	view, err := s.store.GetActiveView(r.Context(), id)
	if err != nil {
		http.Error(w, "get transcript: "+err.Error(), http.StatusInternalServerError)
		return
	}
	items := buildTranscriptItems(view.Path)
	// Reattach completeness (card t_692254f7): a session with a live run
	// owns items the store has not seen yet - the run persists only at
	// its end, so its segments (open thinking, running tool call, the
	// answer so far) live in the runHub's accumulator. The live tail
	// merges BEHIND the persisted history in wire order: the restored
	// view shows the in-flight tool row with its elapsed meter and the
	// attached result lands on it, instead of the refresh dropping the
	// run - the "transcript is gone, but it says streaming" report.
	if tail, live := s.runs.tailItems(id); live {
		items = append(items, tail...)
	}
	if len(view.Branches) > 0 {
		writeJSON(w, http.StatusOK, map[string]any{
			"session": sess,
			"items":   items,
			// live-run flag (always present, running:true/false): the
			// runHub, not the client, owns "is this session streaming" -
			// the webui reattach flow reads this on session open
			"running": s.runs.active(id),
			// variants block (t_17365247): branch points with >=2
			// sibling runs - message_id (the parent), count, the active
			// index, the sibling head ids.
			"variants": view.Branches,
		})
		return
	}
	// No variants: the key stays ABSENT, matching every pre-tree
	// transcript response byte for byte (backward-compat wire).
	writeJSON(w, http.StatusOK, map[string]any{
		"session": sess,
		"items":   items,
		// live-run flag (always present, running:true/false): the runHub,
		// not the client, owns "is this session streaming" - the webui
		// reattach flow reads this on session open
		"running": s.runs.active(id),
	})
}

// buildTranscriptItems maps persisted history into wire-order transcript
// items. Pure: no store, no server state. One assistant message yields
// thinking (first), then its text, then its calls - the order the live
// stream produced them. Tool-role messages become tool_result items in
// place (they are appended after the calls in history). Everything else
// in the stream of records (system, model_change, thinking_level_change)
// and any thin message (no content, no thinking, no calls) renders
// nothing.
func buildTranscriptItems(msgs []core.StoredMessage) []transcriptItem {
	items := make([]transcriptItem, 0, len(msgs))
	for _, sm := range msgs {
		id := core.MsgID(sm.ID)
		switch m := sm.Message.(type) {
		case core.User:
			if m.Content == "" && len(m.Images) == 0 {
				continue
			}
			items = append(items, transcriptItem{
				Kind:    "user",
				ID:      id,
				Content: m.Content,
				Time:    transcriptTime(sm.CreatedAt, m.Timestamp),
				// images ride the restore (scope add 2026-10-20): the
				// persisted payload carries core.User.Images, the client
				// renders the thumbnails - without this the attach
				// pipeline's image messages come back as text-only
				Images: m.Images,
			})
		case core.Assistant:
			if m.Thinking != nil && *m.Thinking != "" {
				it := transcriptItem{
					Kind:    "thinking",
					ID:      id,
					Content: *m.Thinking,
					Time:    transcriptTime(sm.CreatedAt, m.Timestamp),
				}
				if m.Timing != nil {
					it.ThinkingMs = m.Timing.ThinkingMs
				}
				items = append(items, it)
			}
			if m.Content != "" {
				it := transcriptItem{
					Kind:    "message",
					ID:      id,
					Content: m.Content,
					Time:    transcriptTime(sm.CreatedAt, m.Timestamp),
				}
				if m.Timing != nil {
					it.ResponseMs = m.Timing.ResponseMs
				}
				items = append(items, it)
			}
			for i := range m.ToolCalls {
				tc := m.ToolCalls[i]
				items = append(items, transcriptItem{Kind: "tool_call", ToolCall: &tc})
			}
		case core.ToolResult:
			if m.ToolCallID == "" {
				continue
			}
			it := transcriptItem{
				Kind:       "tool_result",
				ID:         id,
				ToolCallID: m.ToolCallID,
				Content:    m.Content,
				IsError:    m.IsError,
			}
			if m.Timing != nil {
				it.ElapsedMs = m.Timing.ElapsedMs
			}
			items = append(items, it)
		}
	}
	return items
}

// transcriptTime picks the display timestamp: the store-level write time
// is authoritative (it is when the message actually landed); the
// message's own timestamp covers rows saved before created_at was
// recorded.
func transcriptTime(createdAt, stamp string) string {
	if createdAt != "" {
		return createdAt
	}
	return stamp
}

// liveTranscriptItems maps a detached run's accumulated state into
// transcript wire items - the runHub's live truth that GET
// /sessions/{id}/transcript merges behind the persisted history while
// the run holds the session's slot. closed segments already left the
// accumulator (a chunk after tool results opened the next segment);
// open is the record still taking events. Completed spans carry their
// measured durations; the open record's in-flight parts carry the
// running hint instead (no final durations on the wire - the client
// renders them streaming), and a call without its result yet renders as
// the running tool line with elapsed-so-far in ElapsedMs.
//
// Pure and race-free: the run's own goroutine builds the slice just
// after accumulateAssistant advanced the records, so callStarts reads
// (and clears) race nothing; the hub stores the finished slice by
// replace, never mutation.
func liveTranscriptItems(closed []assistantRecord, open assistantRecord, now time.Time) []transcriptItem {
	var items []transcriptItem
	for _, rec := range closed {
		items = appendRecordItems(items, rec, false, now)
	}
	return appendRecordItems(items, open, true, now)
}

// appendRecordItems flattens one accumulator record into wire items:
// thinking, then content, then the calls, then the results - the order
// the live stream produced them and persisted history replays.
// Running is true only on the open record's still-in-flight parts: its
// thinking streams while no content has started, its content streams,
// and every call the results have not answered is the running tool
// line. Pure.
func appendRecordItems(items []transcriptItem, rec assistantRecord, open bool, now time.Time) []transcriptItem {
	if rec.thinking != "" {
		it := transcriptItem{Kind: "thinking", Content: rec.thinking}
		if open {
			// the reasoning phase is over once content started - running
			// is the in-flight hint, not the record's life story
			if rec.content == "" {
				it.Running = true
			}
		} else {
			it.ThinkingMs = rec.thinkDur.Milliseconds()
		}
		items = append(items, it)
	}
	if rec.content != "" {
		it := transcriptItem{Kind: "message", Content: rec.content}
		it.Running = open
		if !open {
			it.ResponseMs = rec.respDur.Milliseconds()
		}
		items = append(items, it)
	}
	resulted := make(map[string]bool, len(rec.results))
	for _, r := range rec.results {
		resulted[r.ToolCallID] = true
	}
	for i := range rec.toolCalls {
		tc := rec.toolCalls[i]
		it := transcriptItem{Kind: "tool_call", ToolCall: &tc}
		if !resulted[tc.ID] {
			it.Running = true
			if start, ok := rec.callStarts[tc.ID]; ok && !start.IsZero() {
				it.ElapsedMs = now.Sub(start).Milliseconds()
			}
		}
		items = append(items, it)
	}
	for _, r := range rec.results {
		it := transcriptItem{
			Kind:       "tool_result",
			ToolCallID: r.ToolCallID,
			Content:    r.Content,
			IsError:    r.IsError,
		}
		if r.Timing != nil {
			it.ElapsedMs = r.Timing.ElapsedMs
		}
		items = append(items, it)
	}
	return items
}
