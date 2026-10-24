package http_channel

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/ai"
	"github.com/EndoTheDev/omega/core"
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
	// cancel is the run's internal ctx cancel (the wall-clock cap's own
	// WithTimeout func), registered by launchRun the moment the run ctx
	// exists. Stop handlers cancel through it: the loop unwinds through
	// its normal path, exactly like the timeout does. nil only in the
	// start->launchRun window - a stop landing there is a 409.
	cancel context.CancelFunc
	// tail is the run's live transcript tail: the items rebuilt from the
	// accumulator after each event (liveTranscriptItems) that GET
	// /sessions/{id}/transcript merges behind the persisted history on a
	// reattach. Guarded by h.mu and stored by REPLACE (each rebuild mints
	// a fresh slice, never mutated in place), so a reader racing the next
	// rebuild sees either the old or the new complete slice.
	tail []transcriptItem
	// openTail is the run's OPEN segment only (the accumulator's
	// in-flight record, rebuilt after each event) - the chat panel's
	// composition (chat_panel.go): persisted history + the open
	// segment is duplicate-free, because every closed segment is
	// already persisted the moment it closes (the loop's TurnSink),
	// which a full-tail merge would double-render. Same h.mu, same
	// replace semantics.
	openTail []transcriptItem
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

// registerCancel stores the run's internal ctx cancel, making the run
// stoppable. The cur == run guard makes a late registration a no-op when
// another run has already re-registered the same session id.
func (h *runHub) registerCancel(run *activeRun, cancel context.CancelFunc) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if cur, ok := h.runs[run.id]; ok && cur == run {
		run.cancel = cancel
	}
}

// stop cancels the session's live run. ok is false when the session has
// no live run - the stop endpoint answers 404. cancellable is false when
// the run exists but has not registered its cancel yet (the sub-milli-
// second start->launchRun window) - the endpoint answers 409; the run
// unwinds on its own right after. Cancelling the run ctx is idempotent,
// so a repeated stop on the same winding-down run is a clean no-op 200.
func (h *runHub) stop(id string) (ok bool, cancellable bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	run, found := h.runs[id]
	if !found {
		return false, false
	}
	if run.cancel != nil {
		run.cancel()
	}
	return true, run.cancel != nil
}

// active reports whether the session has a live run.
func (h *runHub) active(id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.runs[id]
	return ok
}

// setTail stores the run's live transcript tail (the accumulator's
// snapshot after the current event). The cur == run guard makes a late
// setTail a no-op once another run re-registered the same session id.
// Called from the run's own goroutine; the build happened OUTSIDE the
// lock (liveTranscriptItems reads callStarts, which only that goroutine
// mutates).
func (h *runHub) setTail(run *activeRun, items []transcriptItem) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if cur, ok := h.runs[run.id]; ok && cur == run {
		run.tail = items
	}
}

// setOpenTail stores the run's open-segment items (the chat panel's
// composition source; runs.go comment on activeRun.openTail). The same
// cur == run guard.
func (h *runHub) setOpenTail(run *activeRun, items []transcriptItem) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if cur, ok := h.runs[run.id]; ok && cur == run {
		run.openTail = items
	}
}

// openItems returns the session's live run's open-segment items. ok is
// false when no run holds the session's slot.
func (h *runHub) openItems(id string) ([]transcriptItem, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	run, ok := h.runs[id]
	if !ok {
		return nil, false
	}
	return run.openTail, true
}

// firstActive returns one live session id - the chat panel's focus
// seed (chat_panel.go). Among concurrent runs the pick is
// lexicographically first for determinism; per-session concurrency is
// one run each, so realistically the map holds one.
func (h *runHub) firstActive() (string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	first := ""
	for id := range h.runs {
		if first == "" || id < first {
			first = id
		}
	}
	return first, first != ""
}

