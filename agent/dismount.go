// The teardown machinery (v0.12 phase E1, implementation sketch 2,
// grill Q3/Q11): Dismount replays a module's contributions backward;
// DismountAll replays MountAll's recorded topo order in REVERSE. A
// runtime remount mounts through the scoped Context proxy with split
// panic semantics: boot re-panics loudly naming the module; a remount
// panic rolls back, rejects the toggle and the server keeps running.
package agent

import (
	"context"
	"fmt"
	"sort"

	"github.com/EndoTheDev/omega/core"
)

// Dismounter is an optional Plugin capability (v0.12 E1): a module
// whose Mount writes the Context directly (the classic style) proves
// its teardown by reversing exactly what it appended. Modules mounted
// through the ExtensionAPI are journaled and dismount automatically -
// they implement Dismounter only for module-internal teardown beyond
// the Context (state the mount opened that the registry cannot see).
type Dismounter interface {
	// Dismount removes every registration the mount made.
	Dismount(ctx *core.Context) error
}

// DismountAll dismounts the mounted plugin set in the REVERSE of
// MountAll's recorded topo order: last mounted, first removed, so a
// module is always torn down before the seam it consumed. Every
// module must prove its teardown - journaled (mounted through the
// ExtensionAPI) or a Dismount implementation - or the proof fails
// loud naming it.
func DismountAll(reg *Registry, ctx *core.Context) error {
	for i := len(reg.mountOrder) - 1; i >= 0; i-- {
		name := reg.mountOrder[i].Name()
		if !reg.isActive(name) {
			continue
		}
		if err := reg.Dismount(ctx, name); err != nil {
			return fmt.Errorf("dismount all (reversed topo order, at %q): %w", name, err)
		}
	}
	return nil
}

// mountOne is the one mount path boot and remount share (Q7:
// boot-skip and the UI toggle mount through the same code). The phase
// selects only the panic semantics and which Context the writes land
// on - the live Context (boot, applied directly) or the scoped proxy
// (remount, applied at commit). On success it returns the module's
// journal for the caller to commit (remount) or keep (boot, already
// applied and recorded).
func mountOne(target *core.Context, reg *Registry, p Plugin, remount bool) (j *journal, err error) {
	j = newJournal(p.Name())
	defer func() {
		rec := recover()
		if rec == nil {
			return
		}
		mpe := &core.ModulePanicError{
			Name:  p.Name(),
			Cause: fmt.Sprint(rec),
			Suggestion: fmt.Sprintf(
				"add %q to \"disabled\" in omega.json", p.Name()),
		}
		if remount {
			// The Context side died with the scoped proxy - the
			// live Context never saw those writes; the registry
			// side landed live and plays backward.
			j.playBackRegistry(reg)
			err = mpe
			return
		}
		panic(mpe)
	}()

	beforeTools := len(target.ToolProviders)
	beforeCmds := len(target.Commands)
	beforeInfos := len(target.Infos)
	if ap, ok := p.(APIPlugin); ok {
		j.apiMounted = true
		api := &ExtensionAPI{module: p.Name(), ctx: target, reg: reg, j: j, live: !remount}
		if err := ap.MountWithAPI(target, api); err != nil {
			return j, fmt.Errorf("mount %q: %w", p.Name(), err)
		}
		if err := api.commit(); err != nil {
			return j, fmt.Errorf("mount %q: %w", p.Name(), err)
		}
	} else if err := p.Mount(target); err != nil {
		return j, fmt.Errorf("mount %q: %w", p.Name(), err)
	}
	info := core.ExtensionInfo{
		Name:  p.Name(),
		Seams: p.Provides(),
	}
	if d, ok := p.(Describer); ok {
		info.Description = d.Description()
	}
	for _, tp := range target.ToolProviders[beforeTools:] {
		if tp == nil {
			continue
		}
		for name, t := range tp.Tools() {
			// Alias entries are model-facing compat shims, not inventory.
			if t.AliasOf != "" {
				continue
			}
			info.Tools++
			info.ToolList = append(info.ToolList, core.ToolInfo{
				Name:        name,
				Description: t.Description,
				Actions:     t.Actions,
			})
		}
	}
	sort.SliceStable(info.ToolList, func(i, j int) bool { return info.ToolList[i].Name < info.ToolList[j].Name })
	info.Commands = len(target.Commands) - beforeCmds
	target.Infos = append(target.Infos, info)
	j.add(contribution{
		module:   p.Name(),
		desc:     "inventory info",
		ctxApply: func(c *core.Context) { c.Infos = append(c.Infos, info) },
		ctxUndo:  func(c *core.Context) { c.Infos = c.Infos[:beforeInfos] },
		applied:  !remount,
	})
	return j, nil
}

