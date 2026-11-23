// The universal mount surface: the universal API is the ONLY mount path (the classic
// seam-assignment ceremony is retired). A capsule calls api.Offer(key, svc) at
// MountWithAPI - one stringly-keyed registration vocabulary; every seam claim, additive
// append and payload boundary is a journaled registration the dismount replays backward.
//
// Exclusive seams are Offers under a CLOSED need-key namespace: provider, store, loop,
// logger, compactor, prompt, approvals (the loop's needs resolve at bind,
// agent/seams.go bindNeed), plus the capsule-to-capsule keys skills, memory, trust, cron,
// notifications and verdicts, reached through consumer-owned interfaces. Proposing a need
// key outside that list is a design event, argued in a card.
//
// Additive surfaces: RegisterTool / RegisterRuntimeTools, RegisterPromptSectioner,
// RegisterPanelProvider (a stateful core.PanelProvider - panels computed per snapshot,
// a core.PanelUpdateSink pushing mutations; kanban's capsule is the reference),
// RegisterChannel, RegisterCommand and RegisterSubcommand. Two families have no Context
// field of their own: routes (RegisterRoute - module REST) and emitters (RegisterEmitter
// - the finished-things handoff).
//
// Identity: machine-facing names - tool names, panel action ids - are namespaced strings
// ("module.verb"), the tenant half enforced against the mounting capsule at registration.
// User-facing names (slash commands) and wire paths (route patterns) keep their natural
// shape; the registry records their owner and rejects a cross-tenant claim. Same-tenant
// re-registration replaces (the clean-idempotent remount); a different tenant on a taken
// seam is a boot error, fail loud.
package agent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync"

	"github.com/EndoTheDev/omega/core"
)

// ExtensionAPI tier: whether the mounting capsule ships built-in vocabulary. Module
// registrations must carry the capsule's own namespace; built-in registrations are the
// machine's vocabulary and keep their natural names - both land on the same one-tenant
// ledger, so the collision law holds across the boundary.
func isBuiltin(p Plugin) bool {
	_, toggleable := p.(Toggleable)
	return !toggleable
}

// MountAllAPI mounts the plugin set and returns the registration registry. The refusals
// sink is host-supplied; nil builds the default in-process refusal ledger. A boot panic
// re-panics as *core.ModulePanicError - die loud naming the module and the fix-it hint.
func MountAllAPI(plugins []Plugin, ctx *Context, refusals core.RefusalSink, disabled []string) (*Registry, error) {
	if refusals == nil {
		refusals = NewRefusalLedger()
	}
	enabled, err := filterDisabled(plugins, disabled)
	if err != nil {
		return nil, err
	}
	reg := newRegistry(refusals)
	provides, err := buildProvidesMap(enabled, reg)
	if err != nil {
		return nil, err
	}
	ordered, err := topoSort(enabled, provides)
	if err != nil {
		return nil, err
	}
	for _, p := range ordered {
		if err := reg.bootMount(ctx, p); err != nil {
			return nil, err
		}
	}
	reg.mu.Lock()
	for _, p := range ordered {
		reg.mountOrder = append(reg.mountOrder, p)
		reg.active[p.Name()] = true
		reg.plugins[p.Name()] = p
	}
	// The catalog keeps EVERY module of the build.
	reg.catalog = make(map[string]Plugin, len(plugins))
	for _, p := range plugins {
		reg.catalog[p.Name()] = p
		reg.catalogOrder = append(reg.catalogOrder, p)
	}
	reg.mu.Unlock()
	reg.deliverEmitters(ctx)
	return reg, nil
}

// Registry is the process's registration state. Created per MountAllAPI wiring - a
// remount builds a fresh registry, which is what makes re-registration a replace and
// the mount stay clean-idempotent.
type Registry struct {
	mu       sync.Mutex
	owner    map[string]string // seam name or namespace pair -> tenant module
	emitter  []emitterReg      // finished-things hooks, delivered at mount end
	routes   []Route           // module REST routes, served by the shell
	subcmds  []Subcommand      // module CLI subcommands, dispatched by the omega binary
	refusals core.RefusalSink

	mountOrder []Plugin
	active     map[string]bool
	journals   map[string]*journal
	plugins    map[string]Plugin

	catalog      map[string]Plugin
	catalogOrder []Plugin
	toggleMu     sync.Mutex
}

