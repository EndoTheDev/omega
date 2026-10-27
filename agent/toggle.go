// The registry introspection and the live toggle (v0.12 phase E2).
// The webui settings panel renders this data; core provides the
// vocabulary (the settings-toggle small law), the registry provides
// the values. Toggle goes through the one mount machinery boot uses
// (Q7): Dismount replays the journal backward, Mount replays it
// forward through the scoped proxy; a panicking remount rolls back
// and rejects (Q11); rejections land in the refusal ledger (Q12).
// Every toggle is census-checked: a dismount may only remove surface,
// never grow it; an enable may only add it, never shrink it.
package agent

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/EndoTheDev/omega/core"
)

// isToggleable reads the tier law off a plugin: a module is
// toggleable only when it opts in (the Default is NOT - machine-
// critical and engine-ring modules never toggle).
func isToggleable(p Plugin) bool {
	t, ok := p.(Toggleable)
	return ok && t.Toggleable()
}

// catalogProvides maps every seam to its providers across the whole
// catalog - mounted or not. A seam with more than one provider never
// strands dependents when one provider goes down.
func catalogProvides(catalog []Plugin) map[string][]string {
	provides := map[string][]string{}
	for _, p := range catalog {
		for _, seam := range p.Provides() {
			provides[seam] = append(provides[seam], p.Name())
		}
	}
	return provides
}

// loneProviders returns the seams one module is the ONLY provider of
// across the whole catalog (nil-safe: a disabled module keeps its
// catalog entry, so its dependents stay computable while it is off).
func loneProviders(catalog []Plugin, name string) map[string]bool {
	lone := map[string]bool{}
	for seam, providers := range catalogProvides(catalog) {
		if len(providers) == 1 && providers[0] == name {
			lone[seam] = true
		}
	}
	return lone
}

// DependentModules returns the ACTIVE modules that require a seam only
// name provides - the Q3 dependents the UI confirms with. Disabled
// modules are never dependents: Q3 stops them at boot, they are not
// live to strand.
func (r *Registry) DependentModules(name string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.dependentModules(name)
}

