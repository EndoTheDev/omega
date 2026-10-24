package verdict

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/core"
)

// fakeStore captures AppendMessage calls so a test can assert the
// transcript audit trail (the card record before the pause, the verdict
// record after the answer / expiry). Implements core.StoreProvider by
// embedding the interface for the methods the hub never calls (it only
// calls AppendMessage) and overriding that one.
type fakeStore struct {
	core.StoreProvider
	mu       sync.Mutex
	records  []recorded
	appendOK bool
}

type recorded struct {
	session string
	role    string
	payload []byte
}

func newFakeStore() *fakeStore { return &fakeStore{appendOK: true} }

func (s *fakeStore) AppendMessage(ctx context.Context, sessionID string, msg core.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.appendOK {
		return errors.New("store down")
	}
	role, payload, err := core.EncodeMessage(msg)
	if err != nil {
		return err
	}
	s.records = append(s.records, recorded{session: sessionID, role: role, payload: payload})
	return nil
}

func (s *fakeStore) snapshot() []recorded {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]recorded, len(s.records))
	copy(out, s.records)
	return out
}

func (s *fakeStore) roles() []string {
	recs := s.snapshot()
	out := make([]string, len(recs))
	for i, r := range recs {
		out[i] = r.role
	}
	return out
}

// vo builds pick rows from labels: the test shorthand for
// []core.VerdictOption (label only, no description).
func vo(labels ...string) []core.VerdictOption {
	out := make([]core.VerdictOption, len(labels))
	for i, l := range labels {
		out[i] = core.VerdictOption{Label: l}
	}
	return out
}

// askAsync runs Ask in a goroutine (it blocks) so the test can answer,
// expire, or cancel it and then collect the returned verdict / error.
// The caller synchronizes registration with waitForPending before
// asserting on Pending or answering - registration happens inside the
// goroutine, not before it.
func askAsync(t *testing.T, h *Hub, card core.VerdictPrompt) <-chan askResult {
	t.Helper()
	ch := make(chan askResult, 1)
	go func() {
		v, err := h.Ask(context.Background(), card)
		ch <- askResult{v: v, err: err}
	}()
	return ch
}

type askResult struct {
	v   core.Verdict
	err error
}

// waitForPending polls until at least `n` cards are pending for the
// session ("" counts every session) and returns the pending list.
func waitForPending(t *testing.T, h *Hub, session string, n int) []core.VerdictPrompt {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if cards := h.Pending(session); len(cards) >= n {
			return cards
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("fewer than %d pending cards in %q within 1s", n, session)
	return nil
}

// drain waits for an Ask result with a deadline so a stuck block fails
// the test fast instead of hanging the whole suite.
func drain(t *testing.T, ch <-chan askResult) askResult {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(2 * time.Second):
		t.Fatal("Ask did not return within 2s")
		return askResult{}
	}
}

// drainWithTimeout waits up to `d` for an Ask result (used by the
// expiry tests where the block legitimately outlasts the default 2s).
func drainWithTimeout(t *testing.T, ch <-chan askResult, d time.Duration) askResult {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(d):
		t.Fatalf("Ask did not return within %s", d)
		return askResult{}
	}
}

func TestAskBlocksUntilAnswered(t *testing.T) {
	h := NewHub(nil, nil)
	card := core.VerdictPrompt{SessionID: "s1", Title: "ok?", Options: vo("yes", "no")}
	ch := askAsync(t, h, card)

	cards := waitForPending(t, h, "s1", 1)
	if cards[0].Title != "ok?" {
		t.Fatalf("Pending = %+v, want one 'ok?' card", cards)
	}
	if _, err := h.Answer("s1", "", core.Verdict{OptionIndex: 1}); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	r := drain(t, ch)
	if r.err != nil {
		t.Fatalf("Ask returned error: %v", r.err)
	}
	if r.v.OptionIndex != 1 || r.v.Option != "yes" {
		t.Fatalf("Ask verdict = %+v, want option 1 'yes'", r.v)
	}
	if got := h.Pending("s1"); len(got) != 0 {
		t.Fatalf("Pending after answer = %d, want 0", len(got))
	}
}