// Route is one module-registered REST route - the core.ModuleRoute alias.
type Route = core.ModuleRoute

// Routes returns the registered module routes in registration order.
func (r *Registry) Routes() []Route {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Route, len(r.routes))
	copy(out, r.routes)
	return out
}

// Subcommand is one module-owned CLI subcommand: the binary routes `omega <name>` to the
// handler with the untouched argv after the name; the module parses its own argv (core
// never grows a flag framework) and prints its own help.
type Subcommand struct {
	Module  string
	Name    string
	Summary string // the one-line list entry `omega --help` shows
	Handler func(argv []string) error
}

// Subcommands returns the registered module subcommands in registration order.
func (r *Registry) Subcommands() []Subcommand {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Subcommand, len(r.subcmds))
	copy(out, r.subcmds)
	return out
}

// Refusals hands the refusal ledger back. Non-sink refusals (the
// default in-process ledger) expose their entries; a host-supplied
// sink returns nil (the host knows where its own refusals went).
func (r *Registry) Refusals() []core.Refusal {
	if l, ok := r.refusals.(*RefusalLedger); ok {
		return l.Refusals()
	}
	return nil
}

// RefusalLedger is the default in-process ledger.
type RefusalLedger struct {
	mu      sync.Mutex
	entries []core.Refusal
}

// NewRefusalLedger returns an empty in-process ledger.
func NewRefusalLedger() *RefusalLedger { return &RefusalLedger{} }

// Log appends one refusal (core.RefusalSink).
func (l *RefusalLedger) Log(r core.Refusal) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, r)
}

// Refusals returns a copy of every recorded refusal, in order.
func (l *RefusalLedger) Refusals() []core.Refusal {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]core.Refusal, len(l.entries))
	copy(out, l.entries)
	return out
}

func newRegistry(refusals core.RefusalSink) *Registry {
	return &Registry{
		owner:    make(map[string]string),
		active:   make(map[string]bool),
		journals: make(map[string]*journal),
		plugins:  make(map[string]Plugin),
		catalog:  make(map[string]Plugin),
		refusals: refusals,
	}
}

// isActive reports whether a module is mounted, under the registry lock.
func (r *Registry) isActive(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.active[name]
}

// recordJournal lands a successfully mounted module's contributions in the ledger.
func (r *Registry) recordJournal(j *journal) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.journals[j.module] = j
}

// adoptMount accepts a committed remount.
func (r *Registry) adoptMount(p Plugin, j *journal) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.plugins[p.Name()] = p
	r.journals[p.Name()] = j
	r.active[p.Name()] = true
	r.mountOrder = append(r.mountOrder, p)
}

// releaseClaim drops a registry claim at undo time - only when the
// tenant is still the module that recorded it (a re-enabled claim
// chain must never release someone else's name).
func (r *Registry) releaseClaim(key, module string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.owner[key] == module {
		delete(r.owner, key)
	}
}

// claimSlot records the one-tenant rule on a bare seam name ("store", "kanban"). The owner
// map holds seam names and "module.verb" pairs; ParseSeamName guarantees the shapes never
// collide.
func (r *Registry) claimSlot(seam, module string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if owner, ok := r.owner[seam]; ok && owner != module {
		return fmt.Errorf("%w: seam %q mounted by %q, claimed by %q", core.ErrSeamForeignTenant, seam, owner, module)
	}
	r.owner[seam] = module // same tenant re-claims = replace
	return nil
}

// claimSlotQuiet records a provides-sweep claim already conflict-
// checked (the provides sweep is declared data: Provides stays the
// topo + census source) - it never errors, it only lands the tenant.
func (r *Registry) claimSlotQuiet(seam, module string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.owner[seam] = module
}

// emitterReg is one registered finished-things hook.
type emitterReg struct {
	module string
	fn     func(svc any)
}

// deliverEmitters hands the mounted finished-things service (nil when
// the module is absent - emitters stay nil-safe by the classic law)
// to every hook at mount end (boot: MountAllAPI's final deliver).
func (r *Registry) deliverEmitters(ctx *Context) {
	r.mu.Lock()
	hooks := make([]emitterReg, len(r.emitter))
	copy(hooks, r.emitter)
	r.mu.Unlock()
	svc := ctx.Services().Resolve("notifications")
	for _, h := range hooks {
		h.fn(svc)
	}
}

