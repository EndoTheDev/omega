package api

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// runFrame is one prepared SSE frame: the event type plus its JSON data.
type runFrame struct {
	event string
	data  []byte
}

// subscriberBuffer bounds one subscriber's frame queue.
const subscriberBuffer = 256

// activeRun is one detached /chat run.
type activeRun struct {
	id   string
	subs map[chan runFrame]struct{}
	// cancel is the run's internal ctx cancel (the wall-clock cap's own WithTimeout. Stop
	// handlers cancel through it: the loop unwinds through its normal path, exactly like
	// the timeout does. nil only in the start->launchRun window - a stop landing there is
	// a 409.
	cancel context.CancelFunc
	// tail is the run's live transcript tail. Guarded by h.mu and stored by REPLACE (each
	// rebuild mints a fresh slice, never mutated in place), so a reader racing the next
	// rebuild sees either the old or the new complete slice.
	tail []transcriptItem
	tailAt time.Time
	// openTail is the run's OPEN segment only (the accumulator's in-flight record, rebuilt
	// after each event) - the chat panel's composition (chat_panel.go): persisted history
	// + the open segment is duplicate-free, because every closed segment is already
	// persisted the moment it closes (the loop's TurnSink), which a full-tail merge would
	// double-render.
	openTail []transcriptItem
	openTailAt time.Time
	// branch pins the run head's parent row id with the session's max store row id at
	// run start. The chat panel's composition reads it to cut the old sibling's
	// persisted rows so the fresh response streams into their place - never stacked.
	branch string
	floor  int64
}

// RunHub tracks detached runs by session id.
type RunHub struct {
	mu   sync.Mutex
	runs map[string]*activeRun
	// stops holds each session's user-stop latch. It OUTLIVES the run it belongs to: a
	// stop must keep holding after the run's slot frees, because the work the run left in
	// motion (dispatched yield calls, their shells) settles later and must not wake the
	// home. A human send re-arms with a fresh latch (rearm); a wake launch never does - a
	// cancelled home stays cancelled until a human speaks again.
	stops map[string]*core.RunStop
}

// start registers a new run for the session.
func (h *RunHub) start(id string) (*activeRun, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.runs == nil {
		h.runs = map[string]*activeRun{}
	}
	if _, exists := h.runs[id]; exists {
		return nil, false
	}
	if h.stops == nil {
		h.stops = map[string]*core.RunStop{}
	}
	if h.stops[id] == nil {
		h.stops[id] = core.NewRunStop()
	}
	run := &activeRun{id: id, subs: map[chan runFrame]struct{}{}}
	h.runs[id] = run
	return run, true
}

// stopSignal returns the session's user-stop latch.
func (h *RunHub) stopSignal(id string) *core.RunStop {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.stops[id]
}

// rearm re-arms the session's stop latch after a human send. An ARMED latch is kept as
// is (the same object, not a fresh one): dispatched calls left running by earlier turns
// of this session registered their cancellation on it, and swapping the object would
// silently orphan them - the very bug this latch exists to fix.
func (h *RunHub) rearm(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stops == nil {
		h.stops = map[string]*core.RunStop{}
	}
	if sig := h.stops[id]; sig != nil && !sig.Stopped() {
		return
	}
	h.stops[id] = core.NewRunStop()
}

// userStopped reports whether the session's work was stopped by the user and no human.
func (h *RunHub) userStopped(id string) bool {
	return h.stopSignal(id).Stopped()
}

// registerCancel stores the run's internal ctx cancel, making the run stoppable.
func (h *RunHub) registerCancel(run *activeRun, cancel context.CancelFunc) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if cur, ok := h.runs[run.id]; ok && cur == run {
		run.cancel = cancel
	}
}

// stop cancels the session's live run. Cancelling the run ctx is idempotent, so a
// repeated stop on the same winding-down run is a clean no-op 200.
func (h *RunHub) stop(id string) (ok bool, cancellable bool) {
	h.mu.Lock()
	run, found := h.runs[id]
	if !found {
		h.mu.Unlock()
		return false, false
	}
	sig := h.stops[id]
	cancel := run.cancel
	h.mu.Unlock()
	if cancel == nil {
		return true, false
	}
	sig.Stop()
	cancel()
	return true, true
}

// active reports whether the session has a live run.
func (h *RunHub) active(id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.runs[id]
	return ok
}

// setTail stores the run's live transcript tail.
// Called from the run's own goroutine; the build happened OUTSIDE the lock
// (liveTranscriptItems reads callStarts, which only that goroutine mutates).
func (h *RunHub) setTail(run *activeRun, items []transcriptItem, builtAt time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if cur, ok := h.runs[run.id]; ok && cur == run {
		run.tail = items
		run.tailAt = builtAt
	}
}

// setOpenTail stores the run's open-segment items.
func (h *RunHub) setOpenTail(run *activeRun, items []transcriptItem, builtAt time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if cur, ok := h.runs[run.id]; ok && cur == run {
		run.openTail = items
		run.openTailAt = builtAt
	}
}

