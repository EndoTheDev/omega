package core

import (
	"context"
	"encoding/json"
	"sync"
)

// Context is the shared service container that extensions mount.
type Context struct {
	eventOb *eventObserverSet

	Captioner Captioner

	ToolProviders []ToolProvider

	DisabledTools []string

	PromptSectioners []PromptSectioner

	ModuleRoutes []ModuleRoute

	// Panel providers (additive - the canvas contract).
	PanelProviders []PanelProvider

	services *Services


	Channels []Channel

	Commands       []ExtensionCommand
	CommandHandler func(ctx context.Context, name, args string) (CommandResult, error)

	CWD     string
	Configs map[string]any // per-plugin typed config, keyed by plugin Name()

	// Lifecycle is the goroutine fence run-group. Nil until the first module
	// registers a handle - Reach it through RunGroup, never assign it directly.
	Lifecycle *Lifecycle

	// inv is the mounted extension inventory. Two lanes write it - the mount journal
	// (ctxApply/ctxUndo) and a runtime re-stamp - so it carries its own lock; read it
	// through Inventory, never as a field.
	inv *infoLedger
}

// infoLedger is the guarded extension inventory: one mutex for the whole slice, so a
// re-stamp's read-modify-write is atomic against a mount or dismount committing beside it.
type infoLedger struct {
	mu    sync.RWMutex
	infos []ExtensionInfo
}

// InitInventory allocates the inventory ledger. MountAllAPI calls it before the first
// mount, so the ledger every serving surface later reads is fixed before a goroutine runs.
func (c *Context) InitInventory() {
	if c.inv == nil {
		c.inv = &infoLedger{}
	}
}

// ledger returns the inventory ledger, creating it for a hand-built Context. The first
// write to such a Context is its mount - boot-serially, before any reader exists.
func (c *Context) ledger() *infoLedger {
	if c.inv == nil {
		c.inv = &infoLedger{}
	}
	return c.inv
}

// Inventory returns a copy of the mounted extension inventory: the rows the Tools
// section and the extensions listing render. A Context nothing has mounted answers nil.
func (c *Context) Inventory() []ExtensionInfo {
	inv := c.inv
	if inv == nil {
		return nil
	}
	inv.mu.RLock()
	defer inv.mu.RUnlock()
	out := make([]ExtensionInfo, len(inv.infos))
	copy(out, inv.infos)
	return out
}

// AddInfo appends one module's inventory entry - the mount journal's forward write.
func (c *Context) AddInfo(info ExtensionInfo) {
	l := c.ledger()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.infos = append(l.infos, info)
}

// RemoveInfo drops one module's inventory entry by name - the journal's undo. Module
// names are unique (the registry refuses a second mount of one name) and an entry's tool
// list is a live snapshot a runtime re-stamp rewrites, so value identity would miss.
func (c *Context) RemoveInfo(module string) {
	l := c.ledger()
	l.mu.Lock()
	defer l.mu.Unlock()
	for i, info := range l.infos {
		if info.Name != module {
			continue
		}
		out := make([]ExtensionInfo, 0, len(l.infos)-1)
		out = append(out, l.infos[:i]...)
		out = append(out, l.infos[i+1:]...)
		l.infos = out
		return
	}
}

// UpdateInfos applies fn to the inventory under the write lock and returns the stored
// result: the one read-modify-write door for entries that change under a live mount. fn
// must not retain or mutate the slice it is handed.
func (c *Context) UpdateInfos(fn func([]ExtensionInfo) []ExtensionInfo) []ExtensionInfo {
	l := c.ledger()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.infos = fn(l.infos)
	out := make([]ExtensionInfo, len(l.infos))
	copy(out, l.infos)
	return out
}

// Scoped returns the Context copy a remount mounts into: its own inventory (a panicked
// mount leaves the live one untouched) and no fence (a fresh run-group is the proxy's).
func (c *Context) Scoped() Context {
	out := *c
	out.Lifecycle = nil
	out.inv = &infoLedger{infos: c.Inventory()}
	return out
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

// RunGroup returns the Context's fence run-group.
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

// AddSessionEventObserver subscribes one session-attributed observer to the run event.
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
// contract; a tick emitter runs on the module's own goroutine, never.
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
