package verdict

// Ephemeral-session persistence leak (card t_860033e3, finding 3): a
// card issued in an `eph-` session must keep BOTH its rows (the issued
// card and the verdict) out of the store - the eph- lane lives in
// memory only, and a row under an `eph-` id would be a durable
// transcript no store read serves and no delete ever cleans.

import (
	"testing"

	"github.com/EndoTheDev/omega/agent"
)

func TestEphemeralSessionNeverPersisted(t *testing.T) {
	st := newFakeStore()
	h := NewHub(st, nil)
	card := agent.VerdictPrompt{ID: "c1", SessionID: "eph-abc123", Title: "ok?", Options: vo("yes")}
	ch := askAsync(t, h, card)
	waitForPending(t, h, "eph-abc123", 1)
	if _, err := h.Answer("eph-abc123", "c1", agent.Verdict{OptionIndex: 1}); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	r := drain(t, ch)
	if r.err != nil {
		t.Fatalf("Ask: %v", r.err)
	}
	if recs := st.snapshot(); len(recs) != 0 {
		t.Fatalf("store rows for an ephemeral session = %d, want 0 (nothing-on-disk law)", len(recs))
	}
}