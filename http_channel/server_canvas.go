// The canvas transport (arc 3b): http_channel translates the canvas
// contract to the web. The snapshot + deltas pattern (settled Q3b):
// GET /ui/canvas serves the current panel state as the load-time
// catch-up, and ViewModelDelta events serialize as one SSE frame type
// on the existing run streams - the same stream the chat already
// rides, no second channel (settled Q10a). POST /ui/actions is the
// web half of the one action pipe (settled Q4b): module-qualified
// calls route through agent.Canvas.Dispatch to the capsule's
// registered handler - zero per-module code in this file. The
// per-module REST routes stay untouched (settled Q6a); the pipe is
// additive.
package http_channel

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// NewCanvas wires the server's canvas from a mounted Context. The
// emitter is the server's delta fan-out: modules updating panel state
// during a run emit ViewModelDeltas onto every live run's stream.
func (s *Server) newCanvas(ctx *core.Context) error {
	var err error
	s.canvas, err = agent.NewCanvas(ctx, s.canvasEmit)
	return err
}

// canvasEmit is the delta emitter the canvas runtime calls on every
// panel Update: the delta serializes once (sseEvent - the same
// translation every run event takes) and fans out to every live
// run's subscribers. Frames stay on the existing streams; a delta
// landing outside any run's lifetime is dropped - the frontend's
// next catch-up is the snapshot (GET /ui/canvas), the same way
// sessions' missed history lives in the store, not the stream.
// Inline store-less runs register no run, so they see no deltas;
// the durable /chat path is the contract's home.
func (s *Server) canvasEmit(event core.Event) {
	eventType, data, err := sseEvent(event)
	if err != nil {
		return
	}
	s.runs.broadcastAll(runFrame{event: eventType, data: data})
}

// handleCanvas serves GET /ui/canvas: the current panel snapshot of
// every mounted module - the webui's load-time catch-up (settled
// Q3b). The body is the JSON array of panel descriptors verbatim
// (Canvas.Snapshot: mount order, panel ids sorted within a module).
// An empty canvas is a 200 with [] - no panels mounted is a state,
// not an error.
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

// handleUIActions serves POST /ui/actions: the web half of the one
// action pipe (settled Q4b). The body is a module-qualified
// core.ActionCall verbatim; the canvas runtime routes it. Route
// failures translate to 404 (unknown module or action - the sentinels
// decide, errors.Is); the handler's own error is a 500; success
// returns the handler's result under {"result": ...}.
// Nil canvas means nothing mounted: no panels to list and no capsule
// to route to - the sentinels' answer is the 404.
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
	json.NewEncoder(w).Encode(uiActionResponse{Result: result}) //nolint:errcheck // best-effort response body
}
