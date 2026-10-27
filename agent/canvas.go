// The canvas runtime (arc 3a): the host-side registry behind the
// canvas contract. It keeps current panel state per mounted module
// (the snapshot source, settled Q3b), emits one ViewModelDelta per
// state change onto the event spine, and routes action calls to the
// capsule's registered handler through one seam (settled Q4b).
//
// The runtime is transport-blind: the TUI consumes it in-process and
// http_channel serializes the same snapshot/deltas/action shapes over
// SSE/REST in its own phase. Registration is declarative - modules
// append a core.PanelProvider in Mount; the host builds the Canvas
// after MountAll, and anything else is a wiring bug.
package agent

import (
	"fmt"
	"sort"
	"sync"

	"github.com/EndoTheDev/omega/core"
)

// Canvas is the current panel state across mounted modules. Safe for
// concurrent use: module goroutines may Update while the serving
// frontend Snapshots and Dispatches.
type Canvas struct {
	mu       sync.Mutex
	state    map[string]map[string]core.Panel // module -> panel id -> current state
	actions  map[string]map[string]bool       // module -> registered action id
	handlers map[string]core.PanelProvider    // module -> the capsule's registered handler
	order    []string                         // module names in mount order
	emit     func(core.Event)                 // delta emitter, nil-safe

	// tallies is the per-seam accept-counter ledger (v0.12 phase F,
	// sketch 3's drift alarm): one accepted/refused pair per action
	// id, counted at this dispatch boundary - the one every action
	// crosses, declarative or live-provider. Accepted reached the
	// module handler; refused is what the module said no to.
	tallies map[string]*seamTally
}

// seamTally is one accept counter: accepted counts payloads the module
// handler took, refused counts payloads a decode or check rejected
// (the module's own no counts - the Q12 ledger records only the
// boundary-owned ShapeCheck rejections on top of this).
type seamTally struct {
	accepted int
	refused  int
}

// ActionTally is one accept-counter snapshot (phase F, sketch 3).
type ActionTally struct {
	Accepted int
	Refused  int
}

// ActionTallies returns the accept counters of every action id that
// crossed this canvas - the drift alarm the conformance suite diffs
// across boots and mutation batteries. Copy under the canvas lock: the
// live map never escapes.
func (c *Canvas) ActionTallies() map[string]ActionTally {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]ActionTally, len(c.tallies))
	for id, t := range c.tallies {
		out[id] = ActionTally{Accepted: t.accepted, Refused: t.refused}
	}
	return out
}

// tally records one verdict of the action id at the boundary. Locks
// the canvas mutex itself: the single call site (Dispatch) is past
// its own critical section, and concurrent Dispatchers race the map
// otherwise (the -race proof demanded this guard).
func (c *Canvas) tally(actionID string, refusable bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.tallies == nil {
		c.tallies = make(map[string]*seamTally)
	}
	t := c.tallies[actionID]
	if t == nil {
		t = &seamTally{}
		c.tallies[actionID] = t
	}
	if refusable {
		t.refused++
	} else {
		t.accepted++
	}
}

// NewCanvas builds the runtime from a mounted Context. Each
// PanelProvider seeds its module's panel state from its declared
// panels and its action index from those panels' Actions. The module
// name is the one all the provider's declared actions agree on - the
// capsule's name on its own declaration, not a host lookup. Mount
// order sets the snapshot's module order; panel ids sort within a
// module. emit receives one ViewModelDelta per Update - nil means
// state changes are tracked but no deltas ride the spine (the
// channel/rest wiring is each phase-3 frontend's job).
func NewCanvas(ctx *core.Context, emit func(core.Event)) (*Canvas, error) {
	c := &Canvas{
		state:    make(map[string]map[string]core.Panel),
		actions:  make(map[string]map[string]bool),
		handlers: make(map[string]core.PanelProvider),
		emit:     emit,
	}
	for _, p := range ctx.PanelProviders {
		module, err := c.register(p)
		if err != nil {
			return nil, err
		}
		// A provider that owns state changing outside the action pipe
		// gets the update closure for its own module namespace - the
		// installation is registration, not wiring (arc 4b law: the
		// sink rides the same registration that declares the panels,
		// so an unmounted provider ships no emitter either).
		if module != "" {
			if sink, ok := p.(core.PanelUpdateSink); ok {
				m := module
				sink.SetPanelUpdate(func(panel core.Panel) error {
					return c.Update(m, panel)
				})
			}
		}
	}
	return c, nil
}

// register mounts one module's declarations. The module name is
// returned so the caller can install a PanelUpdateSink closure bound
// to the same namespace; empty when the provider contributes nothing
// (no actions, no capsule name).
func (c *Canvas) register(p core.PanelProvider) (string, error) {
	declared := p.Panels()
	module := ""
	seen := map[string]bool{}
	for _, panel := range declared {
		for _, a := range panel.Actions {
			if a.Module == "" {
				return "", fmt.Errorf("canvas register: action %q carries no module", a.ID)
			}
			if !seen[a.Module] {
				seen[a.Module] = true
				if module != "" && module != a.Module {
					return "", fmt.Errorf("canvas register: one provider declares actions of %q and %q", module, a.Module)
				}
				module = a.Module
			}
		}
	}
	if module == "" {
		// An actionless provider has no capsule name to route to. It
		// contributes nothing to the canvas: registration is silent.
		return "", nil
	}
	if _, mounted := c.handlers[module]; mounted {
		return "", fmt.Errorf("canvas register: module %q registered twice", module)
	}

	panels := make(map[string]core.Panel, len(declared))
	actionIDs := make(map[string]bool)
	for _, panel := range declared {
		if panel.ID == "" {
			return "", fmt.Errorf("canvas register: module %q declared a panel with no id", module)
		}
		if _, dup := panels[panel.ID]; dup {
			return "", fmt.Errorf("canvas register: panel %q declared twice in %q", panel.ID, module)
		}
		panels[panel.ID] = panel
		for _, a := range panel.Actions {
			actionIDs[a.ID] = true
		}
	}

	c.state[module] = panels
	c.actions[module] = actionIDs
	c.handlers[module] = p
	c.order = append(c.order, module)
	return module, nil
}

