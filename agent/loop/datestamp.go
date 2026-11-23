package loop

import (
	"context"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// stampDayInput stamps the day's first user message with [YYYY-MM-DD] on the wire AND on
// the run's new-input copies, so the transcript the sink persists and the messages the
// model sees stay byte-identical - the stamp is content, and content is append-only. A
// wire whose newest user message already carries a stamp is history being replayed
// (retry, fork, wake re-entry): it is returned untouched.
func stampDayInput(ctx context.Context, wire []core.Message, sessionID string) ([]core.Message, context.Context) {
	if agent.DayStampOff(ctx) {
		return wire, ctx
	}
	idx := lastUserIndex(wire)
	if idx < 0 {
		return wire, ctx
	}
	msg, ok := wire[idx].(core.User)
	if !ok {
		return wire, ctx
	}
	input, at := lastUserInput(ctx)
	if at >= 0 {
		if input != msg.Content {
			// The wire's newest user message is not this run's new input: stamp one without
			// the other and the persisted transcript diverges from what the model saw.
			return wire, ctx
		}
	} else if idx != len(wire)-1 || agent.RunTurnSink(ctx) != nil {
		// Two shapes reach here without new input: a wake re-entering on a settled call (the
		// newest user message is buried in history) and a replay of a persisted transcript
		// (retry, fork). Neither is an arrival, and history is never rewritten. A run with
		// no sink and the user message last is a one-shot conversation: that message IS the
		// input.
		return wire, ctx
	}
	stamped := agent.StampUserDay(ctx, sessionID, wire, msg)
	if stamped.Content == msg.Content {
		return wire, ctx
	}
	out := make([]core.Message, len(wire))
	copy(out, wire)
	out[idx] = stamped
	if at >= 0 {
		msgs := agent.PersistInput(ctx)
		next := make([]core.Message, len(msgs))
		copy(next, msgs)
		next[at] = stamped
		ctx = agent.WithPersistInput(ctx, next)
	}
	return out, ctx
}

// lastUserIndex is the position of the newest user message on the wire, -1 when the run
// carries none.
func lastUserIndex(wire []core.Message) int {
	for i := len(wire) - 1; i >= 0; i-- {
		if _, ok := wire[i].(core.User); ok {
			return i
		}
	}
	return -1
}

// lastUserInput is the content and position of the newest user message among the run's
// new input; -1 when the run adds no user message.
func lastUserInput(ctx context.Context) (string, int) {
	msgs := agent.PersistInput(ctx)
	for i := len(msgs) - 1; i >= 0; i-- {
		if u, ok := msgs[i].(core.User); ok {
			return u.Content, i
		}
	}
	return "", -1
}
