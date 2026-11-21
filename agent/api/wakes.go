package api

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// The wake path. The hub also carries the runaway guard: cap consecutive synthetic-wake
// turns without a human input at the constant below, then park the home with a visible
// note; a user message resets the counter (cron wakes never ride this hub - the cron
// runner launches its own runs).
const maxConsecutiveWakes = 25

// wakeHub is the per-home FIFO of settled results plus the runaway guard.
type wakeHub struct {
	mu sync.Mutex
	drainMu sync.Mutex
	byID    map[string][]core.YieldResult
	wakeSeq map[string]int  // consecutive wake launches without a human reset
	parked  map[string]bool // home parked (guard fired) until a human reset
}

func newWakeHub() *wakeHub {
	return &wakeHub{
		byID:    map[string][]core.YieldResult{},
		wakeSeq: map[string]int{},
		parked:  map[string]bool{},
	}
}

// enqueue takes one settled result into its home's FIFO ordering.
func (h *wakeHub) enqueue(sessionID string, res core.YieldResult) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.byID[sessionID] = append(h.byID[sessionID], res)
}

// pushFront puts one result back as the next drain candidate - the launch lost the
// run-slot race and the live run's finish hook will drain it again.
func (h *wakeHub) pushFront(sessionID string, res core.YieldResult) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.byID[sessionID] = append([]core.YieldResult{res}, h.byID[sessionID]...)
}

// pop takes the home's next result, FIFO.
func (h *wakeHub) pop(sessionID string) (core.YieldResult, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	q := h.byID[sessionID]
	if len(q) == 0 {
		return core.YieldResult{}, false
	}
	res := q[0]
	if len(q) == 1 {
		delete(h.byID, sessionID)
	} else {
		h.byID[sessionID] = q[1:]
	}
	return res, true
}

// dropQueued throws away every queued result for the home and reports how many it.
func (h *wakeHub) dropQueued(sessionID string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := len(h.byID[sessionID])
	delete(h.byID, sessionID)
	return n
}

// reset clears the guard state.
func (h *wakeHub) reset(sessionID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.wakeSeq, sessionID)
	delete(h.parked, sessionID)
}

// allow tentatively admits one more synthetic wake launch: it counts
// the home's consecutive wakes; false means the cap is reached - the
// caller parks (parkHome). On a failed launch (slot race) the caller
// ROLLS BACK - the cap counts launches, not arrivals.
func (h *wakeHub) allow(sessionID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.wakeSeq[sessionID]++
	return h.wakeSeq[sessionID] <= maxConsecutiveWakes
}

// rollback undoes one tentative admit after a failed launch.
func (h *wakeHub) rollback(sessionID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.wakeSeq[sessionID] > 0 {
		h.wakeSeq[sessionID]--
	}
}

// parkedNow reports - and is idempotent - whether the home is already parked.
func (h *wakeHub) parkedNow(sessionID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.parked[sessionID]
}

// park sets the parked state; future wakes for the home are refused until a reset.
func (h *wakeHub) park(sessionID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.parked[sessionID] {
		return false
	}
	h.parked[sessionID] = true
	delete(h.wakeSeq, sessionID)
	return true
}

// sessionTypedLedger is the session-type-aware pending-calls table. That loss is the
// mode's own contract, not a new loss class: the whole session dies at restart anyway,
// and a pending result cannot outlive a home that no longer exists.
type sessionTypedLedger struct {
	durable agent.YieldLedger
	ephemeral agent.YieldLedger
}

// lane picks the backing by session id namespace.
func (l sessionTypedLedger) lane(sessionID string) agent.YieldLedger {
	if isEphemeralID(sessionID) {
		return l.ephemeral
	}
	return l.durable
}

// Register records one dispatched call in its lane (YieldLedger).
func (l sessionTypedLedger) Register(pc agent.PendingCall) error {
	return l.lane(pc.SessionID).Register(pc)
}