func TestAnswerResolvesOldestPendingWhenCardIDEmpty(t *testing.T) {
	h := NewHub(nil, nil)
	ch := askAsync(t, h, core.VerdictPrompt{ID: "cX", SessionID: "s1", Title: "q", Options: vo("a", "b")})
	waitForPending(t, h, "s1", 1)
	if _, err := h.Answer("s1", "", core.Verdict{OptionIndex: 2}); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	r := drain(t, ch)
	if r.v.OptionIndex != 2 || r.v.Option != "b" || r.v.CardID != "cX" {
		t.Fatalf("verdict = %+v, want option 2 'b' card cX", r.v)
	}
}

func TestAnswerWrongSessionRejected(t *testing.T) {
	h := NewHub(nil, nil)
	_ = askAsync(t, h, core.VerdictPrompt{ID: "c1", SessionID: "s1", Title: "q", Options: vo("y")})
	waitForPending(t, h, "s1", 1)
	if _, err := h.Answer("s2", "c1", core.Verdict{OptionIndex: 1}); err == nil {
		t.Fatal("Answer from a different session must error")
	}
	// resolve the ask so the goroutine does not outlive the test
	_, _ = h.Answer("s1", "c1", core.Verdict{OptionIndex: 1})
}

func TestAnswerValidations(t *testing.T) {
	h := NewHub(nil, nil)
	ch := askAsync(t, h, core.VerdictPrompt{ID: "c1", SessionID: "s1", Title: "pick", Options: vo("a", "b")})
	waitForPending(t, h, "s1", 1)
	t.Cleanup(func() {
		// a failed assertion leaves the card pending; skipping resolves it
		_, _ = h.Answer("s1", "c1", core.Verdict{Skipped: true})
		<-ch
	})

	cases := []struct {
		name string
		v    core.Verdict
		want string
	}{
		{"out of range", core.Verdict{OptionIndex: 3}, "out of range"},
		{"free text not allowed", core.Verdict{Text: "hi"}, "not free text"},
		{"empty answer", core.Verdict{}, "needs an option pick"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := h.Answer("s1", "c1", tc.v)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Answer %+v err = %v, want containing %q", tc.v, err, tc.want)
			}
			// a rejected answer leaves the card pending for the next case
			if h.Pending("s1")[0].ID != "c1" {
				t.Fatalf("card lost after rejected answer")
			}
		})
	}
}

func TestFreeTextOnlyCard(t *testing.T) {
	h := NewHub(nil, nil)
	ch := askAsync(t, h, core.VerdictPrompt{ID: "c1", SessionID: "s1", Title: "name?", AllowText: true})
	waitForPending(t, h, "s1", 1)
	// a pick on a free-text-only card is rejected: no options to index.
	if _, err := h.Answer("s1", "c1", core.Verdict{OptionIndex: 1}); err == nil {
		t.Fatal("option pick on a no-options card must error")
	}
	if _, err := h.Answer("s1", "c1", core.Verdict{Text: "omega"}); err != nil {
		t.Fatalf("Answer text: %v", err)
	}
	r := drain(t, ch)
	if r.v.Text != "omega" {
		t.Fatalf("verdict = %+v, want text 'omega'", r.v)
	}
}

func TestMultiAnswerStoresEveryIndex(t *testing.T) {
	st := newFakeStore()
	h := NewHub(st, nil)
	card := core.VerdictPrompt{
		ID: "cm1", SessionID: "s1", Title: "which modules?",
		Options: []core.VerdictOption{{Label: "a", Description: "first"}, {Label: "b"}, {Label: "c"}},
		Multi:   true,
	}
	ch := askAsync(t, h, card)
	waitForPending(t, h, "s1", 1)
	ans, err := h.Answer("s1", "cm1", core.Verdict{OptionIndexes: []int{1, 3}})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if len(ans.OptionIndexes) != 2 || ans.OptionIndexes[0] != 1 || ans.OptionIndexes[1] != 3 {
		t.Fatalf("answer indexes = %v, want [1 3]", ans.OptionIndexes)
	}
	if len(ans.Options) != 2 || ans.Options[0] != "a" || ans.Options[1] != "c" {
		t.Fatalf("answer labels = %v, want [a c]", ans.Options)
	}
	r := drain(t, ch)
	if r.err != nil {
		t.Fatalf("Ask error: %v", r.err)
	}
	if len(r.v.OptionIndexes) != 2 || r.v.Kind() != "answer" {
		t.Fatalf("verdict = %+v, want the multi pick", r.v)
	}
	// exactly one record, and it carries every selected index + label
	roles := st.roles()
	if len(roles) != 2 || roles[1] != "verdict" {
		t.Fatalf("roles = %v, want [verdict_card verdict]", roles)
	}
	var rec core.VerdictRecord
	if err := json.Unmarshal(st.snapshot()[1].payload, &rec); err != nil {
		t.Fatalf("decode verdict row: %v", err)
	}
	if len(rec.OptionIndexes) != 2 || rec.OptionIndexes[0] != 1 || rec.OptionIndexes[1] != 3 {
		t.Fatalf("recorded indexes = %v, want [1 3]", rec.OptionIndexes)
	}
	if len(rec.Options) != 2 || rec.Options[0] != "a" || rec.Options[1] != "c" {
		t.Fatalf("recorded labels = %v, want [a c]", rec.Options)
	}
}

