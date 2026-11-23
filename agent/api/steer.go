package api

// The steer queue: input the human sends while a turn is live. Rows are SESSION-held, not
// run-held - a crashed or finished turn never eats them, and the next turn delivers them
// first - and the live run drains them at each batch boundary (agent.SteerFeed), so the
// agent adjusts in the SAME turn instead of a second run being launched underneath it.
//
// One boundary law: a settle result arriving mid-turn rides this same queue, so a live turn
// gets its own results at the boundary rather than starving them into a parallel wake.

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// Queue entry kinds.
const (
	queuedSteer = "steer"
	queuedWake  = "wake"
)

// queuedInput is one entry waiting for a turn. A steer carries the human's own messages
// (the skill preamble rides with them); a wake carries the settled result that answered a
// call the live turn left running.
type queuedInput struct {
	ID       string
	Kind     string
	At       string
	Messages []core.Message
}

// queuedRow is the wire shape of one visible queue row - the user messages only: a system
// preamble is machinery the row does not render.
type queuedRow struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	At      string `json:"at"`
}

// row renders the entry for the queue surface (wake entries are not user rows).
func (q queuedInput) row() queuedRow {
	var parts []string
	for _, m := range q.Messages {
		if u, ok := m.(core.User); ok && u.Content != "" {
			parts = append(parts, u.Content)
		}
	}
	return queuedRow{ID: q.ID, Content: strings.Join(parts, "\n"), At: q.At}
}

// steerHub is the per-session FIFO. inflight holds the rows a live turn has TAKEN but not
// yet acknowledged: a run that dies mid-delivery hands them back, so the crash eats no
// human words (rule 7).
type steerHub struct {
	mu       sync.Mutex
	byID     map[string][]queuedInput
	inflight map[string][]queuedInput
	seq      int
}

func newSteerHub() *steerHub {
	return &steerHub{byID: map[string][]queuedInput{}, inflight: map[string][]queuedInput{}}
}

// push appends one entry, returning it with its minted id.
func (h *steerHub) push(sessionID, kind string, messages []core.Message) queuedInput {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seq++
	item := queuedInput{
		ID:       "q" + strconv.Itoa(h.seq),
		Kind:     kind,
		At:       core.NowISO(),
		Messages: append([]core.Message(nil), messages...),
	}
	h.byID[sessionID] = append(h.byID[sessionID], item)
	return item
}

// list returns the session's waiting entries in delivery order.
func (h *steerHub) list(sessionID string) []queuedInput {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]queuedInput(nil), h.byID[sessionID]...)
}

// listSteers returns the session's waiting HUMAN rows in delivery order: the queue
// surface's view. A wake entry is machinery for the live turn it feeds - it is not a row
// the human reads, and it is not the surface's to delete.
func (h *steerHub) listSteers(sessionID string) []queuedInput {
	h.mu.Lock()
	defer h.mu.Unlock()
	q := h.byID[sessionID]
	out := make([]queuedInput, 0, len(q))
	for _, item := range q {
		if item.Kind == queuedSteer {
			out = append(out, item)
		}
	}
	return out
}

// remove drops one waiting row (delete-before-delivery). False = it was never there, it is
// already inflight and its delivery is no longer the surface's to cancel, or it is a wake
// entry the surface never showed.
func (h *steerHub) remove(sessionID, id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	q := h.byID[sessionID]
	for i, item := range q {
		if item.ID != id || item.Kind != queuedSteer {
			continue
		}
		h.byID[sessionID] = append(append([]queuedInput(nil), q[:i]...), q[i+1:]...)
		return true
	}
	return false
}

// take hands the session's waiting entries to the live turn, holding them as inflight until
// ack. A run that ends first returns them (returnInflight).
func (h *steerHub) take(sessionID string) []queuedInput {
	h.mu.Lock()
	defer h.mu.Unlock()
	q := h.byID[sessionID]
	if len(q) == 0 {
		return nil
	}
	delete(h.byID, sessionID)
	h.inflight[sessionID] = append(h.inflight[sessionID], q...)
	return append([]queuedInput(nil), q...)
}

// ack retires one delivered entry: the turn persisted it, so the row is spent.
func (h *steerHub) ack(sessionID, id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	q := h.inflight[sessionID]
	for i, item := range q {
		if item.ID != id {
			continue
		}
		h.inflight[sessionID] = append(append([]queuedInput(nil), q[:i]...), q[i+1:]...)
		if len(h.inflight[sessionID]) == 0 {
			delete(h.inflight, sessionID)
		}
		return
	}
}

