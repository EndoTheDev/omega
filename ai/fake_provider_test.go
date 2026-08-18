package ai

import (
	"context"
	"testing"
	"time"
)

func TestFakeProviderEmitsInOrder(t *testing.T) {
	script := []StreamEvent{
		ResponseChunk{Type: "response", Content: "hel"},
		ResponseChunk{Type: "response", Content: "lo"},
		StreamEnd{Type: "stream_end", FinishReason: "stop"},
	}
	p := NewFakeProvider("fake", script...)
	events := p.Stream(context.Background(), nil, nil)

	var got []StreamEvent
	for e := range events {
		got = append(got, e)
	}
	if len(got) != len(script) {
		t.Fatalf("got %d events, want %d", len(got), len(script))
	}
	for i := range script {
		if got[i] != script[i] {
			t.Errorf("event %d = %#v, want %#v", i, got[i], script[i])
		}
	}
}

func TestFakeProviderEmptyScriptClosesImmediately(t *testing.T) {
	events := NewFakeProvider("fake").Stream(context.Background(), nil, nil)
	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("channel should be closed, got an event")
		}
	case <-time.After(time.Second):
		t.Fatal("channel not closed within 1s")
	}
}

func TestFakeProviderContextCancellationStopsEmission(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	p := NewFakeProvider("fake",
		ResponseChunk{Type: "response", Content: "a"},
		ResponseChunk{Type: "response", Content: "b"},
	).WithDelay(50 * time.Millisecond)
	events := p.Stream(ctx, nil, nil)

	// Consume the first event, then cancel before the second is emitted.
	if _, ok := <-events; !ok {
		t.Fatal("expected first event")
	}
	cancel()

	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("expected channel to close after cancellation, got an event")
		}
	case <-time.After(time.Second):
		t.Fatal("channel not closed within 1s of cancellation")
	}
}