func TestMultiAnswerSingleIndex(t *testing.T) {
	h := NewHub(nil, nil)
	ch := askAsync(t, h, core.VerdictPrompt{ID: "cm2", SessionID: "s1", Title: "q", Options: vo("a", "b"), Multi: true})
	waitForPending(t, h, "s1", 1)
	ans, err := h.Answer("s1", "cm2", core.Verdict{OptionIndexes: []int{2}})
	if err != nil {
		t.Fatalf("Answer single-index multi: %v", err)
	}
	if ans.OptionIndex != 2 || ans.Option != "b" || len(ans.OptionIndexes) != 1 {
		t.Fatalf("answer = %+v, want the one-row multi pick", ans)
	}
	_ = drain(t, ch)
}

func TestMultiShapeMismatchesRejected(t *testing.T) {
	h := NewHub(nil, nil)
	mcard := core.VerdictPrompt{ID: "cm3", SessionID: "s1", Title: "multi", Options: vo("a", "b"), Multi: true}
	ch := askAsync(t, h, mcard)
	waitForPending(t, h, "s1", 1)
	t.Cleanup(func() { _, _ = h.Answer("s1", "cm3", core.Verdict{Skipped: true}); <-ch })
	if _, err := h.Answer("s1", "cm3", core.Verdict{OptionIndex: 1}); err == nil || !strings.Contains(err.Error(), "multi-select") {
		t.Fatalf("single pick on multi card err = %v, want the multi-select complaint", err)
	}
	if _, err := h.Answer("s1", "cm3", core.Verdict{OptionIndexes: []int{4}}); err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("out-of-range multi pick err = %v, want out of range", err)
	}
}

func TestMultiAnswerOnSingleCardRejected(t *testing.T) {
	h := NewHub(nil, nil)
	ch := askAsync(t, h, core.VerdictPrompt{ID: "cs1", SessionID: "s1", Title: "single", Options: vo("a", "b")})
	waitForPending(t, h, "s1", 1)
	t.Cleanup(func() { _, _ = h.Answer("s1", "cs1", core.Verdict{Skipped: true}); <-ch })
	if _, err := h.Answer("s1", "cs1", core.Verdict{OptionIndexes: []int{1, 2}}); err == nil || !strings.Contains(err.Error(), "single pick") {
		t.Fatalf("multi answer on single card err = %v, want the single-pick complaint", err)
	}
}

func TestAllowTextOnOptionsCard(t *testing.T) {
	h := NewHub(nil, nil)
	ch := askAsync(t, h, core.VerdictPrompt{ID: "c1", SessionID: "s1", Title: "add?", Options: vo("yes", "no"), AllowText: true})
	waitForPending(t, h, "s1", 1)
	if _, err := h.Answer("s1", "c1", core.Verdict{Text: "yes, but later"}); err != nil {
		t.Fatalf("typed answer on allow_text card: %v", err)
	}
	r := drain(t, ch)
	if r.v.Text != "yes, but later" {
		t.Fatalf("verdict = %+v, want the typed text", r.v)
	}
}