// Settle removes the entry from its lane (YieldLedger). Both lanes are cleared - the id
// lives in exactly one by construction, the other lane's settle is the contract's
// no-op, and boot-recovery residue (an eph row that predates the lane split) settles
// through the same call without the sweep guessing lanes.
func (l sessionTypedLedger) Settle(sessionID, callID string) error {
	if err := l.durable.Settle(sessionID, callID); err != nil {
		return err
	}
	return l.ephemeral.Settle(sessionID, callID)
}

// Pending lists the home's still-settling calls from its lane (YieldLedger).
func (l sessionTypedLedger) Pending(sessionID string) []agent.PendingCall {
	return l.lane(sessionID).Pending(sessionID)
}

// All lists every unsettled call across both lanes, durable lane first (YieldLedger).
func (l sessionTypedLedger) All() []agent.PendingCall {
	out := append([]agent.PendingCall(nil), l.durable.All()...)
	return append(out, l.ephemeral.All()...)
}

// wakeHome takes one settled yield result.
func (s *Server) wakeHome(sessionID string, res core.YieldResult) {
	// A stopped home never re-enters.
	if s.runs.userStopped(sessionID) {
		if lgr := s.Logger(); lgr != nil {
			lgr.Printf("wake result for %s (%s) dropped: session %s was stopped by the user", res.ToolName, res.ToolCallID, sessionID)
		}
		return
	}
	s.publishChipFill(sessionID, res)
	s.wakes.enqueue(sessionID, res)
	s.drainWakes(sessionID)
}

// drainWakes launches queued wake turns while the home's slot is free. Serialized per
// home by construction: a launch only happens through the run hub's one-run-per-session
// slot; a lost slot race re-queues at the front and the live run's finish hook pops it
// again. One drain at a time process-wide (the pop/launch/requeue window must not
// interleave with a finish-hook drain, or a re-queued result can sit unlaunched).
func (s *Server) drainWakes(sessionID string) {
	s.wakes.drainMu.Lock()
	defer s.wakes.drainMu.Unlock()
	for {
		res, ok := s.wakes.pop(sessionID)
		if !ok {
			return
		}
		if s.runs.userStopped(sessionID) {
			// The user stopped this home: the work the queued results belong to is ended. Drop
			// them - a wake turn here would restart the conversation the human killed, which is
			// exactly the bug this drain gate exists.
			if n := s.wakes.dropQueued(sessionID); n > 0 {
				if lgr := s.Logger(); lgr != nil {
					lgr.Printf("session %s stopped by the user: dropped %d queued wake results", sessionID, n+1)
				}
			}
			return
		}
		if s.wakes.parkedNow(sessionID) {
			continue
		}
		if !s.wakes.allow(sessionID) {
			// Runaway guard: park with a visible note, settle the home's outstanding pending
			// calls (a parked home must not pin the compactor's never-compact forever) and drop
			// the queued results - they were counted, the note says.
			s.parkHome(sessionID)
			continue
		}
		launchCtx := context.WithoutCancel(s.bootCtx)
		if _, err := s.launchWake(sessionID, res, launchCtx); err != nil {
			if isEphemeralID(sessionID) && s.eph.get(sessionID) == nil {
				// The eph home closed or expired before the result settled. The re-entry contract
				// died with the home - restart-loss is the eph mode's own law - so the result is
				// DROPPED, never re-queued: a launch against the gone registry would fail
				// identically on every retry and pin the FIFO forever.
				s.wakes.rollback(sessionID)
				continue
			}
			// The slot was taken between pop and start (or the home has no live backing).
			s.wakes.rollback(sessionID)
			s.wakes.pushFront(sessionID, res)
			return
		}
	}
}

// wakeNoteSink wraps a wake run's TurnSink: a wake run must NEVER end with zero.
// Non-wake runs never get this wrapper, so their empty-skip is untouched.
type wakeNoteSink struct {
	s  *Server
	id string
	inner agent.TurnSink
	noted bool
	wrote bool
}

