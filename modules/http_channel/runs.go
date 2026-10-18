package http_channel

import (
	"context"
	"errors"
	"net/http"
	"sync"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/ai"
)

// runFrame is one prepared SSE frame: the event type plus its JSON data.
// The run prepares each frame once; every attached subscriber replays it
// onto its own wire.
type runFrame struct {
	event string
	data  []byte
}

// subscriberBuffer bounds one subscriber's frame queue. The agent emits
// one StreamingChunk event per token delta, so a burst between two
// consumer wakeups stays comfortably inside this on a Pi.
const subscriberBuffer = 256

// activeRun is one detached /chat run. Events fan out to subscriber
// channels; a run with zero attached clients still runs to completion
// and persists - clients come and go, the run does not care.
type activeRun struct {
	id   string
	subs map[chan runFrame]struct{}
}

// runHub tracks detached runs by session id. It is the reattach
// surface's source of truth: /chat launches into it, GET
// /sessions/{id}/events subscribes through it, finish removes.
type runHub struct {
	mu   sync.Mutex
	runs map[string]*activeRun
}

// start registers a new run for the session. ok is false when the
// session already has a live run - a second /chat for the same session
// is rejected (the webui per-session busy gate normally prevents it;
// this is the server-side backstop).
func (h *runHub) start(id string) (*activeRun, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.runs == nil {
		h.runs = map[string]*activeRun{}
	}
	if _, exists := h.runs[id]; exists {
		return nil, false
	}
	run := &activeRun{id: id, subs: map[chan runFrame]struct{}{}}
	h.runs[id] = run
	return run, true
}

// active reports whether the session has a live run.
func (h *runHub) active(id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.runs[id]
	return ok
}

// get returns the session's live run, for subscribing.
func (h *runHub) get(id string) (*activeRun, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	run, ok := h.runs[id]
	return run, ok
}

// subscribe attaches one consumer channel to the run.
func (h *runHub) subscribe(run *activeRun) chan runFrame {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch := make(chan runFrame, subscriberBuffer)
	run.subs[ch] = struct{}{}
	return ch
}

// unsubscribe detaches one consumer: removed and closed. A channel no
// longer in subs was already cut (full queue) or finished (hub close),
// so its close already happened elsewhere - this must not double-close.
func (h *runHub) unsubscribe(run *activeRun, ch chan runFrame) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := run.subs[ch]; !ok {
		return
	}
	delete(run.subs, ch)
	close(ch)
}

// broadcast delivers one frame to every subscriber. A subscriber whose
// queue is full is CUT, never allowed to stall the run for everyone:
// the closed channel ends that SSE stream without agent_end, which the
// webui renders as an interrupted stream - the honest state for a wire
// that stopped moving. Other subscribers keep streaming.
func (h *runHub) broadcast(run *activeRun, f runFrame) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range run.subs {
		select {
		case ch <- f:
		default:
			delete(run.subs, ch)
			close(ch)
		}
	}
}

// finish removes the run from the registry and closes every remaining
// subscriber channel - the run's terminal agent_end frame was already
// broadcast, so a clean consumer sees agent_end followed by stream end.
// The cur == run guard makes a late finish a no-op when another run has
// already re-registered the same session id.
func (h *runHub) finish(run *activeRun) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if cur, ok := h.runs[run.id]; ok && cur == run {
		delete(h.runs, run.id)
	}
	for ch := range run.subs {
		close(ch)
	}
	run.subs = map[chan runFrame]struct{}{}
}

