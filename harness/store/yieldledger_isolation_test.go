package store

// Lane B of the phantom-results probe (card t_a54ef2ce): the prod
// agent's skill accused the pending-result path of MIXING payloads -
// one arrival carrying rows from more than one result, or a settle
// hitting the wrong call. The probe disproved it (every wake result
// matched its own tool_call_id), so this test pins the key space that
// makes mixing impossible: a call id is only ever resolved inside its
// own session, and settling one session's call never touches another's
// row - even when both sessions use the same call id.

import (
	"testing"

	"github.com/EndoTheDev/omega/agent"
)

func TestYieldLedgerSessionIsolation(t *testing.T) {
	st := openLedgerStore(t)

	// The same call id in two homes - the classic cross-call mixing
	// vector on a table keyed by call id alone.
	shared := "call_same"
	if err := st.Register(agent.PendingCall{SessionID: "s1", CallID: shared, ToolName: "files.grep", Arguments: `{"n":1}`, DispatchedAt: "2026-11-09T13:00:00Z"}); err != nil {
		t.Fatalf("register s1: %v", err)
	}
	if err := st.Register(agent.PendingCall{SessionID: "s2", CallID: shared, ToolName: "shell.run", Arguments: `{"n":2}`, DispatchedAt: "2026-11-09T13:00:01Z"}); err != nil {
		t.Fatalf("register s2: %v", err)
	}

	if got := st.Pending("s1"); len(got) != 1 || got[0].ToolName != "files.grep" || got[0].Arguments != `{"n":1}` {
		t.Fatalf("pending s1 = %+v, want only s1's own call", got)
	}
	if got := st.Pending("s2"); len(got) != 1 || got[0].ToolName != "shell.run" || got[0].Arguments != `{"n":2}` {
		t.Fatalf("pending s2 = %+v, want only s2's own call", got)
	}

	// Settling s1's copy must leave s2's row intact - a settle that
	// matched on call id alone would erase the sibling's pending state
	// and its own arrival would be dropped as unknown.
	if err := st.Settle("s1", shared); err != nil {
		t.Fatalf("settle s1: %v", err)
	}
	if got := st.Pending("s1"); len(got) != 0 {
		t.Fatalf("pending s1 after settle = %+v, want empty", got)
	}
	if got := st.Pending("s2"); len(got) != 1 || got[0].CallID != shared {
		t.Fatalf("pending s2 after s1 settled = %+v, want its own row untouched", got)
	}

	// All() is the boot sweep's read: it must still see exactly the
	// surviving row, never a merged or duplicated entry.
	if all := st.All(); len(all) != 1 || all[0].SessionID != "s2" {
		t.Fatalf("all = %+v, want only s2's surviving row", all)
	}
}
