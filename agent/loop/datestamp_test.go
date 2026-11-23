package loop

import (
	"context"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// stampCtx pins the clock and the session for one turn. The session id is ledger-unique
// per test: the stamp ledger is process state, and two tests sharing an id would share a
// day.
func stampCtx(sessionID string, day time.Time) context.Context {
	ctx := agent.WithDateStampClock(context.Background(), func() time.Time { return day })
	return agent.WithSessionID(ctx, sessionID)
}

// runStampedTurn runs one turn and returns the wire the provider saw.
func runStampedTurn(t *testing.T, ctx context.Context, fold, input []core.Message, sink *memSink) []core.Message {
	t.Helper()
	provider := agent.NewFakeProvider("test",
		core.ResponseChunk{Type: "response_chunk", Content: "ok"},
		core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	)
	ctx = agent.WithPersistInput(ctx, input)
	if sink != nil {
		ctx = agent.WithTurnSink(ctx, sink)
	}
	runLoopEvents(t, ctx, agent.LoopOptions{Provider: provider, Messages: fold})
	if len(provider.LastMessages) == 0 {
		t.Fatal("the provider saw no messages")
	}
	return provider.LastMessages
}

// lastUserContent returns the newest user message's content on the wire.
func lastUserContent(t *testing.T, msgs []core.Message) string {
	t.Helper()
	for i := len(msgs) - 1; i >= 0; i-- {
		if u, ok := msgs[i].(core.User); ok {
			return u.Content
		}
	}
	t.Fatal("no user message on the wire")
	return ""
}

// TestDateStampFirstMessageOfTheDay pins the whole arc at the loop: the day's first user
// message reaches the provider AND the sink carrying [YYYY-MM-DD], every later message
// that day rides unstamped, and a session crossing midnight stamps the next day's first.
func TestDateStampFirstMessageOfTheDay(t *testing.T) {
	const session = "loop-date-stamp-first-of-day"
	morning := time.Date(2026, 11, 24, 9, 0, 0, 0, time.Local)
	beforeMidnight := time.Date(2026, 11, 24, 23, 58, 0, 0, time.Local)
	afterMidnight := time.Date(2026, 11, 25, 0, 2, 0, 0, time.Local)

	sink := &memSink{}
	wire := runStampedTurn(t, stampCtx(session, morning),
		[]core.Message{core.NewUser("hi")},
		[]core.Message{core.NewUser("hi")}, sink)
	if got := lastUserContent(t, wire); got != "[2026-11-24] hi" {
		t.Fatalf("first message = %q, want the day's stamp", got)
	}
	if len(sink.msgs) == 0 {
		t.Fatal("the sink persisted nothing")
	}
	persisted := sink.msgs[0].(core.User).Content
	if persisted != "[2026-11-24] hi" {
		t.Errorf("persisted transcript = %q, wire said %q - history and wire diverged", persisted, lastUserContent(t, wire))
	}

	// Second message, same day: the fold it is read back from carries the first stamp.
	fold := []core.Message{core.NewUser(persisted), core.NewAssistant("ok"), core.NewUser("again")}
	sink = &memSink{}
	wire = runStampedTurn(t, stampCtx(session, morning), fold, []core.Message{core.NewUser("again")}, sink)
	if got := lastUserContent(t, wire); got != "again" {
		t.Errorf("second message the same day = %q, want it unstamped", got)
	}
	if got := wire[0].(core.User).Content; got != persisted {
		t.Errorf("the day's earlier message moved to %q, want %q - history is append-only", got, persisted)
	}

	// The session crosses midnight: the next day starts a fresh stamp.
	fold = append(fold, core.NewAssistant("done"), core.NewUser("late"))
	sink = &memSink{}
	wire = runStampedTurn(t, stampCtx(session, beforeMidnight), fold, []core.Message{core.NewUser("late")}, sink)
	if got := lastUserContent(t, wire); got != "late" {
		t.Errorf("still the same day = %q, want it unstamped", got)
	}
	fold = append(fold, core.NewAssistant("ok"))
	fold = append(fold, core.NewUser("after midnight"))
	wire = runStampedTurn(t, stampCtx(session, afterMidnight), fold, []core.Message{core.NewUser("after midnight")}, sink)
	if got := lastUserContent(t, wire); got != "[2026-11-25] after midnight" {
		t.Errorf("first message of the next day = %q, want the next day's stamp", got)
	}
}

// TestDateStampOffLeavesMetadataAlone pins the metadata exemption: a title request or any
// other synthetic instruction turn is not a conversation message, so it carries no date.
func TestDateStampOffLeavesMetadataAlone(t *testing.T) {
	day := time.Date(2026, 11, 24, 9, 0, 0, 0, time.Local)
	ctx := agent.WithDayStampOff(stampCtx("loop-date-stamp-meta", day))
	wire := runStampedTurn(t, ctx, []core.Message{core.NewUser("Write a title")}, nil, nil)
	if got := lastUserContent(t, wire); got != "Write a title" {
		t.Errorf("metadata run message = %q, want it unstamped", got)
	}
}

// TestDateStampLeavesReplayAlone pins the retry/fork law: a run whose newest user message
// already carries a stamp is replaying history, and the wire it builds must be untouched.
// A wake re-entry that ends on a settled result is covered too: its newest user message is
// history, not an arrival.
func TestDateStampLeavesReplayAlone(t *testing.T) {
	const session = "loop-date-stamp-replay"
	day := time.Date(2026, 11, 25, 8, 0, 0, 0, time.Local)
	stamped := core.NewUser("[2026-11-24] already said this")

	wire := runStampedTurn(t, stampCtx(session, day), []core.Message{stamped}, nil, nil)
	if got := lastUserContent(t, wire); got != stamped.Content {
		t.Errorf("replayed message = %q, want %q - content is never rewritten", got, stamped.Content)
	}

	plain := core.NewUser("said before the arc shipped")
	wake := core.NewYieldResult("async:ok", "c1", false, false)
	wire = runStampedTurn(t, stampCtx("loop-date-stamp-wake", day),
		[]core.Message{plain, core.NewAssistant("answer"), wake}, nil, nil)
	if got := lastUserContent(t, wire); got != plain.Content {
		t.Errorf("historical message on a wake run = %q, want %q - not an arrival", got, plain.Content)
	}
	if folded := plain.Content; folded != "said before the arc shipped" {
		t.Errorf("caller's fold mutated to %q", folded)
	}

	// A persisted transcript replayed with no new input (retry) ends on its historical user
	// message: the sink proves it was read back, so the message is not an arrival.
	wire = runStampedTurn(t, stampCtx("loop-date-stamp-retry", day),
		[]core.Message{plain}, nil, &memSink{})
	if got := lastUserContent(t, wire); got != plain.Content {
		t.Errorf("replayed stored message = %q, want %q - history is never rewritten", got, plain.Content)
	}
}
