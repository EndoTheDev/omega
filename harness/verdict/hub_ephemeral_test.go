package verdict

// Ephemeral-session persistence leak: a card issued in an `eph-` session must keep its
// records off disk.

import (
	"testing"

	"github.com/EndoTheDev/omega/core"
)

func TestEphemeralSessionNeverPersisted(t *testing.T) {
	st := newFakeStore()
	h := NewHub(st, nil)
	card := core.QuestionPrompt{ID: "c1", SessionID: "eph-abc123", Title: "ok?", Options: vo("yes")}
	ch := askAsync(t, h, card)
	waitForPending(t, h, "eph-abc123", 1)
	if _, err := h.Answer("eph-abc123", "c1", core.QuestionAnswer{OptionIndex: 1}); err != nil {
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
