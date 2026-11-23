// The teardown machinery. A
// runtime remount mounts through the scoped Context proxy with split panic semantics:
// boot re-panics loudly naming the module; a remount panic rolls back, rejects the
// toggle and the server keeps running.
package agent

import (
	"context"
	"fmt"
	"sort"

	"github.com/EndoTheDev/omega/core"
)

// Dismounter is an optional Plugin capability: module-internal teardown
// beyond the Context - state the mount opened that the journal cannot see (a
// connection, a watcher).
type Dismounter interface {
	Dismount(ctx *core.Context) error
}

// DismountAll dismounts the mounted plugin set in the REVERSE of MountAllAPI's. Every
// module must prove its teardown - journaled (mounted through the ExtensionAPI) or a
// Dismount implementation - or the proof fails loud naming it.
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

// mountOne is the one mount path boot and remount share. The phase selects only the
// panic semantics and which Context the writes land on - the live Context (boot,
// applied directly) or the scoped proxy (remount, applied at commit). On success it
// returns the module's journal for the caller to commit (remount) or keep (boot,
// already applied and recorded).
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
				"add %q to \"disabled\" in config.yaml", p.Name()),
		}
		if remount {
			// The Context side died with the scoped proxy - the live Context never saw those.
			j.playBackRegistry(reg)
			err = mpe
			return
		}
		panic(mpe)
	}()

	beforeTools := len(target.ToolProviders)
	beforeCmds := len(target.Commands)
	j.apiMounted = true
	api := &ExtensionAPI{module: p.Name(), ctx: target, reg: reg, j: j, live: !remount, builtin: isBuiltin(p)}
	if err := p.MountWithAPI(target, api); err != nil {
		return j, fmt.Errorf("mount %q: %w", p.Name(), err)
	}
	if err := api.commit(); err != nil {
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
		info.ToolList = append(info.ToolList, ToolInfos(tp.Tools())...)
	}
	sort.SliceStable(info.ToolList, func(i, j int) bool { return info.ToolList[i].Name < info.ToolList[j].Name })
	info.Tools = len(info.ToolList)
	for _, cmd := range target.Commands[beforeCmds:] {
		info.CommandNames = append(info.CommandNames, cmd.Name)
	}
	info.Commands = len(target.Commands) - beforeCmds
	target.AddInfo(info)
	j.add(contribution{
		ctxApply: func(c *core.Context) { c.AddInfo(info) },
		ctxUndo:  func(c *core.Context) { c.RemoveInfo(info.Name) },
		applied:  !remount,
	})
	return j, nil
}

// ToolInfos stamps one tool set into the inventory shape the Tools section and the
// extensions listing read: model-facing names sorted, the tool's own description, its
// action one-liners, aliases dropped (an alias is never a name the model calls). The mount
// journal and a runtime re-stamp share it, so a reload can never drift from the mount shape.
func ToolInfos(tools map[string]core.Tool) []core.ToolInfo {
	list := make([]core.ToolInfo, 0, len(tools))
	for name, t := range tools {
		if t.AliasOf != "" {
			continue
		}
		list = append(list, core.ToolInfo{Name: name, Description: t.Description, Actions: t.Actions})
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list
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

// Mount mounts one plugin into the live Context (the runtime enable path). A panic
// plays backward and returns as *core.ModulePanicError: rejected toggle, zero
// contributions, the server keeps running.
func (r *Registry) Mount(ctx *core.Context, p Plugin, deps core.StartDeps) error {
	r.mu.Lock()
	already := r.active[p.Name()]
	r.mu.Unlock()
	if already {
		return fmt.Errorf("module %q already mounted - dismount it before remounting", p.Name())
	}
	target := ctx.Scoped() // the scoped Context proxy: the only write path during remount
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

// Dismount removes one mounted module.
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
