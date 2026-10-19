package http_channel

import (
	"net/http"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/ai"
)

// transcriptItem is one render-ready item of GET /sessions/{id}/transcript:
// the backend's final answer to "what did this session look like on the
// wire", so the client's restore path is 'render items' - no reload-side
// reconstruction (order, thinking merge, durations were three bug classes
// that reconstruction produced). kinds:
//
//	user        - a user turn: content + time
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
// and live stream render through the same components.
type transcriptItem struct {
	Kind       string       `json:"kind"`
	Content    string       `json:"content,omitempty"`
	Time       string       `json:"time,omitempty"` // user/message: store created_at, message timestamp as fallback
	ResponseMs int64        `json:"response_ms,omitempty"`
	ThinkingMs int64        `json:"thinking_ms,omitempty"`
	ToolCall   *ai.ToolCall `json:"tool_call,omitempty"`
	ToolCallID string       `json:"tool_call_id,omitempty"`
	IsError    bool         `json:"is_error,omitempty"`
	ElapsedMs  int64        `json:"elapsed_ms,omitempty"`
}

// handleSessionTranscript serves GET /sessions/{id}/transcript: the
// render-ready item stream for the webui restore path (one request on
// session open replaces both the history fetch and its reconstruction
// loop). Same store gates as the other session endpoints: 501 without a
// store, 404 for an unknown or deleted session, 500 on a store fault.
// Items come from GetMessagesWithTimes in append order - the persisted
// order IS the final order (assistant segments carry thinking -> calls ->
// content in message order; tool results sit where history placed them),
// so timing and ordering are the store's facts, recomputed on every read:
// old sessions written before a persistence fix keep whatever shape they
// were saved with.
func (s *Server) handleSessionTranscript(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.store == nil {
		http.Error(w, "session store not configured", http.StatusNotImplemented)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing session id", http.StatusBadRequest)
		return
	}
	sess, err := s.store.GetSession(r.Context(), id)
	if err != nil {
		http.Error(w, "session not found: "+err.Error(), http.StatusNotFound)
		return
	}
	bare, err := s.store.GetMessagesWithTimes(r.Context(), id)
	if err != nil {
		http.Error(w, "get messages: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"session": sess,
		"items":   buildTranscriptItems(bare),
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
func buildTranscriptItems(msgs []agent.StoredMessage) []transcriptItem {
	items := make([]transcriptItem, 0, len(msgs))
	for _, sm := range msgs {
		switch m := sm.Message.(type) {
		case ai.User:
			if m.Content == "" {
				continue
			}
			items = append(items, transcriptItem{
				Kind:    "user",
				Content: m.Content,
				Time:    transcriptTime(sm.CreatedAt, m.Timestamp),
			})
		case ai.Assistant:
			if m.Thinking != nil && *m.Thinking != "" {
				it := transcriptItem{
					Kind:    "thinking",
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
		case ai.ToolResult:
			if m.ToolCallID == "" {
				continue
			}
			it := transcriptItem{
				Kind:       "tool_result",
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