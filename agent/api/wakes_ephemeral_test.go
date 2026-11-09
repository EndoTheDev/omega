package api

// Ephemeral yield-lane tests (card t_61d3def9, session-type parity):
// normal, ephemeral and agent sessions share one async yield shape.
// The `eph-` lane rides the same dispatch + wake machinery over a
// MEMORY-backed pending-calls table (sessionTypedLedger) - an
// ephemeral session's pending call never lands in the store, restart
// eats its results (the mode's own contract, pinned here as
// boot-recovery behavior), and a home that closed mid-call drops the
// settled result instead of re-queueing it forever.

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/agent/loop"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/store"
)

// ephSpyStore wraps the real store and records every pending-call
// Register that names an `eph-` session - the routing law's witness.
// A row that never reaches the store cannot be asserted off the
// table (Settle would have deleted it), so the CALL is the pin.
type ephSpyStore struct {
	*store.Store
	mu          sync.Mutex
	ephRegister []agent.PendingCall
}

func (s *ephSpyStore) Register(pc agent.PendingCall) error {
	if isEphemeralID(pc.SessionID) {
		s.mu.Lock()
		s.ephRegister = append(s.ephRegister, pc)
		s.mu.Unlock()
	}
	return s.Store.Register(pc)
}

// TestEphemeralYieldSliceEndToEnd: the session-type parity pin. One
// ephemeral /chat whose completion calls a yield-class tool: the turn
// ends yielded (loop-back degradation is gone for eph runs), the
// settled result re-enters through the wake path as a second run
// bound to the eph session, the result lands in the registry's
// history AFTER the assistant that carries the call
// (flush-before-result), and the DURABLE store never receives the
// pending call - the eph lane is memory-backed.
func TestEphemeralYieldSliceEndToEnd(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	spy := &ephSpyStore{Store: st}
	prov := newWakeScripted("step", [][]core.ProviderStreamEvent{
		// Run 1: one yield-class call - the turn ends yielded.
		{
			core.ToolCallEvent{Type: "tool_call", ToolCall: core.ToolCall{
				ID: "c1", Name: "probe", Arguments: map[string]any{"msg": "hi"},
			}},
			core.StreamEnd{Type: "stream_end", FinishReason: "tool_call"},
		},
		// Run 2 (the wake): answer and close.
		finalStep("done"),
	})
	tools := map[string]core.Tool{"probe": {
		Description: "probe (yield-class)",
		Yield:       true,
		Run: func(_ context.Context, args map[string]any) (string, error) {
			msg, _ := args["msg"].(string)
			return "async:" + msg, nil
		},
	}}
	a := agent.NewAgent(prov, nil, 0)
	a.SetLoopProvider(loop.Loop{})
	s := NewServer(a, tools, spy, nil, nil, nil)

	id, events := ephemeralChat(t, s, "", "hello ephemeral", nil)
	yielded := false
	for _, f := range events {
		if f.event == "agent_end" {
			var end struct {
				FinishReason string `json:"finish_reason"`
			}
			if err := json.Unmarshal(f.data, &end); err != nil {
				t.Fatalf("decode agent_end: %v", err)
			}
			if end.FinishReason != "yielded" {
				t.Errorf("agent_end finish_reason = %q, want yielded (no loop-back degrade for eph runs)", end.FinishReason)
			}
			yielded = true
		}
	}
	if !yielded {
		t.Fatal("no agent_end frame in the send's stream")
	}

	// The wake run lands the synthetic result in the eph registry.
	deadline := time.Now().Add(5 * time.Second)
	seen := false
	for time.Now().Before(deadline) && !seen {
		seen = hasYieldResult(s.eph.history(id), "c1", core.YieldResult{Content: "async:hi"})
		time.Sleep(10 * time.Millisecond)
	}
	if !seen {
		t.Fatalf("the wake run never landed the probe result in the eph registry (id %s)", id)
	}
	// Flush-before-result in the registry: the assistant carrying the
	// call precedes the result row.
	callIdx, resultIdx := -1, -1
	for i, m := range s.eph.history(id) {
		if as, ok := m.(core.Assistant); ok {
			for _, c := range as.ToolCalls {
				if c.ID == "c1" {
					callIdx = i
				}
			}
		}
		if y, ok := m.(core.YieldResult); ok && y.ToolCallID == "c1" {
			resultIdx = i
		}
	}
	if callIdx == -1 || resultIdx == -1 || callIdx > resultIdx {
		t.Errorf("order broken: assistant(call) index %d, yield_result index %d", callIdx, resultIdx)
	}

	// The routing law: no pending call for the eph home ever reached
	// the durable table, and the memory lane settled clean.
	spy.mu.Lock()
	leaks := append([]agent.PendingCall(nil), spy.ephRegister...)
	spy.mu.Unlock()
	if len(leaks) != 0 {
		for _, pc := range leaks {
			t.Errorf("eph pending call reached the durable store: %+v", pc)
		}
	}
	if left := s.ephLedger.All(); len(left) != 0 {
		t.Errorf("eph ledger holds unsettled rows: %+v", left)
	}
	if left := st.All(); len(left) != 0 {
		t.Errorf("durable ledger holds unsettled rows: %+v", left)
	}
	// The slice ended clean: no live run, no park.
	waitFor(t, "wake run finished", func() bool { return !s.runs.active(id) })
	if s.wakes.parkedNow(id) {
		t.Error("a 1-wake eph slice parked the home")
	}
}