// deliverModuleEmitters replays the emit-on-remount law: a remount's fresh hooks never
// saw the mount-end deliver (they registered after boot's sweep), so Mount delivers the
// named module's hooks itself.
func (r *Registry) deliverModuleEmitters(ctx *Context, module string) {
	r.mu.Lock()
	var hooks []emitterReg
	for _, h := range r.emitter {
		if h.module == module {
			hooks = append(hooks, h)
		}
	}
	r.mu.Unlock()
	svc := ctx.Services().Resolve("notifications")
	for _, h := range hooks {
		h.fn(svc)
	}
}

// ExtensionAPI is one capsule's namespaced view of the mount surface.
type ExtensionAPI struct {
	module  string
	ctx     *core.Context
	reg     *Registry
	j       *journal // this mount's contribution journal
	live    bool     // true = writes are already on the live Context (boot)
	builtin bool     // the capsule ships built-in vocabulary (not Toggleable)
	tools   map[string]core.Tool
	wired   bool // the one tool provider is already on ctx.ToolProviders
	panelProviders []core.PanelProvider
}

// writeSlot records the mount's write into the journal and applies it to the mounting.
// apply runs immediately; undo plays backward at dismount time and removes exactly the
// captured element (removeOne) - never a slice truncation.
func (api *ExtensionAPI) writeSlot(apply func(*core.Context), undo func(*core.Context)) {
	applied := api.live
	if apply != nil {
		apply(api.ctx)
	}
	api.j.add(contribution{
		ctxApply: apply,
		ctxUndo:  undo,
		applied:  applied,
	})
}

// claimRecorded claims a registry key and journals its release, so a
// dismount (or a remount panic) frees the name for the next mount.
func (api *ExtensionAPI) claimRecorded(key string) error {
	if err := api.reg.claimSlot(key, api.module); err != nil {
		return err
	}
	module := api.module
	api.j.add(contribution{
		regUndo: func(r *Registry) { r.releaseClaim(key, module) },
	})
	return nil
}

// claim validates and records a namespaced machine-facing identity: the tenant half must
// be the capsule itself.
func (api *ExtensionAPI) claim(name string) error {
	module, _, err := core.ParseSeamName(name)
	if err != nil {
		return err
	}
	if module != api.module {
		return fmt.Errorf("registration %q must carry its own module namespace (%s.*)", name, api.module)
	}
	api.reg.mu.Lock()
	defer api.reg.mu.Unlock()
	if owner, ok := api.reg.owner[name]; ok && owner != api.module {
		return fmt.Errorf("%w: %q mounted by %q", core.ErrSeamForeignTenant, name, owner)
	}
	api.reg.owner[name] = api.module
	if api.j != nil { // nil only in tests building the api by hand; journal the release
		api.j.add(contribution{
			regUndo: func(r *Registry) { r.releaseClaim(name, module) },
		})
	}
	return nil
}

// RegisterTool adds one tool (ToolProviders, additive). Both land on the same one-tenant
// ledger; a second tenant on a held tool name is a fail-loud error, and a same-tenant
// re-registration replaces (the clean-idempotent remount). The payload contract is raw
// JSON (core.Tool.Run takes map[string]any).
func (api *ExtensionAPI) RegisterTool(name string, t core.Tool) error {
	if api.builtin {
		if err := api.claimRecorded("tool:" + name); err != nil {
			return err
		}
	} else if name == api.module {
		// The bare domain name: the module IS the identity (the one-domain law), so the
		// claim is the bare name.
		if err := api.claimRecorded(name); err != nil {
			return err
		}
	} else if err := api.claim(name); err != nil {
		return err
	}
	api.landTool(name, t)
	return nil
}

// RegisterBuiltinTool adds one tool on the built-in VOCABULARY rail. The exemption
// narrows to the NAME, never the capsule - the tool keeps its one-tenant ledger slot
// (claimed as tool:<name>, journaled for release exactly like RegisterTool's builtin
// branch), the capsule keeps its toggle, and every NEW tool name stays module.verb.
func (api *ExtensionAPI) RegisterBuiltinTool(name string, t core.Tool) error {
	if err := api.claimRecorded("tool:" + name); err != nil {
		return err
	}
	api.landTool(name, t)
	return nil
}

