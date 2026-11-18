package core

import (
	"context"
	"encoding/json"
	"sync"
)

// Context is the shared service container that extensions mount into.
type Context struct {
	eventOb *eventObserverSet

	Captioner Captioner

	ToolProviders []ToolProvider

	DisabledTools []string

	PromptSectioners []PromptSectioner

	ModuleRoutes []ModuleRoute

	// Panel providers (additive - canvas contract, arc 3a).
	PanelProviders []PanelProvider

	services *Services

	// Nudge is gone with the Class A flip (part 3, the seam constitution).

	Channels []Channel

	Commands       []ExtensionCommand
	CommandHandler func(ctx context.Context, name, args string) (CommandResult, error)

	CWD     string
	Configs map[string]any // per-plugin typed config, keyed by plugin Name()

	// Lifecycle is the goroutine fence run-group (v0.12 E1). Nil until the first module
	// registers a handle - Reach it through RunGroup, never assign it directly.
	Lifecycle *Lifecycle

	Infos []ExtensionInfo
}

// applyOffer lands one validated offer on this Context's registry.
func (c *Context) ApplyOffer(key, module string, svc any) {
	if ns, err := c.Services().With(key, module, svc); err == nil {
		c.services = ns
	}
}

// dropOffer is the mount journal's offer undo.
func (c *Context) DropOffer(key, module string) {
	c.services = c.Services().Without(key, module)
}

// Services returns the Context's universal seam registry, creating it on first use.
func (c *Context) Services() *Services {
	if c.services == nil {
		c.services = NewServices()
	}
	return c.services
}

// RunGroup returns the Context's fence run-group (the goroutine fence, v0.12 E1).
func (c *Context) RunGroup() *Lifecycle {
	if c.Lifecycle == nil {
		c.Lifecycle = &Lifecycle{}
	}
	return c.Lifecycle
}

// eventObserverSet guards the Context's spine subscriptions.
type eventObserverSet struct {
	mu  sync.Mutex
	obs []EventObserver
	// sessObs are the session-attributed subscribers: same spine, same cheap-and-blocking
	// contract, plus the run's session id.
	sessObs []SessionEventObserver
}

func (c *Context) obsSet() *eventObserverSet {
	if c.eventOb == nil {
		c.eventOb = &eventObserverSet{}
	}
	return c.eventOb
}

// AddEventObserver subscribes one observer to the run event spine.
func (c *Context) AddEventObserver(o EventObserver) {
	s := c.obsSet()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.obs = append(s.obs, o)
}

// EventObservers snapshots the spine subscriptions at run start.
func (c *Context) EventObservers() []EventObserver {
	s := c.eventOb
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]EventObserver, len(s.obs))
	copy(out, s.obs)
	return out
}

// AddSessionEventObserver subscribes one session-attributed observer to the run event
// spine.
func (c *Context) AddSessionEventObserver(o SessionEventObserver) {
	s := c.obsSet()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessObs = append(s.sessObs, o)
}

// SessionEventObservers snapshots the session-attributed spine subscriptions at run.
func (c *Context) SessionEventObservers() []SessionEventObserver {
	s := c.eventOb
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]SessionEventObserver, len(s.sessObs))
	copy(out, s.sessObs)
	return out
}

// EmitTick fires one module tick through the spine. The payload travels as raw JSON
// (the payload law); a marshal failure never blocks the emitter - it degrades to an
// empty payload, the loud consumers validate and refuse. Observers are cheap by
// contract; a tick emitter runs on the module's own goroutine, never on a run's.
func (c *Context) EmitTick(module, kind string, payload any) {
	raw, err := json.Marshal(payload)
	if err != nil {
		raw = nil
	}
	tick := TickEvent{Module: module, Kind: kind, Payload: raw}
	for _, o := range c.EventObservers() {
		o(tick)
	}
}