func TestSkipRecorded(t *testing.T) {
	h := NewHub(nil, nil)
	ch := askAsync(t, h, core.VerdictPrompt{ID: "c1", SessionID: "s1", Title: "q", Options: vo("y")})
	waitForPending(t, h, "s1", 1)
	r2, err := h.Answer("s1", "c1", core.Verdict{Skipped: true})
	if err != nil {
		t.Fatalf("Answer skip: %v", err)
	}
	if !r2.Skipped || r2.Kind() != "skip" {
		t.Fatalf("Answer returned %+v, want skipped verdict", r2)
	}
	r := drain(t, ch)
	if !r.v.Skipped || r.v.Kind() != "skip" {
		t.Fatalf("verdict = %+v, want skipped", r.v)
	}
}

func TestSecondAnswerRejected(t *testing.T) {
	h := NewHub(nil, nil)
	ch := askAsync(t, h, core.VerdictPrompt{ID: "c1", SessionID: "s1", Title: "q", Options: vo("y")})
	waitForPending(t, h, "s1", 1)
	_, _ = h.Answer("s1", "c1", core.Verdict{OptionIndex: 1})
	_ = drain(t, ch)
	_, err := h.Answer("s1", "c1", core.Verdict{OptionIndex: 1})
	if err == nil || !strings.Contains(err.Error(), "already answered") {
		t.Fatalf("second Answer err = %v, want 'already answered'", err)
	}
}

func TestExpiryFiresSafeAnswer(t *testing.T) {
	h := NewHub(nil, nil)
	card := core.VerdictPrompt{ID: "c1", SessionID: "s1", Title: "q", Options: vo("yes", "no"), TimeoutS: 1}
	ch := askAsync(t, h, card)
	waitForPending(t, h, "s1", 1)
	// wait past the 1s deadline; the timer fires the expired verdict.
	r := drainWithTimeout(t, ch, 3*time.Second)
	if r.err != nil {
		t.Fatalf("Ask error: %v", r.err)
	}
	if !r.v.Expired || r.v.Kind() != "expired" {
		t.Fatalf("verdict = %+v, want expired", r.v)
	}
	// a late answer is rejected as already expired, never double-applied.
	_, err := h.Answer("s1", "c1", core.Verdict{OptionIndex: 1})
	if err == nil || !strings.Contains(err.Error(), "already expired") {
		t.Fatalf("late Answer err = %v, want 'already expired'", err)
	}
}

func TestCtxCancellationSettlesCard(t *testing.T) {
	h := NewHub(nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := h.Ask(ctx, core.VerdictPrompt{ID: "c1", SessionID: "s1", Title: "q", Options: vo("y")})
		done <- err
	}()
	waitForPending(t, h, "s1", 1)
	cancel()
	if err := <-done; err == nil {
		t.Fatal("Ask with cancelled ctx must error")
	}
	// the card is settled; a later answer is rejected (not delivered).
	_, err := h.Answer("s1", "c1", core.Verdict{OptionIndex: 1})
	if err == nil || !strings.Contains(err.Error(), "already answered") {
		t.Fatalf("Answer after cancel err = %v, want a settled rejection", err)
	}
}

func TestCloseUnblocksPendingAsk(t *testing.T) {
	h := NewHub(nil, nil)
	ch := askAsync(t, h, core.VerdictPrompt{ID: "c1", SessionID: "s1", Title: "q", Options: vo("y")})
	waitForPending(t, h, "s1", 1)
	h.Close()
	r := drain(t, ch)
	if r.err == nil {
		t.Fatalf("Close must unblock Ask with an error, got verdict %+v", r.v)
	}
}

func TestPersistenceRecordsCardAndVerdict(t *testing.T) {
	st := newFakeStore()
	h := NewHub(st, nil)
	card := core.VerdictPrompt{ID: "c1", SessionID: "s9", Title: "ok?", Options: vo("yes", "no")}
	ch := askAsync(t, h, card)
	waitForPending(t, h, "s9", 1)
	_, _ = h.Answer("s9", "c1", core.Verdict{OptionIndex: 1})
	_ = drain(t, ch)

	roles := st.roles()
	if len(roles) != 2 || roles[0] != "verdict_card" || roles[1] != "verdict" {
		t.Fatalf("transcript roles = %v, want [verdict_card verdict]", roles)
	}
	// both rows land in the same session.
	recs := st.snapshot()
	if recs[0].session != "s9" || recs[1].session != "s9" {
		t.Fatalf("transcript sessions = %v, want both s9", []string{recs[0].session, recs[1].session})
	}
	var v core.VerdictRecord
	if err := json.Unmarshal(recs[1].payload, &v); err != nil {
		t.Fatalf("decode verdict row: %v", err)
	}
	if v.OptionIndex != 1 || v.Option != "yes" {
		t.Fatalf("persisted verdict = %+v, want option 1 'yes'", v)
	}
}

