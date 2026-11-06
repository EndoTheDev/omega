// The finished-things REST face, riding the notifications capsule
// (shell-ring shrink wave 3, card t_b75df168 — moved whole from
// agent/api/server_notify.go, package unchanged, URLs unchanged).
//
// GET /notifications (list newest-first, ?unread=1 filter,
// ?limit/?offset pagination, the payload also carries the live unread
// count), POST /notifications/read (mark one by id or everything via
// {"all":true}), DELETE /notifications (the webui tab's clear-all:
// every record drops) and GET /notifications/events (SSE: every
// subsequent emission becomes one "notification" frame - the webui
// toast source).
//
// The routes hold the SAME emittingStore the mount offers under the
// "notifications" key: mark-read goes through the capsule's wrapper
// (the canvas panels' snapshot rides the same push), and a disabled
// capsule answers 404 at the module mux - the routes mount and die
// with their capsule, like every module REST face.
package notifications

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// notificationsListDefault is the page size when no limit is given.
const notificationsListDefault = 50

// notificationsListMax caps a client-provided limit (the retention cap
// is 200 - fetching more than that exists is waste).
const notificationsListMax = 200

// notifySSEPing keeps the stream honest through silent stretches.
const notifySSEPing = 30 * time.Second

// Routes is the capsule's REST face over the mounted emittingStore.
type Routes struct {
	svc *emittingStore
}

// newRoutes builds the REST face over the mount's service handle.
func newRoutes(e *emittingStore) *Routes { return &Routes{svc: e} }

// setupSSE sets SSE response headers and returns the flusher. On failure
// an HTTP error is written to w and ok is false.
func setupSSE(w http.ResponseWriter) (http.Flusher, bool) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return nil, false
	}
	return flusher, true
}

// writeSSE writes one SSE frame in the format
// "event: <type>\ndata: <json>\n\n".
func writeSSE(w io.Writer, eventType string, data []byte) {
	fmt.Fprintf(w, "event: %s\n", eventType)
	fmt.Fprintf(w, "data: %s\n\n", data)
}

// writeJSON serves the JSON responses.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// handleNotifications serves GET /notifications (the list) and
// DELETE /notifications (the webui tab's clear-all button).
func (rt *Routes) handleNotifications(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		rt.listNotifications(w, r)
	case http.MethodDelete:
		rt.clearNotifications(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// listNotifications serves GET /notifications.
func (rt *Routes) listNotifications(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	unreadOnly := q.Get("unread") == "1" || q.Get("unread") == "true"
	limit := notificationsListDefault
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			http.Error(w, "invalid limit", http.StatusBadRequest)
			return
		}
		limit = n
	}
	if limit > notificationsListMax {
		limit = notificationsListMax
	}
	offset := 0
	if v := q.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			http.Error(w, "invalid offset", http.StatusBadRequest)
			return
		}
		offset = n
	}
	notes, unread, err := rt.svc.List(r.Context(), unreadOnly, limit, offset)
	if err != nil {
		http.Error(w, "list notifications: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"notifications": notes,
		"unread":        unread,
	})
}

// clearNotifications serves DELETE /notifications: every record drops
// (the feed empties, not just its read flags). The response carries
// the deleted count so the caller can say what it removed.
func (rt *Routes) clearNotifications(w http.ResponseWriter, r *http.Request) {
	n, err := rt.svc.Clear(r.Context())
	if err != nil {
		http.Error(w, "clear notifications: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "cleared": n})
}

// handleNotificationsRead serves POST /notifications/read: {"id": N}
// marks one record, {"all": true} marks everything read.
func (rt *Routes) handleNotificationsRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		ID  *int64 `json:"id"`
		All bool   `json:"all"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}
	switch {
	case body.All:
		if err := rt.svc.MarkAllRead(r.Context()); err != nil {
			http.Error(w, "mark all read: "+err.Error(), http.StatusInternalServerError)
			return
		}
	case body.ID != nil:
		if err := rt.svc.MarkRead(r.Context(), *body.ID); err != nil {
			http.Error(w, "mark read: "+err.Error(), http.StatusNotFound)
			return
		}
	default:
		http.Error(w, "id or all is required", http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleNotificationEvents streams every subsequently emitted
// record as an SSE "notification" frame. The client closes (the
// browser tab, the fetch's cancel); the context cancellation path
// releases the store subscription - the store itself never holds the
// response writer.
func (rt *Routes) handleNotificationEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := setupSSE(w)
	if !ok {
		return
	}
	frames, cancel := rt.svc.Subscribe()
	defer cancel()

	// Keepalive ticks guard against idle-stream proxies closing the
	// connection under the client (the same concern a comment line
	// answers in any SSE surface).
	ticker := time.NewTicker(notifySSEPing)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case n, ok := <-frames:
			if !ok {
				// the store shut down: end the stream cleanly
				return
			}
			data, err := json.Marshal(n)
			if err != nil {
				// a frame that cannot marshal is skipped, not fatal
				continue
			}
			writeSSE(w, "notification", data)
			flusher.Flush()
		case <-ticker.C:
			if _, err := w.Write([]byte(": ping\n\n")); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
