package core

import (
	"context"
	"encoding/json"
	"sync"
)

// Context is the shared service container that extensions mount into.
// The host creates a Context, runs MountAllAPI to populate it, then
// passes it to the agent loop.
//
// Exclusive seam offers (provider, store, logger, compactor, prompt,
// nudger, approvals, loop - part 3, the seam constitution) live in the
// universal registry (Services): one owner per key, capsule services
// resolve by key and assert their own consumer faces. ToolProviders
// and Channels are additive: multiple plugins contribute.
type Context struct {
	// Event observers (card t_72ecca67, M3): additive subscriptions on
	// the run event spine. A module appends its observer at mount (the
	// evidence ledger of the continuous-agents arc) and the agent's
	// runs forward every event through the snapshot taken at run start.
	// Pointer-held so a Context value-copy (dismount replay) shares the
	// one guard instead of copying it.
	eventOb *eventObserverSet

	// Captioner is optional (vision_model knob): the host builds it
	// once config and store are resolved, before NewFromContext, and
	// every agent from this Context captions images through it.
	Captioner Captioner

	// Tool providers (additive — multiple plugins contribute).
	ToolProviders []ToolProvider

	// DisabledTools is the config.yaml `disabled.tools` opt-out
	// (v0.14.1 registry-browser): the host fills it at mount and
	// updates it on every registry toggle; NewFromContext installs it
	// on agents and every future rebuild re-reads it. The merge law
	// (a built-in wins a name conflict) runs before the filter.
	DisabledTools []string

	// Prompt section contributors (additive — multiple plugins
	// contribute). Modules that own prompt content implement
	// PromptSectioner; the prompt builder renders their sections.
	PromptSectioners []PromptSectioner

	// Module routes (additive - v0.12 phase F): module-registered REST
	// routes, journaled through RegisterRoute. The serving channels
	// re-read it at wire and after every live toggle (core.LiveSeams) -
	// a disabled module's routes die without a restart.
	ModuleRoutes []ModuleRoute

	// Panel providers (additive - canvas contract, arc 3a). Modules
	// that own view-model surface append their PanelProvider during
	// Mount - one seam declares both the panels and their action
	// handlers. The canvas runtime seeds the snapshot from these.
	PanelProviders []PanelProvider

	// services is the universal seam registry (part 3, the seam
	services *Services

	// Nudge is gone with the Class A flip (part 3, the seam
	// constitution): the nudger offer resolves by key at bind, the
	// registry above holds it alongside everything else.

	// Channels (additive — multiple plugins contribute delivery
	// transports). The host starts all mounted channels after MountAllAPI.
	Channels []Channel

	// Cross-cutting (set by specific plugins).
	Commands       []ExtensionCommand
	CommandHandler func(ctx context.Context, name, args string) (CommandResult, error)

	// Shared state.
	CWD     string
	Configs map[string]any // per-plugin typed config, keyed by plugin Name()

	// Lifecycle is the goroutine fence run-group (v0.12 E1). Nil
	// until the first module registers a handle - Reach it through
	// RunGroup, never assign it directly.
	Lifecycle *Lifecycle

	// Metadata (built during MountAllAPI).
	Infos []ExtensionInfo
}

// applyOffer lands one validated offer on this Context's registry -
// the mount journal's apply replay (the loud claim check happened at
// the Offer registration; the replay is silent).
func (c *Context) ApplyOffer(key, module string, svc any) {
	if ns, err := c.Services().With(key, module, svc); err == nil {
		c.services = ns
	}
}

// dropOffer is the mount journal's offer undo: the dismount replay
// removes the key when this module still owns it.
func (c *Context) DropOffer(key, module string) {
	c.services = c.Services().Without(key, module)
}

// Services returns the Context's universal seam registry, creating it
// on first use. Capsules offer services through the mount API; every
// consumer resolves by key and type-asserts to its own interface.
func (c *Context) Services() *Services {
	if c.services == nil {
		c.services = NewServices()
	}
	return c.services
}

// RunGroup returns the Context's fence run-group (the goroutine
// fence, v0.12 E1), creating it on first use. Modules call it in
// Mount to register Start/Stop handles; background work starts only
// when the host calls StartAll after the mount commit.
func (c *Context) RunGroup() *Lifecycle {
	if c.Lifecycle == nil {
		c.Lifecycle = &Lifecycle{}
	}
	return c.Lifecycle
}

// eventObserverSet guards the Context's spine subscriptions. Heap-held
// behind a pointer: a Context value-copy shares the set instead of
// copying its guard (vet law).
type eventObserverSet struct {
	mu  sync.Mutex
	obs []EventObserver
	// sessObs are the session-attributed subscribers (card t_8de82cff):
	// same spine, same cheap-and-blocking contract, plus the run's
	// session id.
	sessObs []SessionEventObserver
}

func (c *Context) obsSet() *eventObserverSet {
	if c.eventOb == nil {
		c.eventOb = &eventObserverSet{}
	}
	return c.eventOb
}

// AddEventObserver subscribes one observer to the run event spine
// (card t_72ecca67, M3). Modules call it during MountWithAPI with their
// own closure; the run forwards events through the snapshot taken at
// run start, so a registration lands for every later run. Post-mount
// safe: the guard covers a live toggle's remount.
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

// AddSessionEventObserver subscribes one session-attributed observer to
// the run event spine (card t_8de82cff): same registration surface and
// lifetime as AddEventObserver, and the observer also receives the id of
// the session its run belongs to. Registration is post-mount safe for
// the same reason - the run snapshots the set at its start.
func (c *Context) AddSessionEventObserver(o SessionEventObserver) {
	s := c.obsSet()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessObs = append(s.sessObs, o)
}

// SessionEventObservers snapshots the session-attributed spine
// subscriptions at run start.
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

// EmitTick fires one module tick through the spine (card t_72ecca67,
// M4): every registered observer receives it synchronously, and the
// emitter returns after the spine drained. The payload travels as raw
// JSON (the payload law); a marshal failure never blocks the emitter -
// it degrades to an empty payload, the loud consumers validate and
// refuse. Observers are cheap by contract; a tick emitter runs on the
// module's own goroutine, never on a run's.
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
