package agent

import (
	"context"

	"github.com/EndoTheDev/omega/core"
)

// TurnSink is the completed-turn persistence seam. The agent run loop
// is the ONLY writer of turn copies: every finished message of a run -
// the run's input user messages, one assistant per turn (thinking +
// content + tool calls + Timing), and each tool result - reaches the
// sink in provider-correct order: user -> assistant(A) -> tool
// results -> assistant(B). Channels fetch and render from the store;
// they never append turn content. A nil sink (title runs, one-shot
// CLI, tests) means the run persists nothing.
type TurnSink interface {
	// AppendTurnMessage persists one finished message in run order.
	// ctx is the loop's run context.
	AppendTurnMessage(ctx context.Context, msg core.Message) error
}

// WithTurnSink returns a child context carrying the run's TurnSink.
// Callers wrap their own run context, so parallel runs never share one.
func WithTurnSink(ctx context.Context, sink TurnSink) context.Context {
	if sink == nil {
		return ctx
	}
	return context.WithValue(ctx, turnSinkKey{}, sink)
}

// RunTurnSink returns the run's TurnSink, or nil when the run has no
// persistence seam.
func RunTurnSink(ctx context.Context) TurnSink {
	sink, _ := ctx.Value(turnSinkKey{}).(TurnSink)
	return sink
}

// WithPersistInput tags the run context with the messages the caller
// is adding with this send - the run's new user turns (and fork
// inputs, whose explicit parents the sink honors). The loop persists
// them through the TurnSink before the first turn; the already
// persisted history in Messages is never rewritten.
func WithPersistInput(ctx context.Context, messages []core.Message) context.Context {
	if len(messages) == 0 {
		return ctx
	}
	return context.WithValue(ctx, persistInputKey{}, append([]core.Message(nil), messages...))
}

// PersistInput returns the run's new input messages, or nil.
func PersistInput(ctx context.Context) []core.Message {
	msgs, _ := ctx.Value(persistInputKey{}).([]core.Message)
	return msgs
}

type turnSinkKey struct{}

type persistInputKey struct{}

// NewStoreTurnSink adapts a StoreProvider as a run's TurnSink. Head
// pinning lives here (t_17365247): when headParentID is set - fork of
// a user turn, /retry - the run's FIRST conversation-carrying assistant
// is parented to it explicitly, because the store's derived leaf still
// points at the PREVIOUS run and the sibling-variant hop is caller
// knowledge. Normal sends pin nothing: the head answers the
// just-persisted user message off the derived path. An all-empty
// assistant (blank stream before an error end) writes nothing - the
// message the provider never produced has no transcript row.
func NewStoreTurnSink(store core.StoreProvider, sessionID, headParentID string) TurnSink {
	if store == nil || sessionID == "" {
		return nil
	}
	return &storeTurnSink{store: store, sessionID: sessionID, headParentID: headParentID}
}

type storeTurnSink struct {
	store        core.StoreProvider
	sessionID    string
	headParentID string
	headPinned   bool
}

func (s *storeTurnSink) AppendTurnMessage(ctx context.Context, msg core.Message) error {
	a, isAssistant := msg.(core.Assistant)
	if isAssistant &&
		a.Content == "" &&
		(a.Thinking == nil || *a.Thinking == "") &&
		len(a.ToolCalls) == 0 {
		return nil
	}
	if isAssistant && !s.headPinned {
		if s.headParentID != "" {
			msg = core.WithParentID(msg, s.headParentID)
		}
		s.headPinned = true
	}
	return s.store.AppendMessage(ctx, s.sessionID, msg)
}