// Snapshot returns the current panel state of every mounted module:
// the snapshot half of the snapshot + deltas pattern. Modules come in
// mount order, panel ids sorted within a module - deterministic
// output for a cache, a diff, or a wire response.
func (c *Canvas) Snapshot() []core.Panel {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]core.Panel, 0, len(c.order)*2)
	for _, module := range c.order {
		ids := make([]string, 0, len(c.state[module]))
		for id := range c.state[module] {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			out = append(out, c.state[module][id])
		}
	}
	return out
}

// Update replaces one panel's current state and emits a ViewModelDelta
// carrying it. The panel's kind/title and its Actions travel with
// every delta (descriptors are small by law, self-describing beats a
// second message kind). The module must be mounted and the panel must
// carry the id it updates by - an unknown module is a wiring bug, an
// undeclared panel id likewise (it would mint a phantom panel the
// module never mounted, forever). Both reject with an error. The emit
// happens under the state lock (t_877f4bce): Panel carries no sequence
// number, so a delta emitted after unlock could leave a concurrent
// second Update's delta landing first and regressing both the live
// frontends and the stored state to a stale payload - under one lock,
// delta order is state order. The emitter must therefore not call
// back into the canvas (Snapshot/Update/Dispatch deadlock); the
// transports fan out under their own locks, which is the contract.
func (c *Canvas) Update(module string, panel core.Panel) error {
	if module == "" || panel.ID == "" {
		return fmt.Errorf("canvas update: both module and panel id are required")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, mounted := c.handlers[module]; !mounted {
		return fmt.Errorf("%w: %q", core.ErrUnknownModule, module)
	}
	seed, declared := c.state[module][panel.ID]
	if !declared {
		return fmt.Errorf("canvas update: module %q never declared panel %q - phantom panel rejected", module, panel.ID)
	}
	panel.Actions = seed.Actions
	c.state[module][panel.ID] = panel

	if c.emit != nil {
		c.emit(core.ViewModelDelta{Type: "view_model_delta", Panel: panel})
	}
	return nil
}

// Resync reconciles the runtime against the live Context (v0.12 E2):
// a module dismounted through the live toggle loses its handlers,
// panel rows and action index; a provider the remount appended to the
// Context registers again (registration is the same declarative pass
// boot's build ran). Modules whose provider is still on the Context
// keep their state untouched. The callers hold the toggle serialization:
// this runs after the mount machinery changed the Context and before
// the serving surface re-reads it.
func (c *Canvas) Resync(ctx *core.Context) error {
	// live modules = the providers on the Context that contribute
	// something (a module name derived from their actions).
	live := map[string]core.PanelProvider{}
	c.mu.Lock()
	for _, p := range ctx.PanelProviders {
		if p == nil {
			continue
		}
		module := moduleNameOf(p)
		if module == "" {
			continue
		}
		live[module] = p
	}
	for module := range c.handlers {
		if _, mounted := live[module]; !mounted {
			delete(c.state, module)
			delete(c.actions, module)
			delete(c.handlers, module)
			c.removeOrder(module)
		}
	}
	c.mu.Unlock()
	for module, p := range live {
		c.mu.Lock()
		_, mounted := c.handlers[module]
		c.mu.Unlock()
		if !mounted {
			if _, err := c.register(p); err != nil {
				return err
			}
		}
	}
	return nil
}

// removeOrder drops one module from the snapshot order (caller holds
// the lock).
func (c *Canvas) removeOrder(module string) {
	for i, m := range c.order {
		if m == module {
			c.order = append(c.order[:i], c.order[i+1:]...)
			return
		}
	}
}

// moduleNameOf derives the capsule name a PanelProvider's actions all
// agree on - the same rule register enforces, without the
// already-registered error path.
func moduleNameOf(p core.PanelProvider) string {
	module := ""
	for _, panel := range p.Panels() {
		for _, a := range panel.Actions {
			if a.Module == "" {
				continue
			}
			if module == "" {
				module = a.Module
			}
		}
	}
	return module
}

// Dispatch routes one action call through the pipe (settled Q4b): the
// module must be mounted, the action id must be one the module
// registered on its panels, and then the capsule's handler receives
// the call verbatim. Route failures match the 404 sentinels
// (core.ErrUnknownModule, core.ErrUnknownAction) via errors.Is; a
// handler's own error passes through untouched.
func (c *Canvas) Dispatch(call core.ActionCall) (any, error) {
	if call.Module == "" {
		return nil, fmt.Errorf("%w: %q", core.ErrUnknownModule, call.Module)
	}
	c.mu.Lock()
	handler, mounted := c.handlers[call.Module]
	actionOK := mounted && c.actions[call.Module][call.Action]
	c.mu.Unlock()
	if !mounted {
		return nil, fmt.Errorf("%w: %q", core.ErrUnknownModule, call.Module)
	}
	if !actionOK {
		return nil, fmt.Errorf("%w: %q", core.ErrUnknownAction, call.Action)
	}
	res, err := handler.HandleAction(call)
	c.tally(call.Action, err != nil)
	return res, err
}