// TestSessionTypedLedgerRoutesByNamespace: the composite table's
// Register/Pending/Settle route by session id namespace - the eph
// entry lives only in the memory lane, the durable entry only in the
// store lane, All() carries both, and each lane settles its own.
func TestSessionTypedLedgerRoutesByNamespace(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	eph := agent.NewMemYieldLedger()
	l := sessionTypedLedger{durable: st, ephemeral: eph}

	if err := l.Register(agent.PendingCall{SessionID: "eph-x1", CallID: "ce", ToolName: "probe", DispatchedAt: core.NowISO()}); err != nil {
		t.Fatalf("register eph: %v", err)
	}
	if err := l.Register(agent.PendingCall{SessionID: "s1", CallID: "cd", ToolName: "probe", DispatchedAt: core.NowISO()}); err != nil {
		t.Fatalf("register durable: %v", err)
	}

	if got := st.Pending("eph-x1"); len(got) != 0 {
		t.Errorf("eph entry in the durable lane: %+v", got)
	}
	if got := eph.Pending("s1"); len(got) != 0 {
		t.Errorf("durable entry in the eph lane: %+v", got)
	}
	if got := l.Pending("eph-x1"); len(got) != 1 || got[0].CallID != "ce" {
		t.Errorf("Pending(eph) = %+v, want the one eph call", got)
	}
	if got := l.Pending("s1"); len(got) != 1 || got[0].CallID != "cd" {
		t.Errorf("Pending(durable) = %+v, want the one durable call", got)
	}
	if all := l.All(); len(all) != 2 {
		t.Errorf("All() = %+v, want both lanes' entries", all)
	}

	// Settle removes from the right lane only.
	if err := l.Settle("eph-x1", "ce"); err != nil {
		t.Fatalf("settle eph: %v", err)
	}
	if got := l.Pending("eph-x1"); len(got) != 0 {
		t.Errorf("eph entry survived its settle: %+v", got)
	}
	if got := l.Pending("s1"); len(got) != 1 {
		t.Errorf("durable entry eaten by the eph settle: %+v", got)
	}
}

