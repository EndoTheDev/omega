package http_channel

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

// The finished-things HTTP surface (card t_5d9032d3): GET /notifications
// (list newest-first, ?unread=1 filter, ?limit/?offset pagination, the
// payload also carries the live unread count), POST /notifications/read
// (mark one by id or everything via {"all":true}) and GET
// /notifications/events (SSE: every subsequent emission becomes one
// "notification" frame - the webui toast source).
//
// Every route answers 501 when the notifications seam is not mounted -
// the store is optional, the API degrades like the board's does.

// notificationsListDefault is the page size when no limit is given.
const notificationsListDefault = 50

// notificationsListMax caps a client-provided limit (the retention cap
// is 200 - fetching more than that exists is waste).
const notificationsListMax = 200

// notifySSEPing keeps the stream honest through silent stretches.
const notifySSEPing = 30 * time.Second

// notifyUnavailable is the shared 501 guard for unmounted notifications.
func notifyUnavailable(w http.ResponseWriter) {
	http.Error(w, "notifications store not configured", http.StatusNotImplemented)
}

// handleNotifications serves GET /notifications.
func (s *Server) handleNotifications(w http.ResponseWriter, r *http.Request) {
	if s.Notifications() == nil {
		notifyUnavailable(w)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
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
	notes, unread, err := s.Notifications().List(r.Context(), unreadOnly, limit, offset)
	if err != nil {
		http.Error(w, "list notifications: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"notifications": notes,
		"unread":        unread,
	})
}

// handleNotificationsRead serves POST /notifications/read: {"id": N}
// marks one record, {"all": true} marks everything read.
func (s *Server) handleNotificationsRead(w http.ResponseWriter, r *http.Request) {
	if s.Notifications() == nil {
		notifyUnavailable(w)
		return
	}
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
		if err := s.Notifications().MarkAllRead(r.Context()); err != nil {
			http.Error(w, "mark all read: "+err.Error(), http.StatusInternalServerError)
			return
		}
	case body.ID != nil:
		if err := s.Notifications().MarkRead(r.Context(), *body.ID); err != nil {
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
func (s *Server) handleNotificationEvents(w http.ResponseWriter, r *http.Request) {
	if s.Notifications() == nil {
		notifyUnavailable(w)
		return
	}
	flusher, ok := setupSSE(w)
	if !ok {
		return
	}
	frames, cancel := s.Notifications().Subscribe()
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