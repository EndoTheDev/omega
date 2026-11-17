package core

// The command chain's two shared shapes (card t_d1fe01da): the miss
// sentinel a handler returns for a name it does not own, and the
// invoking surface's session, which rides the command context so a
// session-scoped command acts on the tab that ran it.

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestCommandMissIsStructurallyRecognisable(t *testing.T) {
	err := CommandMiss("/export")
	if !errors.Is(err, ErrCommandMiss) {
		t.Fatalf("CommandMiss does not carry ErrCommandMiss: %v", err)
	}
	// the phrase is the one every surface has printed since the chain
	// existed - the sentinel changes the semantics, not the wording
	if err.Error() != "no handler for /export" {
		t.Fatalf("text = %q, want the historical phrase", err.Error())
	}
	// still a miss when wrapped: a capsule may add its own context
	wrapped := fmt.Errorf("dismount: %w", err)
	if !errors.Is(wrapped, ErrCommandMiss) {
		t.Fatalf("a wrapped miss lost the sentinel: %v", wrapped)
	}
	// any other error is NOT a miss - that distinction is the whole law
	if errors.Is(errors.New("no handler for /export"), ErrCommandMiss) {
		t.Fatalf("a look-alike message read as a miss: the sentinel is type-borne, never string-matched")
	}
}

func TestCommandSessionRoundTrip(t *testing.T) {
	if got := CommandSession(context.Background()); got != "" {
		t.Fatalf("an unbound context carries %q, want empty", got)
	}
	// nil context: the terminal loop's handlers may be called with one
	// under test, and a nil deref here would be a panic into the agent
	if got := CommandSession(nil); got != "" {
		t.Fatalf("nil context carries %q, want empty", got)
	}
	ctx := WithCommandSession(context.Background(), "sess-42")
	if got := CommandSession(ctx); got != "sess-42" {
		t.Fatalf("session = %q, want sess-42", got)
	}
	// an empty id is "no binding", not a binding to the empty string
	if got := CommandSession(WithCommandSession(context.Background(), "")); got != "" {
		t.Fatalf("binding an empty id produced %q", got)
	}
}