// landTool assembles one registration into the provider the loop reads and arms.
func (api *ExtensionAPI) landTool(name string, t core.Tool) {
	if api.tools == nil {
		api.tools = make(map[string]core.Tool)
	}
	api.tools[name] = t
	if !api.wired {
		api.wired = true
		provider := toolMap(api.tools)
		api.writeSlot(
			func(c *core.Context) { c.ToolProviders = append(c.ToolProviders, provider) },
			func(c *core.Context) { c.ToolProviders = removeOne[core.ToolProvider](c.ToolProviders, provider) },
		)
	}
}

// toolMap is the assembled provider the registrations build.
type toolMap map[string]core.Tool

func (m toolMap) Tools() map[string]core.Tool { return m }

// RegisterRuntimeTools lands the capsule's runtime-loaded tool set: server-qualified
// externally sourced names ("proof.echo") are configuration data, not registered
// vocabulary - they never pass the namespace claim, so a server name outside the
// capsule's own namespace stays illegal to CLAIM while being legal to SERVE.
func (api *ExtensionAPI) RegisterRuntimeTools(p core.ToolProvider) error {
	if p == nil {
		return fmt.Errorf("runtime tools %q: a registration carries its provider", api.module)
	}
	module := api.module
	if err := api.reg.claimSlot("ns:"+module+".", module); err != nil {
		return err
	}
	api.j.add(contribution{
		regUndo: func(r *Registry) { r.releaseClaim("ns:"+module+".", module) },
	})
	api.writeSlot(
		func(c *core.Context) { c.ToolProviders = append(c.ToolProviders, p) },
		func(c *core.Context) { c.ToolProviders = removeOne(c.ToolProviders, p) },
	)
	return nil
}

// RegisterPanelProvider registers a panel provider.
func (api *ExtensionAPI) RegisterPanelProvider(p core.PanelProvider) error {
	if p == nil {
		return fmt.Errorf("panel provider %q: a registration carries its provider", api.module)
	}
	for _, panel := range p.Panels() {
		if panel.ID == "" {
			return fmt.Errorf("panel %q: a registered panel carries its id", api.module)
		}
		for _, a := range panel.Actions {
			module := a.Module
			if module == "" {
				module = api.module
			}
			if module != api.module {
				return fmt.Errorf("panel %q action %q claims module %q - the api mounts %q only", panel.ID, a.ID, a.Module, api.module)
			}
			if err := api.claim(a.ID); err != nil {
				return err
			}
		}
	}
	api.panelProviders = append(api.panelProviders, p)
	return nil
}

// commit appends the module's registered panel providers to ctx.PanelProviders.
func (api *ExtensionAPI) commit() error {
	if len(api.panelProviders) > 0 {
		providers := api.panelProviders
		api.writeSlot(
			func(c *core.Context) {
				for _, p := range providers {
					c.PanelProviders = append(c.PanelProviders, p)
				}
			},
			func(c *core.Context) {
				for _, p := range providers {
					c.PanelProviders = removeOne(c.PanelProviders, p)
				}
			},
		)
	}
	return nil
}

// RegisterRoute registers one module REST route.
func (api *ExtensionAPI) RegisterRoute(pattern string, h func(http.ResponseWriter, *http.Request)) error {
	if pattern == "" {
		return fmt.Errorf("route %q: a registered route carries its pattern", api.module)
	}
	if h == nil {
		return fmt.Errorf("route %q: a registration carries its handler", pattern)
	}
	module := api.module
	api.reg.mu.Lock()
	if owner, ok := api.reg.owner["route:"+pattern]; ok && owner != api.module {
		api.reg.mu.Unlock()
		return fmt.Errorf("%w: route %q mounted by %q", core.ErrSeamForeignTenant, pattern, owner)
	}
	api.reg.owner["route:"+pattern] = api.module
	replaced := false
	for i, r := range api.reg.routes {
		if r.Pattern == pattern {
			api.reg.routes[i].Handler = h // same tenant re-registers = replace
			replaced = true
			break
		}
	}
	if !replaced {
		api.reg.routes = append(api.reg.routes, Route{Module: api.module, Pattern: pattern, Handler: h})
	}
	api.reg.mu.Unlock()
	if api.ctx != nil { // remount: api.ctx is the scoped proxy; boot: the live Context
		api.ctx.ModuleRoutes = append(api.ctx.ModuleRoutes, Route{Module: api.module, Pattern: pattern, Handler: h})
	}
	route := Route{Module: module, Pattern: pattern, Handler: h} // ctxApply must not alias the loop's rt
	api.j.add(contribution{                                      // registry is always live; the Context side follows the mount path
		ctxApply: func(c *core.Context) {
			c.ModuleRoutes = append(c.ModuleRoutes, route)
		},
		ctxUndo: func(c *core.Context) {
			for i, rt := range c.ModuleRoutes {
				if rt.Module == module && rt.Pattern == pattern {
					c.ModuleRoutes = append(c.ModuleRoutes[:i], c.ModuleRoutes[i+1:]...)
					break
				}
			}
		},
		applied: api.live,
		regUndo: func(r *Registry) {
			r.mu.Lock()
			routes := make([]Route, 0, len(r.routes))
			for _, rt := range r.routes {
				if rt.Module == module && rt.Pattern == pattern {
					continue
				}
				routes = append(routes, rt)
			}
			r.routes = routes
			r.mu.Unlock()
			r.releaseClaim("route:"+pattern, module)
		},
	})
	return nil
}

