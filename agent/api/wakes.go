package api

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// The wake path (async agent loop phase 1, card t_f3f953d0; design of
// record Workshop/omega/async-agent-loop.md). A dispatched yield-class
// call settles and its executor injects the synthetic core.YieldResult
// here; the home wakes on EACH arrival (ruling 7) through the same
// per-home serialization a user send takes - the run slot. When the
// slot is live (or another wake is draining), the result queues FIFO
// by arrival and launches when the slot frees (the launchRun finish
// hook drains). The hub also carries the runaway guard (ruling 12):
// cap consecutive synthetic-wake turns without a human input at the
// constant below, then park the home with a visible note; a user
// message resets the counter (cron wakes never ride this hub - the
// cron runner launches its own runs).
const maxConsecutiveWakes = 25

// wakeHub is the per-home FIFO of settled results plus the runaway
// guard. Sessions are sparse keys (homes with wake traffic only).
type wakeHub struct {
	mu sync.Mutex
	// drainMu serializes whole drain passes (see drainWakes).
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

// pushFront puts one result back as the next drain candidate - the
// launch lost the run-slot race and the live run's finish hook will
// drain it again. Nothing is lost, order preserved.
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

// reset clears the guard state - a user message (or the operator's
// hand) re-arms the home (ruling 12).
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

// parkedNow reports - and is idempotent - whether the home is already
// parked: a parked home refuses every further launch (a user reset
// re-arms it).
func (h *wakeHub) parkedNow(sessionID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.parked[sessionID]
}

// park sets the parked state; future wakes for the home are refused
// until a reset. Returns false when the home was already parked.
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

// sessionTypedLedger is the session-type-aware pending-calls table
// (card t_61d3def9, the session-type parity ruling): one
// agent.YieldLedger surface split by session namespace, so every
// session type gets the same async yield shape over its own backing.
// Durable sessions keep the store's table - their pending calls
// survive restarts and the boot recovery sweep re-enters them. The
// `eph-` lane is memory-backed beside the ephRegistry: an ephemeral
// session is process-local by law (nothing it owns may land in the
// store - the same nothing-on-disk wall the verdict hub holds, card
// t_860033e3), so RESTART EATS ITS PENDING RESULTS. That loss is the
// mode's own contract, not a new loss class: the whole session dies
// at restart anyway, and a pending result cannot outlive a home
// that no longer exists. No durability is added for the lane.
type sessionTypedLedger struct {
	// durable serves store-backed session ids (the store's table).
	durable agent.YieldLedger
	// ephemeral serves the `eph-` namespace (server memory only).
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

// Settle removes the entry from its lane (YieldLedger). Both lanes
// are cleared - the id lives in exactly one by construction, the
// other lane's settle is the contract's no-op, and boot-recovery
// residue (an eph row that predates the lane split) settles through
// the same call without the sweep guessing lanes.
func (l sessionTypedLedger) Settle(sessionID, callID string) error {
	if err := l.durable.Settle(sessionID, callID); err != nil {
		return err
	}
	return l.ephemeral.Settle(sessionID, callID)
}

// Pending lists the home's still-settling calls from its lane
// (YieldLedger).
func (l sessionTypedLedger) Pending(sessionID string) []agent.PendingCall {
	return l.lane(sessionID).Pending(sessionID)
}

// All lists every unsettled call across both lanes, durable lane
// first (YieldLedger). At serve start the eph lane is empty by
// construction - it is process-local - so the boot sweep reads
// durable rows plus any upgrade residue (see recoverPendingCalls).
func (l sessionTypedLedger) All() []agent.PendingCall {
	out := append([]agent.PendingCall(nil), l.durable.All()...)
	return append(out, l.ephemeral.All()...)
}

// wakeHome takes one settled yield result (called by the loop's
// dispatched executor through agent.SetYieldWake) and tries to drain
// the home.
func (s *Server) wakeHome(sessionID string, res core.YieldResult) {
	// The pending chip fills on the push channel (card t_7624bab1): every
	// settled out-of-band call passes here (immediate-yield dispatches and
	// budget escalations alike), so the chips topic's fill needs no second
	// hook. Published BEFORE the drain - the chip settles the moment the
	// result is in hand, not when the wake turn gets the slot.
	s.publishChipFill(sessionID, res)
	s.wakes.enqueue(sessionID, res)
	s.drainWakes(sessionID)
}

// drainWakes launches queued wake turns while the home's slot is free.
// Called on every result arrival (ruling 7: wake on EACH result) and
// from launchRun's finish hook (the slot just freed). Serialized per
// home by construction: a launch only happens through the run hub's
// one-run-per-session slot; a lost slot race re-queues at the front
// and the live run's finish hook pops it again. One drain at a time
// process-wide (the pop/launch/requeue window must not interleave with
// a finish-hook drain, or a re-queued result can sit unlaunched).
func (s *Server) drainWakes(sessionID string) {
	s.wakes.drainMu.Lock()
	defer s.wakes.drainMu.Unlock()
	for {
		res, ok := s.wakes.pop(sessionID)
		if !ok {
			return
		}
		if s.wakes.parkedNow(sessionID) {
			// The home is parked (guard fired earlier): the note is
			// already on the transcript; drop the result, no launch.
			continue
		}
		if !s.wakes.allow(sessionID) {
			// Runaway guard (ruling 12): park with a visible note,
			// settle the home's outstanding pending calls (a parked
			// home must not pin the compactor's never-fold forever)
			// and drop the queued results - they were counted, the
			// note says so.
			s.parkHome(sessionID)
			continue
		}
		launchCtx := context.WithoutCancel(s.bootCtx)
		if _, err := s.launchWake(sessionID, res, launchCtx); err != nil {
			if isEphemeralID(sessionID) && s.eph.get(sessionID) == nil {
				// The eph home closed or expired before the result
				// settled (card t_61d3def9). The re-entry contract
				// died with the home - restart-loss is the eph
				// mode's own law - so the result is DROPPED, never
				// re-queued: a launch against the gone registry
				// would fail identically on every retry and pin
				// the FIFO forever. The registry probe (not the
				// launch error) decides: a live home's transient
				// launch failure still re-queues below.
				s.wakes.rollback(sessionID)
				continue
			}
			// The slot was taken between pop and start (or the home
			// has no live backing): re-queue and wait for the next
			// finish hook. The tentative admit rolls back - the cap
			// counts launches, not arrivals.
			s.wakes.rollback(sessionID)
			s.wakes.pushFront(sessionID, res)
			return
		}
	}
}

// launchWake starts one wake run: the settled result re-enters through
// the home input path - the same prepareRun/launchRun pipe a user send
// takes (ruling 3: the loop never changes, any future writer of the
// shape is just another launcher). Returns ok=false when the run slot
// was taken - the result rides back into the queue.
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

// parkHome parks a runaway home (ruling 12): a visible note in the
// conversation, every outstanding pending call settled, queued results
// dropped.
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

// appendSessionBookkeeping persists a visible note on a home through
// whichever backing it has (store rows for durable sessions, the
// registry for ephemeral ones).
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

// recoverPendingCalls is the boot recovery sweep (ruling 5): at serve
// start every entry in the pending-calls table gets a synthetic timeout
// result injected through the wake path - the agent sees an interrupted
// result and may retry. Entries settle as they inject; the wake runs
// launch detached.
func (s *Server) recoverPendingCalls() {
	l := s.yieldLedger
	if l == nil {
		return
	}
	lgr := s.Logger()
	for _, pc := range l.All() {
		if isEphemeralID(pc.SessionID) {
			// Restart-loss contract (card t_61d3def9): the eph lane
			// is memory-backed, so a fresh boot holds none of its
			// entries - an `eph-` row that reached the durable
			// surface can only be upgrade residue from a build
			// before the lane split. Its home died with the restart
			// (the registry is gone), so the sweep settles the row
			// and never wakes it: a launch against the gone home
			// would re-queue forever.
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