// returnInflight puts an ended run's unacknowledged rows back at the FRONT of the queue:
// they arrived before anything queued since, and the next turn delivers them first.
func (h *steerHub) returnInflight(sessionID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	q := h.inflight[sessionID]
	if len(q) == 0 {
		return
	}
	delete(h.inflight, sessionID)
	h.byID[sessionID] = append(append([]queuedInput(nil), q...), h.byID[sessionID]...)
}

// requeue puts a batch back at the front. An id the queue already holds is not added
// twice: a failed launch is requeued by the machinery that took the batch AND by the
// prepare path that handed its rows back, and one row is still one delivery.
func (h *steerHub) requeue(sessionID string, batch []queuedInput) {
	h.mu.Lock()
	defer h.mu.Unlock()
	held := make(map[string]bool, len(h.byID[sessionID]))
	for _, item := range h.byID[sessionID] {
		held[item.ID] = true
	}
	lead := make([]queuedInput, 0, len(batch))
	for _, item := range batch {
		if held[item.ID] {
			continue
		}
		held[item.ID] = true
		lead = append(lead, item)
	}
	h.byID[sessionID] = append(lead, h.byID[sessionID]...)
}

// dropWakes removes every wake-kind entry for the session, waiting and inflight: a stop
// ends the work those results belong to, so they must not resurrect it. The human's own
// steer rows stay - a stop is not a delete.
func (h *steerHub) dropWakes(sessionID string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	dropped := 0
	keep := func(in []queuedInput) []queuedInput {
		out := make([]queuedInput, 0, len(in))
		for _, item := range in {
			if item.Kind == queuedWake {
				dropped++
				continue
			}
			out = append(out, item)
		}
		return out
	}
	if q, ok := h.byID[sessionID]; ok {
		if kept := keep(q); len(kept) > 0 {
			h.byID[sessionID] = kept
		} else {
			delete(h.byID, sessionID)
		}
	}
	if q, ok := h.inflight[sessionID]; ok {
		if kept := keep(q); len(kept) > 0 {
			h.inflight[sessionID] = kept
		} else {
			delete(h.inflight, sessionID)
		}
	}
	return dropped
}

// steerFeed is the run's boundary drain (agent.SteerFeed): everything waiting for this
// live turn, taken as one batch in queue order.
func (s *Server) steerFeed(sessionID string) agent.SteerFeed {
	return func() []agent.SteerDelivery {
		batch := s.steers.take(sessionID)
		if len(batch) == 0 {
			return nil
		}
		out := make([]agent.SteerDelivery, 0, len(batch))
		for _, item := range batch {
			for _, msg := range item.Messages {
				out = append(out, agent.SteerDelivery{ID: item.ID, Message: msg})
			}
		}
		return out
	}
}

// steerSend parks one /chat send for the session's live run and answers 202 with the rows.
func (s *Server) steerSend(w http.ResponseWriter, r *http.Request, req *chatRequest, messages []core.Message) {
	messages = s.expandAtMentions(messages, s.steerWorkspace(r.Context(), req))
	item := s.steers.push(req.SessionID, queuedSteer, messages)
	// The run may have ended between the active read and the push. The row is session-held
	// either way, so the kick is only what makes it a turn now instead of at the next send.
	if !s.runs.active(req.SessionID) {
		s.drainWakes(req.SessionID)
	}
	writeQueuedRows(w, http.StatusAccepted, []queuedInput{item})
}

// steerWorkspace resolves the folder @-mentions expand against for a mid-turn send: the
// session's pin, else the default workspace knob (the same root the run itself serves).
func (s *Server) steerWorkspace(ctx context.Context, req *chatRequest) string {
	if s.store != nil && req.SessionID != "" && !req.Ephemeral {
		if sess, err := s.store.GetSession(ctx, req.SessionID); err == nil && sess.Workspace != "" {
			return sess.Workspace
		}
	}
	s.wsMu.Lock()
	defer s.wsMu.Unlock()
	return s.defaultWorkspace
}

// writeQueuedRows answers the steer surfaces: the rows a send parked, in delivery order.
func writeQueuedRows(w http.ResponseWriter, status int, batch []queuedInput) {
	rows := make([]queuedRow, 0, len(batch))
	for _, item := range batch {
		rows = append(rows, item.row())
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"queued": rows})
}

