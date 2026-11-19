// The canvas runtime (arc 3a): the host-side registry behind the canvas contract.
package agent

import (
	"fmt"
	"sort"
	"sync"

	"github.com/EndoTheDev/omega/core"
)

// Canvas is the current panel state across mounted modules. Safe for concurrent use:
// module goroutines may Update while the serving frontend Snapshots and Dispatches.
type Canvas struct {
	mu       sync.Mutex
	state    map[string]map[string]core.Panel // module -> panel id -> current state
	actions  map[string]map[string]bool       // module -> registered action id
	handlers map[string]core.PanelProvider    // module -> the capsule's registered handler
	order    []string                         // module names in mount order
	emit     func(core.Event)                 // delta emitter, nil-safe

	tallies map[string]*seamTally
}

// seamTally is one accept counter.
type seamTally struct {
	accepted int
	refused  int
}

// ActionTally is one accept-counter snapshot (phase F, sketch 3).
type ActionTally struct {
	Accepted int
	Refused  int
}

// ActionTallies returns the accept counters of every action id that crossed this. Copy
// under the canvas lock: the live map never escapes.
func (c *Canvas) ActionTallies() map[string]ActionTally {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]ActionTally, len(c.tallies))
	for id, t := range c.tallies {
		out[id] = ActionTally{Accepted: t.accepted, Refused: t.refused}
	}
	return out
}

// tally records one verdict of the action id at the boundary. Locks the canvas mutex
// itself: the single call site (Dispatch) is past its own critical section, and
// concurrent Dispatchers race the map otherwise (the -race proof demanded this guard).
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

// NewCanvas builds the runtime from a mounted Context. Mount order sets the snapshot's
// module order; panel ids sort within a module. emit receives one ViewModelDelta per
// Update - nil means state changes are tracked but no deltas ride the spine (the
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
		c.installSink(module, p)
	}
	return c, nil
}

// installSink hands one registered provider its update closure - the arc 4b law.
func (c *Canvas) installSink(module string, p core.PanelProvider) {
	if module == "" {
		return
	}
	if sink, ok := p.(core.PanelUpdateSink); ok {
		m := module
		sink.SetPanelUpdate(func(panel core.Panel) error {
			return c.Update(m, panel)
		})
	}
}

// register mounts one module's declarations. The module name is returned so the caller
// can install a PanelUpdateSink closure bound to the same namespace; empty when the
// provider contributes nothing (no actions, no capsule name).
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

// Snapshot returns the current panel state of every mounted module. Modules come in
// mount order, panel ids sorted within a module - deterministic output for a cache, a
// diff, or a wire response.
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

// Update replaces one panel's current state and emits a ViewModelDelta carrying it. The
// module must be mounted and the panel must carry the id it updates by - an unknown
// module is a wiring bug, an undeclared panel id likewise (it would mint a phantom
// panel the module never mounted, forever). The emit happens under the state lock:
// Panel carries no sequence number, so a delta emitted after unlock could leave a
// concurrent second Update's delta landing first and regressing both the live frontends
// and the stored state to a stale payload - under one lock, delta order is state order.
// The emitter must therefore not call back into the canvas (Snapshot/Update/Dispatch
// deadlock); the transports fan out under their own locks, which is the contract.
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

// Resync reconciles the runtime against the live Context (v0.12 E2). The callers hold
// the toggle serialization: this runs after the mount machinery changed the Context and
// before the serving surface re-reads it.
func (c *Canvas) Resync(ctx *core.Context) error {
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
			module, err := c.register(p)
			if err != nil {
				return err
			}
			c.installSink(module, p)
		}
	}
	return nil
}

// removeOrder drops one module from the snapshot order (caller holds the lock).
func (c *Canvas) removeOrder(module string) {
	for i, m := range c.order {
		if m == module {
			c.order = append(c.order[:i], c.order[i+1:]...)
			return
		}
	}
}

// moduleNameOf derives the capsule name a PanelProvider's actions all agree.
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

// Dispatch routes one action call through the pipe (settled Q4b): the module must be
// mounted, the action id must be one the module registered on its panels, and then the
// capsule's handler receives the call verbatim.
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