// launchRun runs the agent turn detached on runCtx (the request context
// WITHOUT cancellation) and broadcasts each frame to whoever is
// attached. This is the cron runner's detached-turn shape: the goroutine
// consumes the event channel itself, accumulates the same segmented
// assistant records and usage the inline path accumulated, and persists
// through completion whether or not any client is listening.
// runCtx is WithoutCancel(ctx): it keeps the request-scoped values (the
// logger seam) while outliving the HTTP request that started it.
//
// The run ctx carries a wall-clock timeout (s.runTimeout): a hung tool
// call or LLM call ends the run instead of holding the session's run
// slot forever. On timeout the loop unwinds through its normal ctx
// path (the tool result errors out, the next turn boundary emits a
// cancelled agent_end); an explicit error frame names the cap, and
// whatever accumulated still persists.
func (s *Server) launchRun(runCtx context.Context, run *activeRun, prov ai.Provider, messages []ai.Message, tools map[string]agent.Tool, req chatRequest) {
	go func() {
		defer s.runs.finish(run)
		ctx, cancel := context.WithTimeout(runCtx, s.runTimeout)
		defer cancel()
		var recs []assistantRecord
		var rec assistantRecord
		var usage streamUsage
		for event := range s.agent.RunWithProvider(ctx, prov, messages, tools) {
			s.accumulateAssistant(&recs, &rec, event, req)
			// Usage arrives on each stream_end; sum every turn of the run.
			if se, ok := event.(agent.StreamEvent); ok {
				if end, ok := se.Event.(ai.StreamEnd); ok {
					if end.PromptEvalCount != nil {
						usage.prompt += *end.PromptEvalCount
					}
					if end.EvalCount != nil {
						usage.completion += *end.EvalCount
					}
				}
			}
			eventType, data, err := sseEvent(event)
			if err != nil {
				s.runs.broadcast(run, runFrame{event: "error", data: []byte("event encode: " + err.Error())})
				s.persistRun(runCtx, run, prov, req, append(recs, rec), usage)
				return
			}
			s.runs.broadcast(run, runFrame{event: eventType, data: data})
		}
		if err := ctx.Err(); errors.Is(err, context.DeadlineExceeded) {
			s.runs.broadcast(run, runFrame{event: "error", data: []byte(
				"run timeout: the run exceeded its wall-clock cap (" + s.runTimeout.String() + ") and was stopped")})
		}
		s.persistRun(runCtx, run, prov, req, append(recs, rec), usage)
	}()
}

// persistRun persists the run's records and turns a persistence failure
// into a broadcast error frame instead of a write to one request's wire
// - nobody in particular is listening when persistence fails, and every
// attached wire should learn about the failure the same way.
func (s *Server) persistRun(ctx context.Context, run *activeRun, prov ai.Provider, req chatRequest, recs []assistantRecord, usage streamUsage) {
	if err := s.persistAssistant(ctx, prov, req, recs, usage); err != nil {
		s.runs.broadcast(run, runFrame{event: "error", data: []byte(err.Error())})
	}
}

// serveRunFrames writes the run's live frames to one SSE response until
// the run finishes (channel closed) or the client leaves (ctx done).
// Exiting here only detaches THIS client - the run keeps going for
// everyone else, and its persistence happens regardless. ch must already
// be subscribed BEFORE the first frame the caller intends to see.
func (s *Server) serveRunFrames(w http.ResponseWriter, flusher http.Flusher, run *activeRun, ch chan runFrame, ctx context.Context) {
	defer s.runs.unsubscribe(run, ch)
	for {
		select {
		case <-ctx.Done():
			return
		case f, ok := <-ch:
			if !ok {
				return
			}
			writeSSE(w, f.event, f.data)
			flusher.Flush()
		}
	}
}

// handleSessionEvents serves GET /sessions/{id}/events: an SSE
// subscription to a session's live run. 501 without a store. The lookup
// is the run registry - a session with no live run (never started, or
// already finished) is 404 "session not active" and the webui renders
// the settled history instead. Live-only for v1: a subscriber attaching
// mid-run receives frames from attach time on; what already happened is
// readable from GET /sessions/{id} history.
func (s *Server) handleSessionEvents(w http.ResponseWriter, r *http.Request) {
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
	run, ok := s.runs.get(id)
	if !ok {
		http.Error(w, "session not active", http.StatusNotFound)
		return
	}
	flusher, ok := setupSSE(w)
	if !ok {
		return
	}
	// Headers go out on attach, not on the run's first frame: the
	// subscriber's fetch() resolves while the run is still between
	// events, so an attach landing in the gap between two frames does
	// not hang on connect.
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	s.serveRunFrames(w, flusher, run, s.runs.subscribe(run), r.Context())
}