// RegisterSubcommand registers the capsule's CLI subcommand.
func (api *ExtensionAPI) RegisterSubcommand(name, summary string, handler func(argv []string) error) error {
	if name == "" || name != strings.TrimSpace(name) {
		return fmt.Errorf("subcommand %q: a registration carries its bare-word name", name)
	}
	if strings.HasPrefix(name, "-") || strings.HasPrefix(name, "/") {
		return fmt.Errorf("subcommand %q: a CLI subcommand is a bare word - flags are the module's own argv, a leading / names the loop command seam", name)
	}
	if handler == nil {
		return fmt.Errorf("subcommand %q: a registration carries its handler", name)
	}
	module := api.module
	if err := api.reg.claimSlot("cli:"+name, api.module); err != nil {
		return err
	}
	api.reg.mu.Lock()
	replaced := false
	for i, s := range api.reg.subcmds {
		if s.Name == name && s.Module == module {
			api.reg.subcmds[i].Summary = summary // same tenant re-registers = replace
			api.reg.subcmds[i].Handler = handler
			replaced = true
			break
		}
	}
	if !replaced {
		api.reg.subcmds = append(api.reg.subcmds, Subcommand{Module: module, Name: name, Summary: summary, Handler: handler})
	}
	api.reg.mu.Unlock()
	api.j.add(contribution{
		regUndo: func(r *Registry) {
			r.mu.Lock()
			subs := make([]Subcommand, 0, len(r.subcmds))
			for _, s := range r.subcmds {
				if s.Module == module && s.Name == name {
					continue
				}
				subs = append(subs, s)
			}
			r.subcmds = subs
			r.mu.Unlock()
			r.releaseClaim("cli:"+name, module)
		},
	})
	return nil
}

// RegisterEmitter registers the capsule's finished-things hook: at mount end the
// service under the "notifications" key is handed to it (nil when the notifications
// module is absent - emitters stay nil-safe).
func (api *ExtensionAPI) RegisterEmitter(fn func(svc any)) error {
	if fn == nil {
		return fmt.Errorf("emitter %q: a registration carries its hook", api.module)
	}
	module := api.module
	api.reg.mu.Lock()
	api.reg.emitter = append(api.reg.emitter, emitterReg{module: api.module, fn: fn})
	api.reg.mu.Unlock()
	api.j.add(contribution{
		regUndo: func(r *Registry) {
			r.mu.Lock()
			hooks := make([]emitterReg, 0, len(r.emitter))
			for _, e := range r.emitter {
				if e.module == module && sameFunc(e.fn, fn) {
					continue
				}
				hooks = append(hooks, e)
			}
			r.emitter = hooks
			r.mu.Unlock()
		},
	})
	return nil
}

// sameFunc compares two func values by code pointer.
func sameFunc(a, b any) bool {
	return reflect.ValueOf(a).Pointer() == reflect.ValueOf(b).Pointer()
}

// RegisterPromptSectioner maps the additive prompt-section seam.
func (api *ExtensionAPI) RegisterPromptSectioner(p core.PromptSectioner) error {
	if p == nil {
		return fmt.Errorf("prompt sectioner %q: a registration carries its provider", api.module)
	}
	api.writeSlot(
		func(c *core.Context) { c.PromptSectioners = append(c.PromptSectioners, p) },
		func(c *core.Context) { c.PromptSectioners = removeOne(c.PromptSectioners, p) },
	)
	return nil
}