// wakeEmptyNoteText words the note.
func wakeEmptyNoteText(toolName string) string {
	if toolName == "" {
		return "[wake turn ended with no output - send a message to continue]"
	}
	return "[wake turn for " + toolName + " ended with no output - send a message to continue]"
}

// wakeToolName reads the settled result the run re-entered with.
func wakeToolName(ctx context.Context) string {
	for _, m := range agent.PersistInput(ctx) {
		if y, ok := m.(core.YieldResult); ok {
			return y.ToolName
		}
	}
	return ""
}

// AppendTurnMessage passes the message through, standing a visible note in for an
// all-empty wake completion.
func (w *wakeNoteSink) AppendTurnMessage(ctx context.Context, msg core.Message) error {
	if a, ok := msg.(core.Assistant); ok {
		if agent.IsEmptyAssistant(a) {
			if !w.noted {
				w.noted = true
				if w.stoppedByUser() {
					w.logSuppressedNote("empty")
				} else {
					name := wakeToolName(ctx)
					if lgr := w.s.Logger(); lgr != nil {
						lgr.Printf("wake turn for session %s (%s) ended with no output - note persisted", w.id, name)
					}
					w.s.appendSessionBookkeeping(w.id, core.NewUser(wakeEmptyNoteText(name)))
				}
			}
		} else {
			// An assistant row is the turn's own output. The run's input row rides this sink too
			// (a yield result), so the zero-trace test cannot be "any row" - only a produced
			// completion counts.
			w.wrote = true
		}
	}
	if w.inner == nil {
		return nil
	}
	return w.inner.AppendTurnMessage(ctx, msg)
}

// wakeStopNoteText words the note a wake run leaves when it stops before producing.
func wakeStopNoteText(reason, toolName string) string {
	what := "ended on a provider error"
	if reason == "cancelled" {
		what = "was stopped by the run cap"
	}
	if toolName == "" {
		return "[wake turn " + what + " - send a message to continue]"
	}
	return "[wake turn for " + toolName + " " + what + " - send a message to continue]"
}

// noteStoppedEnd persists the note a wake run leaves when it ends without producing.
// An "error" end (a provider stream error) returns from the loop BEFORE any completion
// is persisted, so the all-empty path above never sees a message.
func (w *wakeNoteSink) noteStoppedEnd(ctx context.Context, reason string) {
	if w.noted || w.wrote {
		return
	}
	w.noted = true
	if w.stoppedByUser() {
		w.logSuppressedNote(reason)
		return
	}
	name := wakeToolName(ctx)
	if lgr := w.s.Logger(); lgr != nil {
		lgr.Printf("wake turn for session %s (%s) stopped without a turn (%s) - note persisted", w.id, name, reason)
	}
	w.s.appendSessionBookkeeping(w.id, core.NewUser(wakeStopNoteText(reason, name)))
}

// stoppedByUser reports whether this wake run belongs to a home the user stopped.
func (w *wakeNoteSink) stoppedByUser() bool {
	return w.s != nil && w.s.runs.userStopped(w.id)
}

// logSuppressedNote records that a note was withheld, so the reason a stopped run.
func (w *wakeNoteSink) logSuppressedNote(why string) {
	if lgr := w.s.Logger(); lgr != nil {
		lgr.Printf("wake turn for session %s ended with no note (%s): the user stopped the run", w.id, why)
	}
}

// launchWake starts one wake run.
func (s *Server) launchWake(sessionID string, res core.YieldResult, ctx context.Context) (bool, error) {
	req := chatRequest{SessionID: sessionID, wake: true}
	req.Ephemeral = isEphemeralID(sessionID)
	cr, err := s.prepareRun(ctx, &req, []core.Message{res})
	if err != nil {
		return false, err
	}
	s.launchRun(ctx, cr.run, cr.prov, cr.messages, cr.tools, req)
	return true, nil
}

