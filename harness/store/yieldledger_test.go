package store

// The pending-calls ledger tests (async agent loop phase 1, card
// t_f3f953d0): the store's YieldLedger face over the SQLite
// pending_calls table - durable enough that the boot recovery sweep
// (the api side's restart simulation) finds every entry.

import (
	"testing"

	"github.com/EndoTheDev/omega/agent"
)

func openLedgerStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// TestYieldLedgerShape: register, pending, all, settle - the face the
// loop, the placeholders and the boot sweep walk.
func TestYieldLedgerShape(t *testing.T) {
	st := openLedgerStore(t)
	rows := []agent.PendingCall{
		{SessionID: "s1", CallID: "c1", ToolName: "probe", Arguments: `{"msg":"1"}`, DispatchedAt: "2026-11-07T08:00:00Z"},
		{SessionID: "s1", CallID: "c2", ToolName: "probe", Arguments: `{"msg":"2"}`, DispatchedAt: "2026-11-07T08:00:01Z"},
		{SessionID: "s2", CallID: "c3", ToolName: "fetch", Arguments: ``, DispatchedAt: "2026-11-07T08:00:02Z"},
	}
	for _, pc := range rows {
		if err := st.Register(pc); err != nil {
			t.Fatalf("register %s: %v", pc.CallID, err)
		}
	}

	// Pending filters by session, in dispatch order.
	p1 := st.Pending("s1")
	if len(p1) != 2 || p1[0].CallID != "c1" || p1[1].CallID != "c2" {
		t.Fatalf("pending s1 = %+v", p1)
	}
	if p1[0].Arguments != `{"msg":"1"}` {
		t.Fatalf("arguments lost: %+v", p1[0])
	}

	// All carries both homes, insertion order.
	if all := st.All(); len(all) != 3 || all[2].CallID != "c3" || all[2].SessionID != "s2" {
		t.Fatalf("all = %+v", all)
	}

	// Settle is keyed: the settled call leaves, the sibling stays.
	if err := st.Settle("s1", "c1"); err != nil {
		t.Fatalf("settle: %v", err)
	}
	if p := st.Pending("s1"); len(p) != 1 || p[0].CallID != "c2" {
		t.Fatalf("pending after settle = %+v", p)
	}
	if err := st.Settle("s9", "nobody"); err != nil {
		t.Fatalf("settle unknown key must be a no-op, got %v", err)
	}
	if all := st.All(); len(all) != 2 {
		t.Fatalf("all after settle = %+v", all)
	}
}

// TestYieldLedgerRegisterReplaces: a same-key re-registration replaces
// the row (the idempotent retry) instead of duplicating it.
func TestYieldLedgerRegisterReplaces(t *testing.T) {
	st := openLedgerStore(t)
	first := agent.PendingCall{SessionID: "s1", CallID: "c1", ToolName: "probe", Arguments: `{}`, DispatchedAt: "2026-11-07T08:00:00Z"}
	if err := st.Register(first); err != nil {
		t.Fatal(err)
	}
	second := agent.PendingCall{SessionID: "s1", CallID: "c1", ToolName: "probe", Arguments: `{"msg":"2"}`, DispatchedAt: "2026-11-07T08:00:05Z"}
	if err := st.Register(second); err != nil {
		t.Fatal(err)
	}
	p := st.Pending("s1")
	if len(p) != 1 {
		t.Fatalf("re-registration duplicated the row: %+v", p)
	}
	if p[0].Arguments != `{"msg":"2"}` || p[0].DispatchedAt != second.DispatchedAt {
		t.Fatalf("re-registration did not replace: %+v", p[0])
	}
}