// bootMount is the MountAllAPI loop body over one plugin.
func (reg *Registry) bootMount(target *core.Context, p Plugin) error {
	j, err := mountOne(target, reg, p, false)
	if err != nil {
		return err
	}
	reg.recordJournal(j)
	return nil
}

// Mount mounts one plugin into the live Context (the runtime enable
// path, Q7): all writes land on a scoped Context proxy - a shallow
// copy - and nothing touches the live Context until commit replays
// the journal forward. A panic plays backward and returns as
// *core.ModulePanicError: rejected toggle, zero contributions, the
// server keeps running. deps arms the module's fence handles after
// the commit; a zero StartDeps mounts without starting anything.
func (r *Registry) Mount(ctx *core.Context, p Plugin, deps core.StartDeps) error {
	r.mu.Lock()
	already := r.active[p.Name()]
	r.mu.Unlock()
	if already {
		return fmt.Errorf("module %q already mounted - dismount it before remounting", p.Name())
	}
	target := *ctx         // the scoped Context proxy: the only write path during remount
	target.Lifecycle = nil // the proxy owns a fresh run-group: a panicked mount leaves no fence entries behind
	j, err := mountOne(&target, r, p, true)
	if err != nil {
		return err
	}
	j.commit(ctx) // replay forward onto the live Context
	r.adoptMount(p, j)
	// The emit-on-remount law: the remount's fresh emitter hooks
	// registered past boot's deliver sweep - replay them, or the
	// module's finished-things handoff stays nil until restart.
	r.deliverModuleEmitters(ctx, p.Name())
	if deps.Ctx != nil { // start the module's fence handles; deps arms them
		if err := ctx.RunGroup().StartAll(startCtx(deps), deps, p.Name()); err != nil {
			return fmt.Errorf("module %q mounted but its fence handles failed to start: %w", p.Name(), err)
		}
	}
	return nil
}

// Remount is the live config-change path (Q7): dismount then mount
// through the one shared code path. A failed mount returns the
// rejection (Q11) with the module left dismounted - the caller
// decides whether to re-mount the previous plugin.
func (r *Registry) Remount(ctx *core.Context, p Plugin, deps core.StartDeps) error {
	if r.isActive(p.Name()) {
		if err := r.Dismount(ctx, p.Name()); err != nil {
			return err
		}
	}
	return r.Mount(ctx, p, deps)
}

// Dismount removes one mounted module: its fence handles stop first,
// then the module's own Dismount hook (module-internal state), then
// the journal plays backward. A module with neither a journal (never
// mounted through the ExtensionAPI) nor a Dismount implementation has
// no dismount proof and the call fails loud naming it.
func (r *Registry) Dismount(ctx *core.Context, name string) error {
	r.mu.Lock()
	j := r.journals[name]
	p := r.plugins[name]
	active := r.active[name]
	delete(r.journals, name)
	r.active[name] = false
	r.mu.Unlock()
	if !active {
		return fmt.Errorf("module %q is not mounted", name)
	}
	if j == nil || !j.apiMounted {
		if d, ok := p.(Dismounter); !ok || d == nil {
			return fmt.Errorf("module %q has no dismount proof - it writes the Context directly and implements no Dismount; mount it through the ExtensionAPI or add agent.Dismounter", name)
		}
	}
	if ctx.Lifecycle != nil {
		_ = ctx.RunGroup().StopAll(name)
	}
	if d, ok := p.(Dismounter); ok && d != nil {
		if err := d.Dismount(ctx); err != nil {
			return fmt.Errorf("dismount %q: %w", name, err)
		}
	}
	if j != nil {
		j.undo(ctx, r)
	}
	return nil
}

// startCtx resolves the context the handles run under: the host's Run
// context, or Background when the caller armed nothing.
func startCtx(deps core.StartDeps) context.Context {
	if deps.Run == nil {
		return context.Background()
	}
	return deps.Run
}

func sortToolList(l []core.ToolInfo) {
	sort.SliceStable(l, func(i, j int) bool { return l[i].Name < l[j].Name })
}