// parkHome parks a runaway home.
func (s *Server) parkHome(sessionID string) {
	if !s.wakes.park(sessionID) {
		return
	}
	lgr := s.Logger()
	if lgr != nil {
		lgr.Errorf("session %s parked: %d consecutive synthetic wakes without human input", sessionID, maxConsecutiveWakes)
	}
	if l := s.yieldLedger; l != nil {
		for _, pc := range l.Pending(sessionID) {
			if err := l.Settle(pc.SessionID, pc.CallID); err != nil && lgr != nil {
				lgr.Errorf("park settle %s (%s): %v", pc.ToolName, pc.CallID, err)
			}
		}
	}
	note := core.NewUser("[pending calls parked: " + fmt.Sprint(maxConsecutiveWakes) +
		" consecutive synthetic wakes without user input - send a message to resume]")
	s.appendSessionBookkeeping(sessionID, note)
}

// appendSessionBookkeeping persists a visible note on a home through whichever.
func (s *Server) appendSessionBookkeeping(sessionID string, msg core.Message) {
	lgr := s.Logger()
	if isEphemeralID(sessionID) {
		s.eph.appendUser(sessionID, []core.Message{msg}, time.Now())
		return
	}
	if s.store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.store.AppendMessage(ctx, sessionID, msg); err != nil && lgr != nil {
		lgr.Errorf("park note for %s: %v", sessionID, err)
	}
}

// abandonPending ends every piece of work the home had in flight at a user stop: each
// pending call is settled without a result (nothing re-enters a conversation the human
// ended), and the wake FIFO is emptied so a queued settlement cannot launch either.
// Settling is not cosmetic - the never-compact law and the pending placeholders both read
// this table, and a row left behind would pin the compactor's boundary.
func (s *Server) abandonPending(sessionID string) {
	lgr := s.Logger()
	if l := s.yieldLedger; l != nil {
		for _, pc := range l.Pending(sessionID) {
			if err := l.Settle(pc.SessionID, pc.CallID); err != nil && lgr != nil {
				lgr.Errorf("stop settle %s (%s): %v", pc.ToolName, pc.CallID, err)
			}
		}
	}
	if n := s.wakes.dropQueued(sessionID); n > 0 && lgr != nil {
		lgr.Printf("session %s stopped by the user: dropped %d queued wake results", sessionID, n)
	}
}

// recoverPendingCalls is the boot recovery sweep.
func (s *Server) recoverPendingCalls() {
	l := s.yieldLedger
	if l == nil {
		return
	}
	lgr := s.Logger()
	for _, pc := range l.All() {
		if isEphemeralID(pc.SessionID) {
			// Restart-loss contract: the eph lane is memory-backed, so a fresh boot holds none.
			// Its home died with the restart (the registry is gone), so the sweep settles the
			// row and never wakes it: a launch against the gone home would re-queue forever.
			if lgr != nil {
				lgr.Printf("boot recovery: pending call %s (%s) belongs to the ephemeral namespace - settled, dropped (restart ate its home)", pc.ToolName, pc.CallID)
			}
			if err := l.Settle(pc.SessionID, pc.CallID); err != nil && lgr != nil {
				lgr.Errorf("boot settle %s (%s): %v", pc.ToolName, pc.CallID, err)
			}
			continue
		}
		if lgr != nil {
			lgr.Printf("boot recovery: pending call %s (%s, session %s) injects a timeout result", pc.ToolName, pc.CallID, pc.SessionID)
		}
		res := core.YieldResult{
			ToolCallID: pc.CallID,
			ToolName:   pc.ToolName,
			Content:    "[interrupted: the tool result was lost to a restart - the call never reported; inspect its effects and retry if needed]",
			IsError:    true,
			Timeout:    true,
			Timestamp:  core.NowISO(),
		}
		if err := l.Settle(pc.SessionID, pc.CallID); err != nil && lgr != nil {
			lgr.Errorf("boot settle %s (%s): %v", pc.ToolName, pc.CallID, err)
		}
		s.wakeHome(pc.SessionID, res)
	}
}
