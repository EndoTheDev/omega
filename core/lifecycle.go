// The goroutine fence (v0.12 phase E1, implementation sketch 1, grill
// Q11): a module must not spawn background work in Mount. Mount-time
// code registers Start/Stop lifecycle handles instead; the host calls
// Start after the mount commit. Recover cannot unwind a goroutine that
// a panicked Mount already spawned - the fence is what keeps the
// contribution-rollback story true: a rejected remount leaves zero
// running background work behind.
//
// Start law: Start arms the handle and returns promptly. Long-running
// work launches its own goroutine. A blocking Start is a fence
// violation - the caller of StartAll runs handles in registration
// order, and one blocking handle starves the rest.
//
// Stop is the reverse: StopAll walks the started handles in reverse
// registration order, and every Stop marks the handle unstarted so a
// later remount can start it again.
package core

import (
	"context"
	"fmt"
	"sync"
)

// StartDeps carries what a handle needs at start time - the values
// that do not exist while modules mount (the built agent, the opened
// store). Handles receive them at StartAll, never at registration:
// capturing host state at Mount is the capture the fence outlaws.
type StartDeps struct {
	Ctx   *Context
	Agent *Agent
	Store StoreProvider
	// Run is the cancellation context the armed work runs under -
	// the shutdown signal the host owns (serve passes its signal
	// context). Nil means the caller asked for a mount with nothing
	// armed.
	Run context.Context
}

// LifecycleHandle is one registered unit of background work: a name
// the census reports, the Start that arms it, the Stop that ends it.
type LifecycleHandle struct {
	Name  string
	Start func(ctx context.Context, deps StartDeps) error
	Stop  func() error
}

// lifecycleEntry is a registered handle plus its ownership and state.
type lifecycleEntry struct {
	LifecycleHandle
	owner   string
	started bool
}

// LifecycleSnapshot is the census view of one registered handle.
type LifecycleSnapshot struct {
	Owner   string
	Name    string
	Started bool
}

// Lifecycle is the run-group: the registration surface of the fence.
// Modules reach it through Context.RunGroup; hosts start and stop it.
type Lifecycle struct {
	mu      sync.Mutex
	entries []*lifecycleEntry
}

// Register records a handle for owner module. The same owner
// re-registering the same name replaces (the clean-idempotent
// remount, Q7); the same name under a different owner is a fence
// violation and fails loud. Registration starts nothing.
func (l *Lifecycle) Register(owner string, h LifecycleHandle) {
	if h.Name == "" {
		panic(fmt.Sprintf("lifecycle handle by %q: a handle carries its name", owner))
	}
	if h.Start == nil {
		panic(fmt.Sprintf("lifecycle handle %q by %q: a handle carries its start", h.Name, owner))
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, e := range l.entries {
		if e.Name != h.Name {
			continue
		}
		if e.owner != owner {
			panic(fmt.Sprintf("lifecycle handle %q already registered by %q, claimed by %q", h.Name, e.owner, owner))
		}
		e.LifecycleHandle = h // same owner replaces; state carries over
		return
	}
	l.entries = append(l.entries, &lifecycleEntry{LifecycleHandle: h, owner: owner})
}

// Unregister removes owner's handles (optionally just the named one
// when name is not empty) in reverse registration order, stopping any
// that are started. Returns what it removed.
func (l *Lifecycle) Unregister(owner, name string) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var removed []string
	for i := len(l.entries) - 1; i >= 0; i-- {
		e := l.entries[i]
		if e.owner != owner || (name != "" && e.Name != name) {
			continue
		}
		if e.started && e.Stop != nil {
			_ = e.Stop()
		}
		removed = append(removed, e.Name)
		l.entries = append(l.entries[:i], l.entries[i+1:]...)
	}
	return removed
}

// StartAll arms the unstarted handles owned by owner ("" = all) in
// registration order. On the first failure it stops the ones it
// already started, in reverse, and returns the error: a half-started
// boot is unwound, not abandoned.
func (l *Lifecycle) StartAll(ctx context.Context, deps StartDeps, owner string) error {
	l.mu.Lock()
	var started []*lifecycleEntry
	var armErr error
	for _, e := range l.entries {
		if owner != "" && e.owner != owner {
			continue
		}
		if e.started {
			continue
		}
		if err := e.Start(ctx, deps); err != nil {
			armErr = fmt.Errorf("lifecycle start %q (%s): %w", e.Name, e.owner, err)
			break
		}
		e.started = true
		started = append(started, e)
	}
	stop := started
	l.mu.Unlock()

	if armErr != nil {
		var first error = armErr
		for i := len(stop) - 1; i >= 0; i-- {
			e := stop[i]
			if e.Stop != nil {
				if err := e.Stop(); err != nil && first == nil {
					first = err
				}
			}
			e.started = false
		}
		return first
	}
	return nil
}

// StopAll ends the started handles owned by owner ("" = all) in
// reverse registration order. Started flips to false either way: the
// fence state always says what can be started next. The first error
// is returned after every handle had its turn.
func (l *Lifecycle) StopAll(owner string) error {
	l.mu.Lock()
	var stops []*lifecycleEntry
	for _, e := range l.entries {
		if !e.started {
			continue
		}
		if owner != "" && e.owner != owner {
			continue
		}
		stops = append(stops, e)
	}
	l.mu.Unlock()

	var first error
	for i := len(stops) - 1; i >= 0; i-- {
		e := stops[i]
		if e.Stop != nil {
			if err := e.Stop(); err != nil && first == nil {
				first = err
			}
		}
		e.started = false
	}
	return first
}

// Snapshot returns the census view of every registered handle in
// registration order, started or not.
func (l *Lifecycle) Snapshot() []LifecycleSnapshot {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]LifecycleSnapshot, 0, len(l.entries))
	for _, e := range l.entries {
		out = append(out, LifecycleSnapshot{Owner: e.owner, Name: e.Name, Started: e.started})
	}
	return out
}
