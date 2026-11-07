package agent

import (
	"context"
	"sync"

	"github.com/EndoTheDev/omega/core"
)

// The yield harness (async agent loop phase 1, card t_f3f953d0;
// design of record Workshop/omega/async-agent-loop.md). A yield-class
// tool call ENDS the turn: the loop registers the call in the durable
// pending-calls table, dispatches it on its own goroutine, and the turn
// (and run) end - nothing parks. When the call settles, the executor
// injects a core.YieldResult through the home's input path (the wake);
// it starts a new turn exactly like a user message. These are the
// CONSUMER's faces: the loop and the serving shell declare what they
// need, the implementers satisfy the shape structurally (the store's
// SQLite table; tests and store-less hosts the in-memory floor below).
// The interface lives here and nowhere in core - no capsule nouns in
// the substrate.

// PendingCall is one yield-class call awaiting its result, as the
// pending-calls table holds it.
type PendingCall struct {
	SessionID string
	CallID    string
	ToolName  string
	// Arguments is the raw JSON arguments payload - a record, the
	// re-entry never re-runs the call.
	Arguments string
	// DispatchedAt is the RFC 3339 wall-clock stamp.
	DispatchedAt string
}

// YieldLedger is the durable pending-calls table (ruling 5, the
// minimal restart story): the loop Registers a call at dispatch (the
// record is durable in the table BEFORE the result can arrive - the
// flush-before-result order) and Settles it when the result is
// injected; Pending feeds the wake turn's still-pending sibling
// placeholders and the never-fold law; All is the boot recovery sweep.
// Dispatched goroutines and turns never share sequencing, so every
// method must be safe for concurrent calls.
type YieldLedger interface {
	// Register records one dispatched call. A same-key (session,
	// call id) re-registration replaces.
	Register(pc PendingCall) error
	// Settle removes the entry - its result has been handed to the
	// wake path. Settling an unknown key is a no-op.
	Settle(sessionID, callID string) error
	// Pending lists the home's still-settling calls, dispatch order.
	Pending(sessionID string) []PendingCall
	// All lists every unsettled call, insertion order - the boot
	// recovery sweep reads it at serve start.
	All() []PendingCall
}

// MemYieldLedger is the in-memory floor: the table lives with the
// process, so boot recovery over it finds an empty table (nothing was
// ever durable) - the documented degradation for hosts whose store
// does not carry the pending-calls surface. Tests seed it by hand.
type MemYieldLedger struct {
	mu    sync.Mutex
	calls map[string]map[string]PendingCall // session -> call id -> call
	order []string                          // insertion keys "session\x00call", for All()
}

// NewMemYieldLedger returns an empty in-memory ledger.
func NewMemYieldLedger() *MemYieldLedger {
	return &MemYieldLedger{calls: map[string]map[string]PendingCall{}}
}

func ledgerKey(sessionID, callID string) string { return sessionID + "\x00" + callID }

// Register records one dispatched call (YieldLedger).
func (m *MemYieldLedger) Register(pc PendingCall) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.calls[pc.SessionID] == nil {
		m.calls[pc.SessionID] = map[string]PendingCall{}
	}
	key := ledgerKey(pc.SessionID, pc.CallID)
	if _, exists := m.calls[pc.SessionID][pc.CallID]; !exists {
		m.order = append(m.order, key)
	}
	m.calls[pc.SessionID][pc.CallID] = pc
	return nil
}

// Settle removes the entry (YieldLedger).
func (m *MemYieldLedger) Settle(sessionID, callID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if calls := m.calls[sessionID]; calls != nil {
		if _, ok := calls[callID]; ok {
			delete(calls, callID)
			key := ledgerKey(sessionID, callID)
			for i, o := range m.order {
				if o == key {
					m.order = append(m.order[:i], m.order[i+1:]...)
					break
				}
			}
		}
	}
	return nil
}

// Pending lists the home's still-settling calls in dispatch order
// (YieldLedger).
func (m *MemYieldLedger) Pending(sessionID string) []PendingCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []PendingCall
	for _, key := range m.order {
		sid, cid, ok := splitLedgerKey(key)
		if !ok || sid != sessionID {
			continue
		}
		if pc, ok := m.calls[sid][cid]; ok {
			out = append(out, pc)
		}
	}
	return out
}

// All lists every unsettled call in insertion order (YieldLedger).
func (m *MemYieldLedger) All() []PendingCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]PendingCall, 0, len(m.order))
	for _, key := range m.order {
		sid, cid, ok := splitLedgerKey(key)
		if !ok {
			continue
		}
		if pc, ok := m.calls[sid][cid]; ok {
			out = append(out, pc)
		}
	}
	return out
}

func splitLedgerKey(key string) (sessionID, callID string, ok bool) {
	for i := 0; i < len(key); i++ {
		if key[i] == 0 {
			return key[:i], key[i+1:], true
		}
	}
	return "", "", false
}

// yieldLedgerKey carries the run's YieldLedger so the downstream
// consumers (the compactor's never-fold law) read the live table off
// the run context. The loop threads it at Run start.
type yieldLedgerKey struct{}

// WithYieldLedger returns a child context carrying the pending-calls
// table for this run.
func WithYieldLedger(ctx context.Context, ledger YieldLedger) context.Context {
	if ledger == nil {
		return ctx
	}
	return context.WithValue(ctx, yieldLedgerKey{}, ledger)
}

// YieldLedgerFromContext returns the run's pending-calls table, or nil.
func YieldLedgerFromContext(ctx context.Context) YieldLedger {
	ledger, _ := ctx.Value(yieldLedgerKey{}).(YieldLedger)
	return ledger
}

// PendingAssistantIndex finds the message index of the EARLIEST
// assistant message in history that carries one of the still-pending
// calls - the fold boundary's skip-past anchor (ruling 11: a turn with
// pending calls is NEVER folded). -1 when no pending call's assistant
// message sits in the history (never expected: the flush-before-result
// order guarantees the call is durable in history before the ledger
// registration, but the caller still treats -1 as "nothing to
// protect").
func PendingAssistantIndex(messages []core.Message, pending []PendingCall) int {
	if len(pending) == 0 {
		return -1
	}
	pendingIDs := make(map[string]bool, len(pending))
	for _, pc := range pending {
		if pc.CallID != "" {
			pendingIDs[pc.CallID] = true
		}
	}
	for i, msg := range messages {
		a, ok := msg.(core.Assistant)
		if !ok {
			continue
		}
		for _, call := range a.ToolCalls {
			if pendingIDs[call.ID] {
				return i
			}
		}
	}
	return -1
}
