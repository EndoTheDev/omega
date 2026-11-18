// The canvas transport (arc 3b): http_channel translates the canvas contract to the.
package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// NewCanvas wires the server's canvas from a mounted Context.
func (s *Server) newCanvas(ctx *core.Context) error {
	var err error
	s.canvas, err = agent.NewCanvas(ctx, s.CanvasEmit)
	return err
}

// CanvasEmit is the delta emitter the canvas runtime calls on every panel Update.
// Inline store-less runs register no run, so they see no deltas; the durable /chat path
// is the contract's home.
func (s *Server) CanvasEmit(event core.Event) {
	eventType, data, err := sseEvent(event)
	if err != nil {
		return
	}
	s.runs.broadcastAll(runFrame{event: eventType, data: data})
	// The push channel's canvas topic.
	s.sockets.PublishEvent(TopicCanvas, "", eventType, data)
}

// handleCanvas serves GET /ui/canvas. The body is the JSON array of panel descriptors
// verbatim (Canvas.Snapshot: mount order, panel ids sorted within a module).
func (s *Server) handleCanvas(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	panels := []core.Panel{}
	if s.canvas != nil {
		panels = s.canvas.Snapshot()
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(panels); err != nil {
		http.Error(w, "encode snapshot: "+err.Error(), http.StatusInternalServerError)
		return
	}
}

// uiActionResponse wraps a successful action pipe result. Actions
// return anything; the wrapper keeps the body self-describing and
// renders a clean {"result":null} for the nil case.
type uiActionResponse struct {
	Result any `json:"result"`
}

// handleUIActions serves POST /ui/actions. Nil canvas means nothing mounted: no panels
// to list and no capsule to route to - the sentinels' answer is the 404.
func (s *Server) handleUIActions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.canvas == nil {
		http.Error(w, core.ErrUnknownModule.Error(), http.StatusNotFound)
		return
	}
	var call core.ActionCall
	if err := json.NewDecoder(r.Body).Decode(&call); err != nil {
		http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}
	result, err := s.canvas.Dispatch(call)
	if errors.Is(err, core.ErrUnknownModule) || errors.Is(err, core.ErrUnknownAction) {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if err != nil {
		// A runError carries its own HTTP status (the same surface the
		// chat verbs serve: a retry against a session with a live run is
		// a 409 through the pipe, exactly like POST /chat answers).
		if re, ok := err.(*runError); ok {
			http.Error(w, err.Error(), re.code)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(uiActionResponse{Result: result}) // best-effort response body
}
