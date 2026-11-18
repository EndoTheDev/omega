package core

import (
	"context"
	"errors"
	"sync"
)

// The user-stop latch. The first two are the machinery's own ends; the third is the
// human withdrawing the work, and it must reach every piece of work the run put in
// motion - including the yield-class calls that outlive the turn by design
// (agent/yield.go) and the shells those calls spawned. A context alone cannot carry
// that distinction: the dispatched executor DROPS the run context's cancellation on
// purpose (a normal run end must not eat a dispatched call), and a plain cancel says
// nothing about intent. The latch is per RUN, never per call: one stop ends every call
// that run left running, and the next human send re-arms with a fresh latch. It is
// nil-safe, so a host that cannot be stopped (one-shot runs, cron, the CLI loop)
// threads nil and pays nothing.
type RunStop struct {
	mu      sync.Mutex
	done    chan struct{}
	stopped bool
	hooks   map[int]func()
	next    int
}

// ErrUserStopped is the cause a user stop stamps on the work it cut (context.Cause of.
var ErrUserStopped = errors.New("stopped by the user")

// NewRunStop returns a fresh, unstopped latch.
func NewRunStop() *RunStop { return &RunStop{done: make(chan struct{})} }

// Stop latches the stop and runs every registered hook. Idempotent: the
// second call reports false and fires nothing, so a repeated stop
// request on a winding-down run is a clean no-op. Hooks run OUTSIDE the
// latch's lock and must not block - they are cancellation forwards.
func (s *RunStop) Stop() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return false
	}
	s.stopped = true
	close(s.done)
	hooks := make([]func(), 0, len(s.hooks))
	for _, fn := range s.hooks {
		hooks = append(hooks, fn)
	}
	s.hooks = nil
	s.mu.Unlock()
	for _, fn := range hooks {
		fn()
	}
	return true
}

// Stopped reports whether the latch has fired. A nil latch is never
// stopped - there is no user to press anything.
func (s *RunStop) Stopped() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped
}

// Done closes when the latch fires (nil for a nil latch: it never
// fires, so a select on it simply never takes that case).
func (s *RunStop) Done() <-chan struct{} {
	if s == nil {
		return nil
	}
	return s.done
}

// OnStop registers fn to run when the latch fires and returns a
// function that deregisters it (idempotent, safe after the hook ran).
// A latch that ALREADY fired runs fn before returning, so a race
// between registering and stopping can never lose the cancellation.
func (s *RunStop) OnStop(fn func()) (off func()) {
	if s == nil || fn == nil {
		return func() {}
	}
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		fn()
		return func() {}
	}
	if s.hooks == nil {
		s.hooks = map[int]func(){}
	}
	id := s.next
	s.next++
	s.hooks[id] = fn
	s.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			delete(s.hooks, id)
			s.mu.Unlock()
		})
	}
}

// runStopKey carries the run's latch on the run context.
type runStopKey struct{}

// WithRunStop returns a child context carrying the run's stop latch.
// A nil latch wraps nothing: the run is not stoppable by a human.
func WithRunStop(ctx context.Context, stop *RunStop) context.Context {
	if stop == nil {
		return ctx
	}
	return context.WithValue(ctx, runStopKey{}, stop)
}

// RunStopFrom returns the run's stop latch, or nil.
func RunStopFrom(ctx context.Context) *RunStop {
	stop, _ := ctx.Value(runStopKey{}).(*RunStop)
	return stop
}

// RunStopped reports whether the run behind ctx was stopped by the user.
func RunStopped(ctx context.Context) bool {
	return RunStopFrom(ctx).Stopped()
}
