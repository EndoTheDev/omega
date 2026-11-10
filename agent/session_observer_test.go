package agent

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// TestSessionEventObserversSeeTheRunSession pins the session-attributed
// spine (card t_8de82cff): the observer shape that carries the run's
// session id delivers EVERY event of the run under that id, while the
// plain observers keep seeing the same events unchanged. The id comes
// from the run context, so the attribution default (a store sink's
// session) applies here too.
func TestSessionEventObserversSeeTheRunSession(t *testing.T) {
	var mu sync.Mutex
	var sessions []string
	var sessEvents, plainEvents []core.Event

	plain := func(ev core.Event) {
		mu.Lock()
		defer mu.Unlock()
		plainEvents = append(plainEvents, ev)
	}
	session := func(session string, ev core.Event) {
		mu.Lock()
		defer mu.Unlock()
		sessions = append(sessions, session)
		sessEvents = append(sessEvents, ev)
	}

	ctx := &core.Context{}
	ctx.AddEventObserver(plain)
	ctx.AddSessionEventObserver(session)

	provider := NewFakeProvider("fake",
		core.ResponseChunk{Type: "response_chunk", Content: "hello"},
		core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	)
	ag := newTestAgent(provider, nil, 0)
	ag.SetEventObserverSource(ctx.EventObservers)
	ag.SetSessionEventObserverSource(ctx.SessionEventObservers)

	run := WithSessionID(context.Background(), "sess-observability")
	collect(t, ag.Run(run, []core.Message{core.NewUser("hi")}, nil))

	mu.Lock()
	defer mu.Unlock()
	if len(sessEvents) == 0 {
		t.Fatal("the session observer saw no events - the spine did not forward")
	}
	sessTypes := eventTypes(sessEvents)
	if plainTypes := eventTypes(plainEvents); !slices.Equal(sessTypes, plainTypes) {
		t.Fatalf("the two rails diverged:\n session = %v\n plain   = %v", sessTypes, plainTypes)
	}
	for i, s := range sessions {
		if s != "sess-observability" {
			t.Fatalf("event %d (%s): session = %q, want sess-observability", i, sessTypes[i], s)
		}
	}
	if !slices.Contains(sessTypes, "agent_start") || !slices.Contains(sessTypes, "agent_end") {
		t.Fatalf("run boundaries missing from the spine: %v", sessTypes)
	}
}

// TestSessionObserverCarriesTheSinkDefault pins the attribution default
// on this rail: a run whose host wrapped only a turn sink is still
// attributed (the RunWithProvider default resolves it before the spine
// snapshot), and a sessionless run reports the honest empty id.
func TestSessionObserverCarriesTheSinkDefault(t *testing.T) {
	var mu sync.Mutex
	var sessions []string
	obs := func(session string, ev core.Event) {
		mu.Lock()
		defer mu.Unlock()
		sessions = append(sessions, session)
	}
	ctx := &core.Context{}
	ctx.AddSessionEventObserver(obs)

	provider := NewFakeProvider("fake", core.StreamEnd{Type: "stream_end", FinishReason: "stop"})
	ag := newTestAgent(provider, nil, 0)
	ag.SetSessionEventObserverSource(ctx.SessionEventObservers)

	sinkCtx := WithTurnSink(context.Background(), NewStoreTurnSink(fakeStore{}, "sink-sess-90", ""))
	collect(t, ag.Run(sinkCtx, []core.Message{core.NewUser("hi")}, nil))

	mu.Lock()
	defer mu.Unlock()
	if len(sessions) == 0 {
		t.Fatal("no events observed")
	}
	for _, s := range sessions {
		if s != "sink-sess-90" {
			t.Fatalf("session = %q, want the sink's sink-sess-90", s)
		}
	}
}

// TestSessionEventObserverRegistrationIsSnapshotted pins the per-run
// snapshot rule: an observer registered AFTER a run started does not see
// that run (registration lands for later runs), matching the plain rail.
func TestSessionEventObserverRegistrationIsSnapshotted(t *testing.T) {
	var mu sync.Mutex
	seen := 0
	ctx := &core.Context{}
	ag := newTestAgent(NewFakeProvider("fake", core.StreamEnd{Type: "stream_end", FinishReason: "stop"}), nil, 0)
	ag.SetSessionEventObserverSource(ctx.SessionEventObservers)

	// Snapshot taken here, before any registration.
	events := ag.Run(context.Background(), []core.Message{core.NewUser("hi")}, nil)
	ctx.AddSessionEventObserver(func(string, core.Event) {
		mu.Lock()
		defer mu.Unlock()
		seen++
	})
	collect(t, events)

	mu.Lock()
	first := seen
	mu.Unlock()
	if first != 0 {
		t.Fatalf("a mid-run registration saw %d events; the snapshot must exclude it", first)
	}
	// The next run does see it.
	collect(t, ag.Run(context.Background(), []core.Message{core.NewUser("hi")}, nil))
	mu.Lock()
	defer mu.Unlock()
	if seen == 0 {
		t.Fatal("the observer never saw a later run's events")
	}
}