func (r *Registry) dependentModules(name string) []string {
	lone := loneProviders(r.catalogOrder, name)
	var out []string
	for _, m := range r.catalogOrder {
		if m.Name() == name || !r.active[m.Name()] {
			continue
		}
		for _, req := range m.Requires() {
			if lone[req] {
				out = append(out, m.Name())
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

// disableCascade returns name plus its transitive dependents, in
// REVERSE mount order: the set one disable takes down, dismount-order
// ready (a dependent tears down before the seam it consumes).
func (r *Registry) disableCascade(name string) []string {
	r.mu.Lock()
	seen := map[string]bool{name: true}
	queue := []string{name}
	for i := 0; i < len(queue); i++ {
		for _, dep := range r.dependentModules(queue[i]) {
			if !seen[dep] {
				seen[dep] = true
				queue = append(queue, dep)
			}
		}
	}
	var dead []string
	collected := map[string]bool{}
	for i := len(r.mountOrder) - 1; i >= 0; i-- {
		n := r.mountOrder[i].Name()
		// mountOrder holds one entry per mount lifetime (a re-enabled
		// module appends again) - the cascade dismounts each module
		// once, at its latest topo position.
		if seen[n] && !collected[n] {
			collected[n] = true
			dead = append(dead, n)
		}
	}
	r.mu.Unlock()
	return dead
}

// Modules returns the catalog introspection: every module in the
// build, its live state and its dependents. Order follows the build's
// plugin list - stable for the UI.
func (r *Registry) Modules() []core.ModuleStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]core.ModuleStatus, 0, len(r.catalogOrder))
	for _, p := range r.catalogOrder {
		st := core.ModuleStatus{
			Name:       p.Name(),
			Provides:   append([]string(nil), p.Provides()...),
			Requires:   append([]string(nil), p.Requires()...),
			Toggleable: isToggleable(p),
		}
		if d, ok := p.(Describer); ok {
			st.Description = d.Description()
		}
		if r.active[p.Name()] {
			st.State = "mounted"
			st.Dependents = r.dependentModules(p.Name())
		} else {
			st.State = "disabled"
		}
		out = append(out, st)
	}
	return out
}

// logRefusal lands a toggle rejection in the ledger (Q12: refusals
// visible as events - the serving surface reads them back).
func (r *Registry) logRefusal(name, predicate string, err error) {
	r.refusals.Log(core.Refusal{Module: name, Seam: "toggle", Predicate: predicate, Err: err, At: time.Now()})
}

// Toggle enables or disables one module live. The disabled-list write
// (omega.json) is the host's job - the registry owns only the mount
// machinery, which is the path boot shares (Q7).
//
// Disable: the cascade (name + transitive dependents) dismounts in
// reverse topo order through the journal path; the census must not
// GROW, or the toggle is rejected and the cascade is best-effort put
// back.
//
// Enable: every required seam must already be mounted (Q3 - the
// rejection names the missing edge); the mount goes through the
// scoped proxy, and a panic rolls back and rejects with the
// ModulePanicError text (Q11).
func (r *Registry) Toggle(ctx *core.Context, name string, enabled bool, deps core.StartDeps) error {
	if enabled {
		return r.toggleOn(ctx, name, deps)
	}
	return r.toggleOff(ctx, name, deps)
}

func (r *Registry) toggleOff(ctx *core.Context, name string, deps core.StartDeps) error {
	r.toggleMu.Lock()
	defer r.toggleMu.Unlock()
	p, ok := r.catalog[name]
	if !ok {
		err := fmt.Errorf("no such module %q", name)
		r.logRefusal(name, "toggle.disable", err)
		return err
	}
	if !isToggleable(p) {
		err := fmt.Errorf("module %q is not toggleable - the disable list only names the opinionated capsules, %q is machine-critical or engine ring", name, name)
		r.logRefusal(name, "toggle.disable", err)
		return err
	}
	if !r.isActive(name) {
		return nil // already off: idempotent, the UI just refreshes
	}
	cascade := r.disableCascade(name)
	before := core.Census(ctx)
	for _, n := range cascade {
		if err := r.Dismount(ctx, n); err != nil {
			// A failed dismount leaves the set partly down - put
			// back what came down and reject.
			err := fmt.Errorf("disable %q: %w", name, err)
			r.logRefusal(name, "toggle.disable", err)
			r.remountForReset(ctx, cascade, deps)
			return err
		}
	}
	after := core.Census(ctx)
	if grew := censusGrew(before, after); len(grew) > 0 {
		err := fmt.Errorf("disable %q rejected: the census grew during teardown (%s) - the module does not fully dismount", name, strings.Join(grew, ", "))
		r.logRefusal(name, "toggle.census", err)
		r.remountForReset(ctx, cascade, deps)
		return err
	}
	return nil
}

func (r *Registry) toggleOn(ctx *core.Context, name string, deps core.StartDeps) error {
	r.toggleMu.Lock()
	defer r.toggleMu.Unlock()
	p, ok := r.catalog[name]
	if !ok {
		err := fmt.Errorf("no such module %q", name)
		r.logRefusal(name, "toggle.enable", err)
		return err
	}
	if r.isActive(name) {
		return nil // already on
	}
	// Q3, surfaced: every required seam must be mounted now. The
	// active-provider map is computed under the registry lock; the
	// toggleMu serialization keeps the window stable after it.
	r.mu.Lock()
	activeProvides := map[string]string{}
	for _, m := range r.catalogOrder {
		if r.active[m.Name()] {
			for _, seam := range m.Provides() {
				activeProvides[seam] = m.Name()
			}
		}
	}
	r.mu.Unlock()
	for _, req := range p.Requires() {
		if _, mounted := activeProvides[req]; mounted {
			continue
		}
		lone, any := loneCatalogProvider(r.catalogOrder, req)
		if any {
			err := fmt.Errorf("module %q requires seam %q, mounted only by disabled module %q - enable %q first (Q3: no silent degradation)", name, req, lone, lone)
			r.logRefusal(name, "toggle.enable", err)
			return err
		}
		err := fmt.Errorf("module %q requires seam %q but no plugin provides it", name, req)
		r.logRefusal(name, "toggle.enable", err)
		return err
	}
	before := core.Census(ctx)
	if err := r.Mount(ctx, p, deps); err != nil {
		r.logRefusal(name, "toggle.enable", err) // Q11: the roll-back rejection rides the ledger
		return err
	}
	after := core.Census(ctx)
	if shrank := censusShrank(before, after); len(shrank) > 0 {
		err := fmt.Errorf("enable %q rejected: enabling shrank the census (%s) - other surface disappeared", name, strings.Join(shrank, ", "))
		r.logRefusal(name, "toggle.census", err)
		return err
	}
	return nil
}

// loneCatalogProvider returns the sole provider of a seam across the
// catalog, when exactly one module provides it.
func loneCatalogProvider(catalog []Plugin, seam string) (string, bool) {
	var providers []string
	for _, m := range catalog {
		for _, s := range m.Provides() {
			if s == seam {
				providers = append(providers, m.Name())
			}
		}
	}
	if len(providers) == 1 {
		return providers[0], true
	}
	return "", false
}

// remountForReset is the rejected-toggle bail-out: put the cascade
// back the way it was, best effort, so a rejected disable leaves a
// running server close to its pre-toggle shape. Order: the cascade
// came last-mounted-first; this walks it back-to-front = original
// mount order.
func (r *Registry) remountForReset(ctx *core.Context, cascade []string, deps core.StartDeps) {
	for i := len(cascade) - 1; i >= 0; i-- {
		n := cascade[i]
		if r.isActive(n) {
			continue
		}
		p := r.catalog[n]
		if p == nil {
			continue
		}
		_ = r.Mount(ctx, p, deps)
	}
}

// censusGrew lists the surfaces after that did not exist before.
func censusGrew(before, after core.CensusSnapshot) []string {
	var out []string
	for k := range after.Slots {
		if !before.Slots[k] {
			out = append(out, "slot "+k)
		}
	}
	out = append(out, listAdded("tools", before.Tools, after.Tools)...)
	out = append(out, listAdded("commands", before.Commands, after.Commands)...)
	out = append(out, listAdded("panels", before.Panels, after.Panels)...)
	out = append(out, listAdded("routes", before.Routes, after.Routes)...)
	out = append(out, listAdded("handles", before.Handles, after.Handles)...)
	if after.Channels > before.Channels {
		out = append(out, "channels")
	}
	if after.TUIProviders > before.TUIProviders {
		out = append(out, "tui_providers")
	}
	if after.PromptSectioners > before.PromptSectioners {
		out = append(out, "prompt_sectioners")
	}
	if after.PanelProviders > before.PanelProviders {
		out = append(out, "panel_providers")
	}
	if after.InjectedMessages && !before.InjectedMessages {
		out = append(out, "injected_messages")
	}
	if after.PendingDelegations && !before.PendingDelegations {
		out = append(out, "pending_delegations")
	}
	sort.Strings(out)
	return out
}

// censusShrank lists the surfaces before that are gone after.
func censusShrank(before, after core.CensusSnapshot) []string {
	var out []string
	for k := range before.Slots {
		if !after.Slots[k] {
			out = append(out, "slot "+k)
		}
	}
	out = append(out, listAdded("tools", after.Tools, before.Tools)...)
	out = append(out, listAdded("commands", after.Commands, before.Commands)...)
	out = append(out, listAdded("panels", after.Panels, before.Panels)...)
	out = append(out, listAdded("routes", after.Routes, before.Routes)...)
	out = append(out, listAdded("handles", after.Handles, before.Handles)...)
	if after.Channels < before.Channels {
		out = append(out, "channels")
	}
	if after.TUIProviders < before.TUIProviders {
		out = append(out, "tui_providers")
	}
	if after.PromptSectioners < before.PromptSectioners {
		out = append(out, "prompt_sectioners")
	}
	if after.PanelProviders < before.PanelProviders {
		out = append(out, "panel_providers")
	}
	if !after.InjectedMessages && before.InjectedMessages {
		out = append(out, "injected_messages")
	}
	if !after.PendingDelegations && before.PendingDelegations {
		out = append(out, "pending_delegations")
	}
	sort.Strings(out)
	return out
}

// listAdded lists entries of newer that are absent from older.
func listAdded(what string, older, newer []string) []string {
	var out []string
	had := map[string]bool{}
	for _, e := range older {
		had[e] = true
	}
	for _, e := range newer {
		if !had[e] {
			out = append(out, what+": "+e)
		}
	}
	return out
}