func TestExpiryPersisted(t *testing.T) {
	st := newFakeStore()
	h := NewHub(st, nil)
	ch := askAsync(t, h, core.VerdictPrompt{ID: "c1", SessionID: "s1", Title: "q", Options: vo("y"), TimeoutS: 1})
	waitForPending(t, h, "s1", 1)
	_ = drainWithTimeout(t, ch, 3*time.Second)
	roles := st.roles()
	if len(roles) != 2 || roles[1] != "verdict" {
		t.Fatalf("transcript roles = %v, want [verdict_card verdict]", roles)
	}
	recs := st.snapshot()
	var v core.VerdictRecord
	_ = json.Unmarshal(recs[1].payload, &v)
	if !v.Expired {
		t.Fatalf("persisted verdict row not expired: %+v", v)
	}
}

func TestPersistenceFailureDoesNotBlockQuestion(t *testing.T) {
	st := newFakeStore()
	st.appendOK = false
	h := NewHub(st, nil) // nil logger keeps the failure quiet
	ch := askAsync(t, h, core.VerdictPrompt{ID: "c1", SessionID: "s1", Title: "q", Options: vo("y")})
	// the card registers despite the store failing to persist it.
	waitForPending(t, h, "s1", 1)
	if _, err := h.Answer("s1", "c1", core.Verdict{OptionIndex: 1}); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	r := drain(t, ch)
	if r.v.OptionIndex != 1 {
		t.Fatalf("verdict = %+v, want option 1", r.v)
	}
}

func TestResultMapShapes(t *testing.T) {
	cases := []struct {
		name string
		v    core.Verdict
		want map[string]any
	}{
		{"pick", core.Verdict{CardID: "c1", OptionIndex: 2, Option: "no"},
			map[string]any{"card_id": "c1", "verdict": "answer", "option_index": float64(2), "option": "no"}},
		{"text", core.Verdict{CardID: "c2", Text: "hi"},
			map[string]any{"card_id": "c2", "verdict": "answer", "text": "hi"}},
		{"skip", core.Verdict{CardID: "c1", Skipped: true},
			map[string]any{"card_id": "c1", "verdict": "skip", "skipped": true}},
		{"expired", core.Verdict{CardID: "c1", Expired: true},
			map[string]any{"card_id": "c1", "verdict": "expired", "expired": true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got map[string]any
			if err := json.Unmarshal([]byte(resultMap(tc.v)), &got); err != nil {
				t.Fatalf("resultMap not JSON: %v", err)
			}
			for k, want := range tc.want {
				if got[k] != want {
					t.Errorf("resultMap[%q] = %v, want %v (full: %v)", k, got[k], want, got)
				}
			}
		})
	}
}

func TestPendingFiltersBySessionAndExcludesSettled(t *testing.T) {
	h := NewHub(nil, nil)
	ch1 := askAsync(t, h, core.VerdictPrompt{ID: "c1", SessionID: "s1", Title: "q1", Options: vo("y")})
	ch2 := askAsync(t, h, core.VerdictPrompt{ID: "c2", SessionID: "s2", Title: "q2", Options: vo("y")})
	waitForPending(t, h, "", 2)
	if got := h.Pending("s1"); len(got) != 1 || got[0].ID != "c1" {
		t.Fatalf("Pending(s1) = %+v, want only c1", got)
	}
	if got := h.Pending(""); len(got) != 2 {
		t.Fatalf("Pending('') = %d, want 2", len(got))
	}
	_, _ = h.Answer("s1", "c1", core.Verdict{OptionIndex: 1})
	_ = drain(t, ch1)
	if got := h.Pending(""); len(got) != 1 || got[0].ID != "c2" {
		t.Fatalf("Pending after settle = %+v, want only c2", got)
	}
	_, _ = h.Answer("s2", "c2", core.Verdict{OptionIndex: 1})
	_ = drain(t, ch2)
}

// containsStr kept from the original draft; superseded by strings.Contains.
func containsStr(s, sub string) bool {
	return strings.Contains(s, sub)
}
