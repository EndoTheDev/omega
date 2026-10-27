package core

import (
	"context"
	"encoding/json"
	"sync"
)

// Context is the shared service container that extensions mount into.
// Each slot corresponds to a capability seam. The host creates a Context,
// runs MountAllAPI to populate it, then passes it to the agent loop.
//
// Seam slots (Provider, Compactor, Store, Skills, Loop, PromptBuilder)
// are exclusive: one plugin per slot. ToolProviders and Channels are
// additive: multiple plugins contribute.
type Context struct {
	// Event observers (card t_72ecca67, M3): additive subscriptions on
	// the run event spine. A module appends its observer at mount (the
	// evidence ledger of the continuous-agents arc) and the agent's
	// runs forward every event through the snapshot taken at run start.
	// Pointer-held so a Context value-copy (dismount replay) shares the
	// one guard instead of copying it.
	eventOb *eventObserverSet

	// Seam slots (exclusive — one plugin per slot).
	Provider      Provider
	Compactor     CompactionProvider
	Store         StoreProvider
	Skills        SkillsProvider
	Loop          LoopProvider
	Logger        LoggerProvider
	Memory        MemoryProvider
	PromptBuilder PromptBuilder

	// Captioner is optional (vision_model knob): the host builds it
	// once config and store are resolved, before NewFromContext, and
	// every agent from this Context captions images through it.
	Captioner Captioner

	// Tool providers (additive — multiple plugins contribute).
	ToolProviders []ToolProvider

	// Prompt section contributors (additive — multiple plugins
	// contribute). Modules that own prompt content implement
	// PromptSectioner; the prompt builder renders their sections.
	PromptSectioners []PromptSectioner

	// Frontend (exclusive — one frontend at a time). The host calls
	// Run after mounting all extensions.
	Frontend Frontend

	// TUI providers (additive — module-owned TUI commands and
	// screens, card t_88bb7dcf). Each module appends its TUIProvider
	// during Mount - the registration point; a plugin left out of
	// buildPlugins never mounts and its TUI surface is simply absent,
	// the same law the webui enforces through directory registration.
	TUIProviders []TUIProvider

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
	Trust          TrustProvider
	Cron           CronProvider
	// Notifications is the finished-things store seam (card t_5d9032d3):
	// kanban completions, cron fires and dispatched chat runs each emit
	// one record the webui bell serves. Nil when the notifications
	// plugin is absent - every emitter is nil-safe, the HTTP endpoints
	// answer 501.
	Notifications NotificationsProvider

	// Verdicts is the interactive-question seam (verdict cards v1):
	// the verdict tool blocks on Ask, the HTTP channel and the
	// TUI answer through it. Nil when the verdict module is absent -
	// the tool refuses to block without the seam, the HTTP endpoints
	// answer 501.
	Verdicts VerdictsProvider

	// Channels (additive — multiple plugins contribute delivery
	// transports). The host starts all mounted channels after MountAllAPI.
	Channels []Channel

	// Cross-cutting (set by specific plugins).
	Commands           []ExtensionCommand
	CommandHandler     func(ctx context.Context, name, args string) (CommandResult, error)
	InjectedMessages   <-chan InjectedMessage
	PendingDelegations func() int

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
