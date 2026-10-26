package core

import (
	"context"
	"errors"
	"testing"
)

func TestRunGroupRegisterReplaceAndConflict(t *testing.T) {
	var l Lifecycle
	l.Register("m1", LifecycleHandle{Name: "h", Start: func(context.Context, StartDeps) error { return nil }})
	l.Register("m1", LifecycleHandle{Name: "h", Start: func(context.Context, StartDeps) error { return nil }}) // same owner replaces
	if got := len(l.Snapshot()); got != 1 {
		t.Fatalf("same-owner re-register replaced: got %d entries, want 1", got)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("cross-owner name takeover must panic")
		}
	}()
	l.Register("m2", LifecycleHandle{Name: "h", Start: func(context.Context, StartDeps) error { return nil }})
}

func TestRunGroupStartAllArmsInOrder(t *testing.T) {
	var l Lifecycle
	var order []string
	for _, name := range []string{"a", "b"} {
		l.Register("m", LifecycleHandle{
			Name: name,
			Start: func(_ context.Context, _ StartDeps) error {
				order = append(order, name)
				return nil
			},
		})
	}
	if err := l.StartAll(context.Background(), StartDeps{}, ""); err != nil {
		t.Fatal(err)
	}
	if len(order) != 2 || order[0] != "a" || order[1] != "b" {
		t.Fatalf("start order: got %v", order)
	}
	for _, s := range l.Snapshot() {
		if !s.Started {
			t.Fatalf("handle %s not marked started", s.Name)
		}
	}
}

func TestRunGroupStartAllFailureUnwindsStarted(t *testing.T) {
	var l Lifecycle
	stopped := []string{}
	boom := errors.New("boom")
	l.Register("m", LifecycleHandle{Name: "ok", Start: func(context.Context, StartDeps) error { return nil }, Stop: func() error { stopped = append(stopped, "ok"); return nil }})
	l.Register("m", LifecycleHandle{Name: "bad", Start: func(context.Context, StartDeps) error { return boom }})
	if err := l.StartAll(context.Background(), StartDeps{}, ""); !errors.Is(err, boom) {
		t.Fatalf("want boom, got %v", err)
	}
	if len(stopped) != 1 || stopped[0] != "ok" {
		t.Fatalf("started handles not unwound in reverse: %v", stopped)
	}
	for _, s := range l.Snapshot() {
		if s.Started {
			t.Fatalf("handle %s still marked started after unwind", s.Name)
		}
	}
}

func TestRunGroupStopAllReverseAndRestart(t *testing.T) {
	var l Lifecycle
	var stops []string
	l.Register("m", LifecycleHandle{Name: "a", Start: func(context.Context, StartDeps) error { return nil }, Stop: func() error { stops = append(stops, "a"); return nil }})
	l.Register("m", LifecycleHandle{Name: "b", Start: func(context.Context, StartDeps) error { return nil }, Stop: func() error { stops = append(stops, "b"); return nil }})
	if err := l.StartAll(context.Background(), StartDeps{}, ""); err != nil {
		t.Fatal(err)
	}
	if err := l.StopAll("m"); err != nil {
		t.Fatal(err)
	}
	if len(stops) != 2 || stops[0] != "b" || stops[1] != "a" {
		t.Fatalf("stop order must reverse start order: got %v", stops)
	}
	// Stopped handles are startable again (the remount path).
	if err := l.StartAll(context.Background(), StartDeps{}, ""); err != nil {
		t.Fatal(err)
	}
}

func TestRunGroupOwnerScoping(t *testing.T) {
	var l Lifecycle
	starts := 0
	l.Register("m1", LifecycleHandle{Name: "h", Start: func(context.Context, StartDeps) error { starts++; return nil }})
	l.Register("m2", LifecycleHandle{Name: "h2", Start: func(context.Context, StartDeps) error { starts++; return nil }})
	if err := l.StartAll(context.Background(), StartDeps{}, "m1"); err != nil {
		t.Fatal(err)
	}
	if starts != 1 {
		t.Fatalf("owner filter broken: started %d", starts)
	}
	l.Unregister("m1", "")
	if got := l.Snapshot(); len(got) != 1 || got[0].Owner != "m2" {
		t.Fatalf("unregister by owner left %v", got)
	}
}

func TestCensusZeroContext(t *testing.T) {
	a := Census(&Context{})
	b := Census(&Context{})
	if !a.Equal(b) {
		t.Fatalf("fresh contexts differ: %v", a.Diff(b))
	}
	if len(a.Slots) != 0 || len(a.Tools) != 0 || len(a.Handles) != 0 {
		t.Fatalf("fresh context census not empty: %+v", a)
	}
}

func TestCensusDetectsSurface(t *testing.T) {
	base := Census(&Context{})
	ctx := &Context{}
	ctx.RunGroup().Register("m", LifecycleHandle{Name: "h", Start: func(context.Context, StartDeps) error { return nil }})
	ctx.Commands = append(ctx.Commands, ExtensionCommand{Name: "/x"})
	c := Census(ctx)
	if !c.Equal(c) {
		t.Fatal("census not equal to itself")
	}
	if d := base.Diff(c); len(d) != 2 {
		t.Fatalf("diff should name commands+handles, got %v", d)
	}
}