// tailItems returns the session's live run's transcript tail, for the
// transcript endpoint's reattach merge. ok is false when no run holds
// the session's slot - the endpoint then serves persisted history only.
func (h *runHub) tailItems(id string) ([]transcriptItem, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	run, ok := h.runs[id]
	if !ok {
		return nil, false
	}
	return run.tail, true
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

// broadcastAll delivers one frame to every live run's subscribers -
// the canvas delta fan-out (server_canvas.go). Panel state is global,
// not per-session, so a delta rides every open stream: a webui tabs'
// chat stream and its reattach stream are the same frame pipes, and
// the frontend dispatches on the frame type. Same full-queue law as
// broadcast: a stalled subscriber is cut, never allowed to stall a
// run's loop for everyone.
func (h *runHub) broadcastAll(f runFrame) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, run := range h.runs {
		for ch := range run.subs {
			select {
			case ch <- f:
			default:
				delete(run.subs, ch)
				close(ch)
			}
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
func (s *Server) launchRun(runCtx context.Context, run *activeRun, prov ai.Provider, messages []ai.Message, tools map[string]core.Tool, req chatRequest) {
	// Chat panel (chat-last 1/3): NewServer always wires the capsule;
	// no nil guard needed here.
	s.chat.beginRun(req.SessionID)
	ctx, cancel := context.WithTimeout(runCtx, s.runTimeout)
	// Register the internal cancel BEFORE the goroutine starts: a stop
	// request arriving during launch must find the run cancellable, not
	// stuck in the 409 "not started" window.
	s.runs.registerCancel(run, cancel)
	go func() {
		defer s.runs.finish(run)
		// The chat panel's exit hook runs BEFORE the hub releases the
		// slot: the ephemeral final rows still read the live tail.
		defer s.chat.endRun(req.SessionID)
		defer cancel()
		// detached runs carry the session id too: task tools in the detached
		// loop link to the same conversation the request started
		ctx = agent.WithSessionID(ctx, req.SessionID)
		// Verdict cards v1 (card t_2ecf9c51): a webui /chat is an
		// interactive turn - verdict.request may issue from here.
		ctx = core.WithRunOrigin(ctx, core.RunOriginInteractive)
		// Turn copy persistence belongs to the loop (card t_471f6d4a),
		// same wiring as the inline path.
		ctx = agent.WithTurnSink(ctx, s.turnSinkFor(req))
		ctx = agent.WithPersistInput(ctx, req.persistInput)
		// Ephemeral (card t_d96a5cd5): the loop filters the denied name
		// AFTER every provider merge, so a tool an extension re-adds
		// under the same name never runs in a no-persist session.
		if req.Ephemeral {
			ctx = agent.WithDeniedTools(ctx, "memory")
		}
		var recs []assistantRecord
		var rec assistantRecord
		var usage streamUsage
		meter := newRunMeter()
		for event := range s.agent.RunWithProvider(ctx, prov, messages, tools) {
			s.accumulateAssistant(&recs, &rec, event, req)
			// Chat panel hook (chat-last 1/3): one call per event -
			// token chunks batch, structural events flush.
			s.chat.runEvent(req.SessionID, event)
			meter.tick(event)
			// Live-tail snapshot (card t_692254f7): the accumulator's
			// state right after this event becomes the transcript
			// endpoint's reattach merge - a refresh mid-run restores
			// the open thinking block, the running tool call (elapsed
			// ticking from the call's own start) and the segments not
			// persisted yet. Built on this goroutine after accumulate;
			// the hub only swaps the slice. The open segment rides the
			// same rebuild into the hub (runs.go openTail) - the chat
			// panel composes persisted history + open.
			s.runs.setTail(run, liveTranscriptItems(recs, rec, time.Now()))
			s.runs.setOpenTail(run, liveTranscriptItems(nil, rec, time.Now()))
			// Usage arrives on each stream_end; sum every turn of the run.
			if se, ok := event.(core.StreamEvent); ok {
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
				s.persistRun(runCtx, run, prov, req, append(recs, rec), usage, meter.stats(usage))
				return
			}
			s.runs.broadcast(run, runFrame{event: eventType, data: data})
		}
		if err := ctx.Err(); errors.Is(err, context.DeadlineExceeded) {
			s.runs.broadcast(run, runFrame{event: "error", data: []byte(
				"run timeout: the run exceeded its wall-clock cap (" + s.runTimeout.String() + ") and was stopped")})
		}
		s.persistRun(runCtx, run, prov, req, append(recs, rec), usage, meter.stats(usage))
	}()
}

// persistRun persists the run's records and turns a persistence failure
// into a broadcast error frame instead of a write to one request's wire
// - nobody in particular is listening when persistence fails, and every
// attached wire should learn about the failure the same way.
func (s *Server) persistRun(ctx context.Context, run *activeRun, prov ai.Provider, req chatRequest, recs []assistantRecord, usage streamUsage, last *core.LastRunStats) {
	if err := s.persistAssistant(ctx, prov, req, recs, usage, last); err != nil {
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
	id := r.PathValue("id")
	// Ephemeral reattach: the run hub is the truth, no store needed (an
	// eph run without a store still broadcasts). Non-eph ids keep the
	// store gate - store-less servers have no session transcript.
	if s.store == nil && !isEphemeralID(id) {
		http.Error(w, "session store not configured", http.StatusNotImplemented)
		return
	}
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