// RegisterChannel maps the additive channel seam.
func (api *ExtensionAPI) RegisterChannel(ch core.Channel) error {
	if ch == nil {
		return fmt.Errorf("channel %q: a registration carries its channel", api.module)
	}
	api.writeSlot(
		func(c *core.Context) { c.Channels = append(c.Channels, ch) },
		func(c *core.Context) { c.Channels = removeOne(c.Channels, ch) },
	)
	return nil
}

// RegisterCommand maps the command seam: the same append onto ctx.Commands and the same
// chain-on-miss CommandHandler wrapper the classic modules build (byte-equal shape -
// parity holds).
func (api *ExtensionAPI) RegisterCommand(name, description string, args []string, h func(ctx context.Context, name, args string) (core.CommandResult, error)) error {
	if name == "" || name[0] != '/' {
		return fmt.Errorf("command %q: an extension command keeps its leading slash", name)
	}
	if h == nil {
		return fmt.Errorf("command %q: a registration carries its handler", name)
	}
	if err := api.claimRecorded(strings.TrimPrefix(name, "/")); err != nil {
		return err
	}
	cmd := core.ExtensionCommand{Name: name, Description: description, Args: args}
	api.writeSlot(
		func(c *core.Context) { c.Commands = append(c.Commands, cmd) },
		func(c *core.Context) { c.Commands = removeOne(c.Commands, cmd) },
	)
	prev := api.ctx.CommandHandler
	api.writeSlot(
		func(c *core.Context) {
			inner := c.CommandHandler // chain on the target Context's current handler
			c.CommandHandler = func(ctx context.Context, n, a string) (core.CommandResult, error) {
				out, err := h(ctx, n, a)
				if err == nil || !errors.Is(err, core.ErrCommandMiss) {
					return out, err
				}
				if inner != nil {
					return inner(ctx, n, a)
				}
				return out, err // the chain's end: nobody owns the name
			}
		},
		func(c *core.Context) { c.CommandHandler = prev },
	)
	return nil
}

// RegisterLifecycle records one Start/Stop fence handle for the capsule: mount never
// starts background work, so every goroutine rides a registered handle.
func (api *ExtensionAPI) RegisterLifecycle(name string, start func(ctx context.Context, deps core.StartDeps) error, stop func() error) error {
	if name == "" {
		return fmt.Errorf("lifecycle handle by %q: a handle carries its name", api.module)
	}
	if start == nil {
		return fmt.Errorf("lifecycle handle %q: a handle carries its start", name)
	}
	module := api.module
	api.writeSlot(
		func(c *core.Context) {
			c.RunGroup().Register(module, core.LifecycleHandle{Name: name, Start: start, Stop: stop})
		},
		func(c *core.Context) {
			if c.Lifecycle != nil {
				c.RunGroup().Unregister(module, name)
			}
		},
	)
	return nil
}

// Offer mounts one service under a stringly-keyed seam: the universal mechanism every
// capsule - and the substrate itself - shares. One owner per key: a foreign offer on a held
// key fails the mount loud, a same-tenant re-offer replaces (the clean-idempotent remount).
// Consumers resolve by key from ctx.Services() and type-assert to their OWN interfaces; the
// key string is data, never a shared type. Offer is journaled like every registration: the
// dismount removes the offer, a remount replaces it.
//
// The key namespace is closed per direction (see the package doc); "chat-runs" is the one
// shell-offered capsule-service key - the consumer owns the face.
func (api *ExtensionAPI) Offer(key string, svc any) error {
	if svc == nil {
		return fmt.Errorf("service %q: a registration carries its service", key)
	}
	module := api.module
	if err := api.claimRecorded(key); err != nil {
		return err
	}
	// A foreign offer on a held key errors loud - never a lazy resolve, never a silent
	// overwrite.
	if _, err := api.ctx.Services().With(key, module, svc); err != nil {
		return err
	}
	api.writeSlot(
		func(c *core.Context) { c.ApplyOffer(key, module, svc) },
		func(c *core.Context) { c.DropOffer(key, module) },
	)
	return nil
}
