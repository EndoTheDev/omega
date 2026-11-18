package api

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
// user - a user turn: content + images + time
// message - an assistant text segment: content + time + response_ms
// thinking - an assistant reasoning segment: content + thinking_ms
// tool_call - tool meta (tool_call: {id, name, arguments}) - the same
//	              nested shape the live SSE tool_call frame carries
//	tool_result - the outcome of a call: tool_call_id + content + is_error +
//	              gate + elapsed_ms + answer_ms, delivered where history
//	              placed it (after the calls) so the caller merges it like a
//	              live tool_result frame
//
// The shape is the client transcript's component contract: items restore
// and live stream render through the same components. ID
// is the message's store row id as a string - user items need it to
// address branch points on the variants surface; live-tail items (the
// run still holds the row) carry none.
type transcriptItem struct {
	Kind       string         `json:"kind"`
	ID         string         `json:"id,omitempty"`
	Content    string         `json:"content,omitempty"`
	Time       string         `json:"time,omitempty"` // user/message: store created_at, message timestamp as fallback
	ResponseMs int64          `json:"response_ms,omitempty"`
	ThinkingMs int64          `json:"thinking_ms,omitempty"`
	ToolCall   *core.ToolCall `json:"tool_call,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	IsError    bool           `json:"is_error,omitempty"`
	Gate      string `json:"gate,omitempty"`
	ElapsedMs int64  `json:"elapsed_ms,omitempty"`
	AnswerMs int64               `json:"answer_ms,omitempty"`
	Images   []core.ImageContent `json:"images,omitempty"` // user: core.User.Images rides the restore
	Attachments []core.Attachment `json:"attachments,omitempty"`
	// running marks the LIVE tail's in-flight items (the transcript-refs merge). Persisted
	// items never carry it.
	Running bool `json:"running,omitempty"`
	// StartedAt is an UNANSWERED tool_call's open wall clock, RFC 3339. A start stamp
	// cannot go stale, however long the payload is cached.
	StartedAt string `json:"started_at,omitempty"`
}

// handleSessionTranscript serves GET /sessions/{id}/transcript. Items come from the
// ACTIVE PATH: GetActiveView derives the tree from the persisted parent pointers and
// returns the render path - sessions without pointers derive the linear append order
// and render exactly as they always did - plus the variants block (branch points with
// >=2 sibling runs; absent for sessions without variants). The active path IS the final
// order (assistant segments carry thinking -> calls -> content in message order; tool
// results sit where history placed them), so timing and ordering are the store's facts,
// recomputed on every read: old sessions written before a persistence fix keep whatever
// shape they were saved.
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
	// A branch-pinned live run (retry, fork-of-user) cuts the old sibling's rows off the
	// path: the tail below renders into the response's slot, never stacked under the prior
	// answer.
	path := view.Path
	if branch, floor, pinned := s.runs.runBranch(id); pinned {
		path = branchPath(path, branch, floor)
	}
	items := buildTranscriptItems(path)
	// Reattach completeness: a session with a live run owns items the store has not seen.
	// The tail is a SNAPSHOT (rebuilt per event), so its running tool line ages with the
	// snapshot: a reload mid-call must anchor the meter at the call's true elapsed, not
	// the call-open anchor the last event baked.
	if tail, builtAt, live := s.runs.tailSnapshot(id); live {
		items = composeItems(items, freshenRunningCalls(tail, time.Since(builtAt)))
	}
	if len(view.Branches) > 0 {
		writeJSON(w, http.StatusOK, map[string]any{
			"session": sess,
			"items":   items,
			"running": s.runs.active(id),
			"variants": view.Branches,
		})
		return
	}
	// No variants: the key stays ABSENT, matching every pre-tree
	// transcript response byte for byte (backward-compat wire).
	writeJSON(w, http.StatusOK, map[string]any{
		"session": sess,
		"items":   items,
		"running": s.runs.active(id),
	})
}

// buildTranscriptItems maps persisted history into wire-order transcript items.
func buildTranscriptItems(msgs []core.StoredMessage) []transcriptItem {
	items := make([]transcriptItem, 0, len(msgs))
	answered := answeredCalls(msgs)
	for i, sm := range msgs {
		id := core.MsgID(sm.ID)
		switch m := sm.Message.(type) {
		case core.User:
			if m.Content == "" && len(m.Images) == 0 && len(m.Attachments) == 0 {
				continue
			}
			items = append(items, transcriptItem{
				Kind:    "user",
				ID:      id,
				Content: m.Content,
				Time:    transcriptTime(sm.CreatedAt, m.Timestamp),
				Images: m.Images,
				Attachments: m.Attachments,
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
				it := transcriptItem{Kind: "tool_call", ToolCall: &tc}
				if !answered[tc.ID] {
					it.StartedAt = transcriptTime(sm.CreatedAt, m.Timestamp)
				}
				items = append(items, it)
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
				Gate:       m.Gate,
			}
			if m.Timing != nil {
				it.ElapsedMs = m.Timing.ElapsedMs
			}
			it.AnswerMs = answerWindowMs(msgs, i+1)
			items = append(items, it)
		case core.YieldResult:
			if m.ToolCallID == "" {
				continue
			}
			it := transcriptItem{
				Kind:       "tool_result",
				ID:         id,
				ToolCallID: m.ToolCallID,
				Content:    m.Content,
				IsError:    m.IsError,
				Gate:       m.Gate,
			}
			if m.Timing != nil {
				it.ElapsedMs = m.Timing.ElapsedMs
			}
			it.AnswerMs = answerWindowMs(msgs, i+1)
			items = append(items, it)
		}
	}
	return items
}

// answeredCalls collects the call ids history holds a result row.
func answeredCalls(msgs []core.StoredMessage) map[string]bool {
	answered := make(map[string]bool, len(msgs))
	for _, sm := range msgs {
		switch m := sm.Message.(type) {
		case core.ToolResult:
			if m.ToolCallID != "" {
				answered[m.ToolCallID] = true
			}
		case core.YieldResult:
			if m.ToolCallID != "" {
				answered[m.ToolCallID] = true
			}
		}
	}
	return answered
}

// callStartStamp renders a call's open wall clock for the wire (the
// StartedAt shape): RFC 3339 with nanoseconds, UTC, the same clock
// core.NowISO writes - so a caller can parse it back losslessly.
func callStartStamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// answerWindowMs looks ahead from a tool result's position for the answer segment.
func answerWindowMs(msgs []core.StoredMessage, from int) int64 {
	for _, sm := range msgs[from:] {
		switch m := sm.Message.(type) {
		case core.User:
			return 0
		case core.Assistant:
			if m.Content != "" && m.Timing != nil {
				return m.Timing.ResponseMs
			}
			return 0
		}
	}
	return 0
}

// transcriptTime picks the display timestamp.
func transcriptTime(createdAt, stamp string) string {
	if createdAt != "" {
		return createdAt
	}
	return stamp
}

// liveTranscriptItems maps a detached run's accumulated state into transcript wire.
// Pure and race-free: the run's own goroutine builds the slice just after
// accumulateAssistant advanced the records, so callStarts reads (and clears) race
// nothing; the hub stores the finished slice by replace, never mutation.
func liveTranscriptItems(closed []assistantRecord, open assistantRecord, now time.Time) []transcriptItem {
	var items []transcriptItem
	for _, rec := range closed {
		items = appendRecordItems(items, rec, false, now)
	}
	return appendRecordItems(items, open, true, now)
}

// freshenRunningCalls advances a snapshot's running tool lines by the snapshot's age.
// Copies on the way out: a stored snapshot is replaced, never mutated in place, and
// readers race the next rebuild.
func freshenRunningCalls(items []transcriptItem, age time.Duration) []transcriptItem {
	if age <= 0 {
		return items
	}
	out := make([]transcriptItem, len(items))
	copy(out, items)
	for i := range out {
		if out[i].Kind != "tool_call" || !out[i].Running {
			continue
		}
		out[i].ElapsedMs += age.Milliseconds()
	}
	return out
}

// appendRecordItems flattens one accumulator record into wire items.
func appendRecordItems(items []transcriptItem, rec assistantRecord, open bool, now time.Time) []transcriptItem {
	if rec.thinking != "" {
		it := transcriptItem{Kind: "thinking", Content: rec.thinking}
		// the reasoning phase is over once content started or a tool call closed the segment.
		if open && !rec.segDone && rec.content == "" {
			it.Running = true
			it.ElapsedMs = phaseElapsed(rec.thinkDur, rec.lastThink, rec.openAt, now)
		} else {
			it.ThinkingMs = rec.thinkDur.Milliseconds()
		}
		items = append(items, it)
	}
	if rec.content != "" {
		it := transcriptItem{Kind: "message", Content: rec.content}
		it.Running = open && !rec.segDone
		if it.Running {
			it.ElapsedMs = phaseElapsed(rec.respDur, rec.lastResp, rec.openAt, now)
		} else {
			// a finished segment's measured span: the same number the persisted row will carry,
			// so the landing turn never snaps the label and a remount mid-call never ticks it
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
				// the same clock as a start STAMP.
				it.StartedAt = callStartStamp(start)
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
			Gate:       r.Gate,
		}
		if r.Timing != nil {
			it.ElapsedMs = r.Timing.ElapsedMs
		}
		items = append(items, it)
	}
	return items
}