// TestBootRecoveryDropsEphemeralResidue: the restart-loss contract.
// The eph lane is memory-backed, so a boot never carries its entries;
// the one durable shape that can still name an `eph-` session is
// upgrade residue from a pre-split build. The sweep settles that row
// and never wakes it - its home died with the restart - while the
// durable sibling row gets its injected timeout wake as always.
func TestBootRecoveryDropsEphemeralResidue(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.CreateSession(context.Background(), "s1", "", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := st.Register(agent.PendingCall{SessionID: "eph-residue", CallID: "cE", ToolName: "probe", Arguments: `{}`, DispatchedAt: core.NowISO()}); err != nil {
		t.Fatalf("seed eph residue: %v", err)
	}
	if err := st.Register(agent.PendingCall{SessionID: "s1", CallID: "cD", ToolName: "probe", Arguments: `{}`, DispatchedAt: core.NowISO()}); err != nil {
		t.Fatalf("seed durable row: %v", err)
	}

	scripts := [][]core.ProviderStreamEvent{finalStep("recovering")}
	prov := newWakeScripted("step", scripts)
	a := agent.NewAgent(prov, nil, 0)
	a.SetLoopProvider(loop.Loop{})
	s := NewServer(a, nil, st, nil, nil, nil)

	// The sweep settles every row - the residue included.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && len(s.yieldLedger.All()) != 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if left := s.yieldLedger.All(); len(left) != 0 {
		t.Fatalf("boot sweep left entries: %+v", left)
	}
	// The durable home woke (the existing recovery law).
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && !s.runs.active("s1") {
		time.Sleep(10 * time.Millisecond)
	}
	if !s.runs.active("s1") {
		t.Error("boot recovery never launched the durable wake run")
	}
	waitFor(t, "durable wake finished", func() bool { return !s.runs.active("s1") })

	// The residue never woke: no run ever took its id, and - the pin
	// the pre-split ghost could not hold - nothing sits re-queued in
	// its FIFO waiting for a home that will never exist.
	s.wakes.mu.Lock()
	ghost := s.wakes.byID["eph-residue"]
	s.wakes.mu.Unlock()
	if len(ghost) != 0 {
		t.Errorf("eph residue re-queued for a dead home: %+v", ghost)
	}
	if s.eph.get("eph-residue") != nil {
		t.Error("boot sweep resurrected an eph home")
	}
}

// TestWakeDroppedWhenEphemeralHomeGone: a result settling after its
// eph home closed (or the process restarted around it) is dropped at
// the launch gate - the re-entry contract died with the home. The
// queue must not hold it forever; a parked-style note is pointless
// (there is no transcript to pin it to).
func TestWakeDroppedWhenEphemeralHomeGone(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	prov := newWakeScripted("step", [][]core.ProviderStreamEvent{finalStep("never")})
	a := agent.NewAgent(prov, nil, 0)
	a.SetLoopProvider(loop.Loop{})
	s := NewServer(a, nil, st, nil, nil, nil)

	// The registry holds no eph-gone record (closed or restarted).
	s.wakeHome("eph-gone", core.YieldResult{ToolCallID: "c1", ToolName: "probe", Content: "late"})

	s.wakes.mu.Lock()
	left := s.wakes.byID["eph-gone"]
	s.wakes.mu.Unlock()
	if len(left) != 0 {
		t.Errorf("settled result for a gone eph home re-queued: %+v", left)
	}
	if s.runs.active("eph-gone") {
		t.Error("a wake run launched against a gone eph home")
	}
}

// TestEphemeralLedgerKeepsPendingThroughSettle: the eph lane's rows
// are readable while the call is still outstanding (the compactor's
// never-fold law and the wake turn's sibling placeholders read the
// same table), and settle-then-inject leaves it empty.
func TestEphemeralLedgerKeepsPendingThroughSettle(t *testing.T) {
	s, _ := newTestServerWithStore(t)
	pc := agent.PendingCall{SessionID: "eph-live", CallID: "c1", ToolName: "probe", DispatchedAt: core.NowISO()}
	if err := s.yieldLedger.Register(pc); err != nil {
		t.Fatalf("register: %v", err)
	}
	if got := s.yieldLedger.Pending("eph-live"); len(got) != 1 || got[0].CallID != "c1" {
		t.Fatalf("eph lane Pending = %+v, want the registered call", got)
	}
	if err := s.yieldLedger.Settle("eph-live", "c1"); err != nil {
		t.Fatalf("settle: %v", err)
	}
	if got := s.yieldLedger.Pending("eph-live"); len(got) != 0 {
		t.Errorf("eph lane holds a settled row: %+v", got)
	}
}