// openSnapshot returns the session's live run's open-segment items and the instant.
func (h *RunHub) openSnapshot(id string) ([]transcriptItem, time.Time, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	run, ok := h.runs[id]
	if !ok {
		return nil, time.Time{}, false
	}
	return run.openTail, run.openTailAt, true
}

// setBranch pins the run's branch-parent row and the pre-run row-id floor.
func (h *RunHub) setBranch(run *activeRun, branch string, floor int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if cur, ok := h.runs[run.id]; ok && cur == run {
		run.branch = branch
		run.floor = floor
	}
}

// runBranch reports the session's live run branch pin.
func (h *RunHub) runBranch(id string) (branch string, floor int64, pinned bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	run, ok := h.runs[id]
	if !ok || run.branch == "" {
		return "", 0, false
	}
	return run.branch, run.floor, true
}

// firstActive returns one live session id. Among concurrent runs the pick is
// lexicographically first for determinism; per-session concurrency is one run each, so
// realistically the map holds one.
func (h *RunHub) firstActive() (string, bool) {
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

// tailSnapshot returns the session's live run's transcript tail and the instant it.
func (h *RunHub) tailSnapshot(id string) ([]transcriptItem, time.Time, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	run, ok := h.runs[id]
	if !ok {
		return nil, time.Time{}, false
	}
	return run.tail, run.tailAt, true
}

// get returns the session's live run, for subscribing.
func (h *RunHub) get(id string) (*activeRun, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	run, ok := h.runs[id]
	return run, ok
}

// subscribe attaches one consumer channel to the run.
func (h *RunHub) subscribe(run *activeRun) chan runFrame {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch := make(chan runFrame, subscriberBuffer)
	run.subs[ch] = struct{}{}
	return ch
}

// unsubscribe detaches one consumer: removed and closed. A channel no
// longer in subs was already cut (full queue) or finished (hub close),
// so its close already happened elsewhere - this must not double-close.
func (h *RunHub) unsubscribe(run *activeRun, ch chan runFrame) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := run.subs[ch]; !ok {
		return
	}
	delete(run.subs, ch)
	close(ch)
}

