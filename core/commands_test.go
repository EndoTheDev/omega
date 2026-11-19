package core

// The command chain's two shared shapes.

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
	if err.Error() != "no handler for /export" {
		t.Fatalf("text = %q, want the historical phrase", err.Error())
	}
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
	if got := CommandSession(WithCommandSession(context.Background(), "")); got != "" {
		t.Fatalf("binding an empty id produced %q", got)
	}
}