// handleSessionQueue serves GET /sessions/{id}/queue: the human rows waiting for the
// session, rehydrated by any surface that reattaches mid-turn. A wake entry (a settled
// result feeding the live turn) is machinery, not a row - it never appears here.
func (s *Server) handleSessionQueue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing session id", http.StatusBadRequest)
		return
	}
	writeQueuedRows(w, http.StatusOK, s.steers.listSteers(id))
}

// handleSessionQueueRow serves DELETE /sessions/{id}/queue/{row}: drop one row before the
// boundary reaches it. An unknown (or already delivered) row is a 404 - the surface's row
// is stale, which is honest rather than a silent no-op. A wake entry draws the same 404:
// it was never a row the surface could show, and deleting it would eat a result the live
// turn is waiting for.
func (s *Server) handleSessionQueueRow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.PathValue("id")
	row := r.PathValue("row")
	if id == "" || row == "" {
		http.Error(w, "missing session or row id", http.StatusBadRequest)
		return
	}
	if !s.steers.remove(id, row) {
		http.Error(w, "queue row not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// launchQueued starts the turn that delivers a batch the live run never took: the rows are
// the run's INCOMING input (their normal turn), so ordering survives the death of the run
// that queued them. A batch carrying any human message is a human turn - it re-arms the
// stop latch and resets the runaway guard exactly like a fresh send; an all-wake batch
// keeps wake semantics.
func (s *Server) launchQueued(sessionID string, batch []queuedInput) bool {
	var messages []core.Message
	wake := true
	for _, item := range batch {
		messages = append(messages, item.Messages...)
		if item.Kind != queuedWake {
			wake = false
		}
	}
	if len(messages) == 0 {
		return true
	}
	req := chatRequest{SessionID: sessionID, wake: wake}
	req.Ephemeral = isEphemeralID(sessionID)
	ctx := context.WithoutCancel(s.bootCtx)
	cr, err := s.prepareRun(ctx, &req, messages)
	if err != nil {
		return false
	}
	// The batch IS this turn's input now: acknowledging retires the rows from the in-flight
	// set BEFORE the run exists, so its finish hook cannot re-queue them into a second
	// identical turn (and a fast run cannot win the race to hand them back). Rows the
	// prepare itself took ride cr.queued (parked while this launch was in flight).
	s.ackQueued(sessionID, batch)
	s.ackQueued(sessionID, cr.queued)
	s.launchRun(ctx, cr.run, cr.prov, cr.messages, cr.tools, req)
	return true
}

// steerNow reports whether the session's live run can take queued input (the queue's gate
// for a /chat send that would otherwise 409).
func (s *Server) steerNow(req *chatRequest) bool {
	if req.SessionID == "" || req.Retry || req.Fork != "" {
		return false
	}
	if s.store == nil && !req.Ephemeral {
		return false
	}
	return s.runs.active(req.SessionID)
}

// takeQueuedInto moves the session's queued rows into this turn's incoming messages and
// hands the batch back: the caller ACKS it once the launch is committed, so a prepare that
// dies first returns the rows instead of eating them (rule 7).
func (s *Server) takeQueuedInto(sessionID string) ([]core.Message, []queuedInput) {
	batch := s.steers.take(sessionID)
	if len(batch) == 0 {
		return nil, nil
	}
	var lead []core.Message
	for _, item := range batch {
		lead = append(lead, item.Messages...)
	}
	return lead, batch
}

// ackQueued retires rows a committed turn took: the turn is their delivery, so they are
// spent.
func (s *Server) ackQueued(sessionID string, batch []queuedInput) {
	for _, item := range batch {
		s.steers.ack(sessionID, item.ID)
	}
}

// prepareFailed releases a run slot whose prepare never reached a launch, and hands the
// rows the prepare had already taken off the queue back to it: the human's 202 is a
// receipt for DELIVERY, so a launch that dies before its turn runs eats nothing (rule 7).
func (s *Server) prepareFailed(run *activeRun, sessionID string) {
	s.steers.returnInflight(sessionID)
	s.runs.finish(run)
	s.publishRunState(sessionID, false)
}

// queuedWakeResult parks a settled result for the session's live turn.
func (s *Server) queuedWakeResult(sessionID string, res core.YieldResult) {
	s.steers.push(sessionID, queuedWake, []core.Message{res})
}