// broadcast delivers one frame to every subscriber. A subscriber whose queue is full is
// CUT, never allowed to stall the run for everyone: the closed channel ends that SSE
// stream without agent_end, which the webui renders as an interrupted stream - the
// honest state for a wire that stopped moving.
func (h *RunHub) broadcast(run *activeRun, f runFrame) {
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

// broadcastAll delivers one frame to every live run's subscribers. Same full-queue law
// as broadcast: a stalled subscriber is cut, never allowed to stall a run's loop for
// everyone.
func (h *RunHub) broadcastAll(f runFrame) {
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

// finish removes the run from the registry and closes every remaining subscriber.
func (h *RunHub) finish(run *activeRun) {
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

// launchRun runs the agent turn detached on runCtx (the request context WITHOUT. This
// is the cron runner's detached-turn shape: the goroutine consumes the event channel
// itself, accumulates the same segmented assistant records and usage the inline path
// accumulated, and persists through completion whether or not any client is listening.
func (s *Server) launchRun(runCtx context.Context, run *activeRun, prov agent.Provider, messages []core.Message, tools map[string]core.Tool, req chatRequest) {
	// Branch pin: BEFORE beginRun's first delta - the opening push must already compose.
	// Plain sends pin nothing and compose exactly.
	s.runs.setBranch(run, req.branchParent, req.branchFloor)
	// Chat panel (chat-last 1/3): NewServer always wires the capsule; no nil guard.
	s.chat.beginRun(req.SessionID)
	ctx, cancel := context.WithTimeout(runCtx, s.runTimeout)
	// Register the internal cancel BEFORE the goroutine starts: a stop
	// request arriving during launch must find the run cancellable, not
	// stuck in the 409 "not started" window.
	s.runs.registerCancel(run, cancel)
	go func() {
		// Wake drain (async agent loop, agent/api/wakes.go): declared FIRST so it runs LAST -
		// the drain must see the slot the hub finish already released, and this entry (a
		// plain send) is what resets the runaway guard.
		defer s.drainWakes(req.SessionID)
		// The runs topic's release edge rides the slot release: declared BEFORE the finish
		// defer, so it publishes after the slot frees - a client that refetches on the edge
		// does not race a still-occupied slot.
		defer s.publishRunState(req.SessionID, false)
		defer s.runs.finish(run)
		// Chat panel's exit hook runs BEFORE the hub releases the slot.
		defer s.chat.endRun(req.SessionID)
		defer cancel()
		if !req.wake {
			s.wakes.reset(req.SessionID)
		}
		ctx = agent.WithSessionID(ctx, req.SessionID)
		ctx = core.WithRunStop(ctx, s.runs.stopSignal(req.SessionID))
		// Session workspace: prepareRun resolved req.workspace.
		// Without this wrap the tools of every session-backed chat ran at the process cwd
		// (the service lane: C:\Windows\System32) and the prompt context the trust resolver
		// built was silently dropped.
		ctx = core.WithRunWorkspace(ctx, req.workspace, req.workspacePromptContext)
		ctx = core.WithRunOrigin(ctx, "interactive")
		ctx = core.WithRunOutputTap(ctx, s.chipTap(req.SessionID))
		sink := s.turnSinkFor(req)
		ctx = agent.WithTurnSink(ctx, sink)
		ctx = agent.WithPersistInput(ctx, req.persistInput)
		// Ephemeral: the loop filters the denied names AFTER every provider merge, so a tool
		// an extension re-adds under the same name never runs in a no-persist session.
		if req.Ephemeral {
			ctx = agent.WithDeniedTools(ctx, "memory", "memory.add", "memory.replace", "memory.remove", "memory.batch")
		}
		var recs []assistantRecord
		var rec assistantRecord
		var usage streamUsage
		// stoppedReason records the loop's terminal end that writes no completion. Both
		// return from the loop before any row persists, so the wake note below stands in for
		// the turn that never wrote.
		var stoppedReason string
		meter := newRunMeter()
		runner := s.runAgent(&req)
		for event := range runner.RunWithProvider(ctx, prov, messages, tools) {
			s.accumulateAssistant(&recs, &rec, event, req)
			s.chat.runEvent(req.SessionID, event)
			meter.tick(event)
			// Live-tail snapshot: the accumulator's state right after this event becomes.
			// Built on this goroutine after accumulate; the hub only swaps the slice.
			now := time.Now()
			s.runs.setTail(run, liveTranscriptItems(recs, rec, now), now)
			s.runs.setOpenTail(run, liveTranscriptItems(nil, rec, now), now)
			// Usage arrives on each stream_end; sum every turn of the run.
			if se, ok := event.(core.StreamEvent); ok {
				if end, ok := se.Event.(core.StreamEnd); ok {
					if end.PromptEvalCount != nil {
						usage.prompt += *end.PromptEvalCount
					}
					if end.EvalCount != nil {
						usage.completion += *end.EvalCount
					}
				}
			}
			// A terminal stop-without-completion persists nothing itself. Both leave the wake
			// note below to stand in for the row the turn never wrote.
			if end, ok := event.(core.AgentEnd); ok {
				if end.FinishReason == "error" || end.FinishReason == "cancelled" {
					stoppedReason = end.FinishReason
				}
			}
			eventType, data, err := sseEvent(event)
			if err != nil {
				s.emitRunFrame(run, runFrame{event: "error", data: []byte("event encode: " + err.Error())})
				s.persistRun(runCtx, run, prov, req, append(recs, rec), usage, meter.stats(usage))
				return
			}
			s.emitRunFrame(run, runFrame{event: eventType, data: data})
		}
		if err := ctx.Err(); errors.Is(err, context.DeadlineExceeded) {
			s.emitRunFrame(run, runFrame{event: "error", data: []byte(
				"run timeout: the run exceeded its wall-clock cap (" + s.runTimeout.String() + ") and was stopped")})
		}
		// Wake runs only: a wake turn that died on a provider error - or on the run cap
		// expiring between turns - leaves a visible note, so the re-entry turn never vanishes
		// from the session on the next reload.
		if stoppedReason != "" && req.wake {
			if ws, ok := sink.(*wakeNoteSink); ok {
				ws.noteStoppedEnd(ctx, stoppedReason)
			}
		}
		s.persistRun(runCtx, run, prov, req, append(recs, rec), usage, meter.stats(usage))
	}()
}

// persistRun persists the run's records and turns a persistence failure.
func (s *Server) persistRun(ctx context.Context, run *activeRun, prov agent.Provider, req chatRequest, recs []assistantRecord, usage streamUsage, last *core.LastRunStats) {
	if err := s.persistAssistant(ctx, prov, req, recs, usage, last); err != nil {
		s.emitRunFrame(run, runFrame{event: "error", data: []byte(err.Error())})
	}
}

// serveRunFrames writes the run's live frames to one SSE response until the run. ch
// must already be subscribed BEFORE the first frame the caller intends to see.
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

// handleSessionEvents serves GET /sessions/{id}/events. The lookup is the run registry
// - a session with no live run (never started, or already finished) is 404 "session not
// active" and the webui renders the settled history instead.
func (s *Server) handleSessionEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.PathValue("id")
	// Ephemeral reattach: the run hub is the truth, no store needed.
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
	// Subscribe BEFORE the client-visible 200: the moment the headers flush, the
	// subscriber's channel must be registered in run.subs - otherwise a frame broadcast in
	// the header-to-subscribe gap (a canvas delta, or the fast client firing its next
	// request on the resolved fetch) lands on a subscriber list that does not exist yet
	// and is dropped.
	ch := s.runs.subscribe(run)
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	s.serveRunFrames(w, flusher, run, ch, r.Context())
}
