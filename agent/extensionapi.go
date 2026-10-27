// The universal mount surface (v0.12 phase F - the universal API is
// the ONLY mount path; the classic seam-assignment ceremony and the
// dual mount of phase B are retired). A capsule calls
// api.Register*(...) at MountWithAPI - one typed registration
// vocabulary in the pi style, every seam claim, additive append and
// payload boundary a journaled registration the dismount replays
// backward.
//
// THE SEAM CENSUS (23 surfaces on the post-B0 tree; the registrations
// cover every one of them):
//
//	Exclusive slots (15)           Registration
//	 Provider                      RegisterProvider
//	 Compactor                     RegisterCompactor
//	 Store                         RegisterStore
//	 Skills                        RegisterSkills
//	 Loop                          RegisterLoop
//	 Logger                        RegisterLogger
//	 Memory                        RegisterMemory
//	 PromptBuilder                 RegisterPromptBuilder
//	 Frontend                      RegisterFrontend
//	 Trust                         RegisterTrust
//	 Cron                          RegisterCron
//	 Notifications                 RegisterNotifications
//	 Verdicts                      RegisterVerdicts
//	 Captioner                     host-built after the mount today
//
//	 Additive surfaces (8)          Registration
//	 ToolProviders                 RegisterTool / RegisterRuntimeTools
//	 PromptSectioners              RegisterPromptSectioner
//	 TUIProviders                  RegisterRenderer
//	 PanelProviders                RegisterPanelProvider (a stateful
//	                              core.PanelProvider - Panels()
//	                              computed per snapshot, a
//	                              core.PanelUpdateSink pushing through
//	                              mutations; kanban's capsule is the
//	                              reference)
//	 Channels                      RegisterChannel
//	 Commands + CommandHandler     RegisterCommand
//	 CLI subcommands (phase D)     RegisterSubcommand
//	 InjectedMessages              RegisterDelegation
//	 PendingDelegations            RegisterDelegation
//
// Two families have no Context field of their own: routes
// (RegisterRoute - module REST; the http channel consumes it with the
// phase-D route migration) and emitters (RegisterEmitter - the
// finished-things handoff the capsules capture directly today).
//
// Identity (settled Q5/Q10): machine-facing names - tool names, panel
// action ids - are namespaced strings ("module.verb"), the tenant half
// enforced against the mounting capsule at registration. User-facing
// names (slash commands) and wire paths (route patterns) keep their
// natural shape; the registry records their owner and rejects a
// cross-tenant claim. Same-tenant re-registration replaces (the
// clean-idempotent remount, Q7); a different tenant on a taken seam is
// a boot error, fail loud (Q5).
package agent

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync"

	"github.com/EndoTheDev/omega/core"
)

// ExtensionAPI tier: whether the mounting capsule ships built-in
// vocabulary (machine-critical, not toggleable - provider, store,
// skills, tools, the loop) or module vocabulary (the opinionated
// capsules). Module registrations must carry the capsule's own
// namespace (Q10); built-in registrations are the engine's vocabulary
// and keep their natural names - both land on the same one-tenant
// ledger, so the collision law holds across the boundary.
func isBuiltin(p Plugin) bool {
	_, toggleable := p.(Toggleable)
	return !toggleable
}

// MountAllAPI mounts the plugin set and returns the
// registration registry: claims, module routes and the refusal
// ledger. The refusals sink is host-supplied; nil builds the default
// in-process refusal ledger. disabled names the modules omega.json
// toggles off (Q1): Mount skips them; a remaining plugin depending on
// a disabled module fails the boot naming the edge (Q3). A boot panic
// re-panics as *core.ModulePanicError - die loud naming the module
// and the fix-it hint (Q11).
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
	// The catalog keeps EVERY module of the build - disabled ones too -
	// so the E2 registry surface can introspect and remount them.
	reg.catalog = make(map[string]Plugin, len(plugins))
	for _, p := range plugins {
		reg.catalog[p.Name()] = p
		reg.catalogOrder = append(reg.catalogOrder, p)
	}
	reg.mu.Unlock()
	reg.deliverEmitters(ctx)
	return reg, nil
}

// Registry is the process's registration state: the one-name-one-
// tenant ledger (Q5/Q10), the registrations only the host consumes
// (module REST routes; phase-D consumption), and the refusal sink
// every boundary rejection lands in (Q12). Created per MountAllAPI
// wiring - a remount builds a fresh registry, which is what makes
// re-registration a replace and the mount stay clean-idempotent.
type Registry struct {
	mu       sync.Mutex
	owner    map[string]string // seam name or namespace pair -> tenant module
	emitter  []emitterReg      // finished-things hooks, delivered at mount end
	routes   []Route           // module REST, consumed in phase D
	subcmds  []Subcommand      // module CLI subcommands, dispatched by the omega binary
	refusals core.RefusalSink

	// Teardown state (v0.12 E1): the recorded topo order DismountAll
	// replays in reverse, which modules are live, each module's
	// contribution journal and the plugin handles by name.
	mountOrder []Plugin
	active     map[string]bool
	journals   map[string]*journal
	plugins    map[string]Plugin

	// Introspection + toggle state (v0.12 E2): the full plugin
	// catalog - mounted or disabled - behind the registry's module
	// surface, its build order for the UI list, and the mutex
	// serializing live toggles.
	catalog      map[string]Plugin
	catalogOrder []Plugin
	toggleMu     sync.Mutex
}

// Route is one module-registered REST route - the core.ModuleRoute
// alias: the shape rides the live Context (phase F), the registry
// keeps the claim ledger and the registration order. Nothing serves
// these from the registry; the channels read ctx.ModuleRoutes.
type Route = core.ModuleRoute

// Routes returns the registered module routes in registration order.
func (r *Registry) Routes() []Route {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Route, len(r.routes))
	copy(out, r.routes)
	return out
}

// Subcommand is one module-owned CLI subcommand (v0.12 phase D, grill
// Q6 - CLI homecoming): the binary routes `omega <name> ...` to the
// handler with the untouched argv after the name; the module parses
// its own argv (core never grows a flag framework) and prints its own
// help. Deleting the module removes the registration: the subcommand
// vanishes from `omega --help` and dispatch falls back to the
// project-path behavior.
type Subcommand struct {
	Module  string
	Name    string
	Summary string // the one-line list entry `omega --help` shows
	Handler func(argv []string) error
}

// Subcommands returns the registered module subcommands in
// registration order.
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

// RefusalLedger is the default in-process ledger (Q12): it keeps the
// refusal values for hosts and tests to read. Printing, spine wiring
// and the conformance suite come with the phases that consume them.
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

// isActive reports whether a module is mounted, under the registry
// lock.
func (r *Registry) isActive(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.active[name]
}

// recordJournal lands a successfully mounted module's contributions
// in the ledger (boot path - the writes are already live).
func (r *Registry) recordJournal(j *journal) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.journals[j.module] = j
}

// adoptMount accepts a committed remount: the live record of module,
// journal and topo position.
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

// claimSlot records the one-tenant rule on a bare seam name ("store",
// "kanban" - the exclusive slots; the same key the provides sweep
// recorded, so the sweep and the registration agree on tenancy).
// The owner map holds seam names and "module.verb" pairs;
// ParseSeamName guarantees the shapes never collide.
func (r *Registry) claimSlot(seam, module string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if owner, ok := r.owner[seam]; ok && owner != module {
		return fmt.Errorf("%w: seam %q mounted by %q, claimed by %q", core.ErrSeamForeignTenant, seam, owner, module)
	}
	r.owner[seam] = module // same tenant re-claims = replace (Q7)
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
	fn     func(core.NotificationsProvider)
}

// deliverEmitters hands the mounted Notifications seam (nil when the
// module is absent - emitters stay nil-safe by the classic law) to
// every hook at mount end (boot: MountAllAPI's final deliver).
func (r *Registry) deliverEmitters(ctx *Context) {
	r.mu.Lock()
	hooks := make([]emitterReg, len(r.emitter))
	copy(hooks, r.emitter)
	r.mu.Unlock()
	for _, h := range hooks {
		h.fn(ctx.Notifications)
	}
}

// deliverModuleEmitters replays the emit-on-remount law: a remount's
// fresh hooks never saw the mount-end deliver (they registered after
// boot's sweep), so Mount delivers the named module's hooks itself.
// Only that module's entries are replayed - the others are either
// already delivered (boot) or gone (their dismount undid them). A
// rejected remount panics before this runs and plays the hooks back
// instead, so nothing is delivered half-mounted (Q11).
func (r *Registry) deliverModuleEmitters(ctx *Context, module string) {
	r.mu.Lock()
	var hooks []emitterReg
	for _, h := range r.emitter {
		if h.module == module {
			hooks = append(hooks, h)
		}
	}
	r.mu.Unlock()
	for _, h := range hooks {
		h.fn(ctx.Notifications)
	}
}

// ExtensionAPI is one capsule's namespaced view of the mount surface,
// created by the mount loop for every plugin of the build. Every
// enforcement below carries the capsule's plugin name as the tenant;
// the methods land each registration on the Context surface it owns
// - journaled so the dismount replays them backward.
type ExtensionAPI struct {
	module  string
	ctx     *core.Context
	reg     *Registry
	j       *journal // this mount's contribution journal (v0.12 E1)
	live    bool     // true = writes are already on the live Context (boot)
	builtin bool     // the capsule ships built-in vocabulary (not Toggleable)
	tools   map[string]core.Tool
	wired   bool // the one tool provider is already on ctx.ToolProviders
	// panelProviders holds the registered providers - each lands on
	// ctx.PanelProviders at commit.
	panelProviders []core.PanelProvider
}

// writeSlot records the mount's write into the journal and applies it
// to the mounting Context (boot: the live Context; remount: the
// scoped proxy). apply runs immediately; undo plays backward at
// dismount time and removes exactly the captured element
// (removeOne) - never a slice truncation.
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

// claim validates and records a namespaced machine-facing identity
// (Q10): the tenant half must be the capsule itself.
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

// RegisterTool adds one tool (ToolProviders, additive). The name is
// the model-facing identity. A module capsule (Toggleable) must carry
// its own namespace prefix (Q10: "kanban.task"); a built-in capsule
// (provider, store, skills, tools, the loop - machine-critical, in the
// engine's vocabulary) registers its natural name ("shell.run",
// "sessions.search"). Both land on the same one-tenant ledger; a
// second tenant on a held tool name is a fail-loud error (Q5), and a
// same-tenant re-registration replaces (Q7, the clean-idempotent
// remount). The payload contract is unchanged raw JSON (core.Tool.Run
// takes map[string]any, Q9).
func (api *ExtensionAPI) RegisterTool(name string, t core.Tool) error {
	if api.builtin {
		// Built-in vocabulary: the one-tenant ledger holds the name
		// itself, no namespace pairing. Journaled for release.
		if err := api.claimRecorded("tool:" + name); err != nil {
			return err
		}
	} else if err := api.claim(name); err != nil {
		return err
	}
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
	return nil
}

// toolMap is the assembled provider the registrations build; the map
// lives with the view, so later registrations merge into it.
type toolMap map[string]core.Tool

func (m toolMap) Tools() map[string]core.Tool { return m }

// RegisterRuntimeTools lands the capsule's runtime-loaded tool set
// (the mcp ruling, card t_6c0375b5): server-qualified externally
// sourced names ("proof.echo") are configuration data, not registered
// vocabulary - they never pass the namespace claim, so a server name
// outside the capsule's own namespace stays illegal to CLAIM while
// being legal to SERVE. The provider is appended whole (it re-reads
// its live set on every Tools() call, the classic bridge shape). The
// capsule claims its own namespace ONCE as the guard ("mcp." - the
// guard is journaled like every other), and the tools ride
// beneath it as runtime data. Binary purity holds: removing the module
// removes all of them regardless of naming. The one-dot law is not
// weakened anywhere - no multi-dot parse, no flattening.
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

// RegisterPanelProvider registers a panel provider: its Panels()
// computes the snapshot from its own state on every read and a
// core.PanelUpdateSink (installed by the canvas at registration)
// pushes deltas around module-face mutations. Kanban's capsule is the
// reference (phase C1). The validation: every panel carries its id,
// every action id is a namespaced pair claimed by this capsule. The
// provider lands on ctx.PanelProviders at commit - the exact append
// the classic mount makes.
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

// commit appends the module's registered panel providers to
// ctx.PanelProviders - the exact slice the classic mount appends to.
// Called by the mount loop after MountWithAPI returns; validation
// failures abort the mount (fail loud, Q5).
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

// RegisterRoute registers one module REST route (the phase-D vocabulary;
// validated and recorded, nothing serves it in phase B). The pattern
// is an http.ServeMux pattern; the registry rejects a cross-tenant
// claim of the same pattern and replaces a same-tenant one.
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
			api.reg.routes[i].Handler = h // same tenant re-registers = replace (Q7)
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

// RegisterSubcommand registers the capsule's CLI subcommand (v0.12
// phase D, Q6 - module owns its argv): a thin name+handler prefix. The
// binary consults the registry's cli table when a first non-flag
// argument matches no built-in subcommand; the registration carries
// the name (user-facing identity, natural shape like slash commands -
// no namespace pair), the one-line summary the global help list shows,
// and the handler that parses its own argv. The claim is
// one-name-one-tenant: a cross-tenant claim of the same subcommand
// name fails the boot loud (Q5), same-tenant re-registration replaces
// (Q7).
func (api *ExtensionAPI) RegisterSubcommand(name, summary string, handler func(argv []string) error) error {
	if name == "" || name != strings.TrimSpace(name) {
		return fmt.Errorf("subcommand %q: a registration carries its bare-word name", name)
	}
	if strings.HasPrefix(name, "-") || strings.HasPrefix(name, "/") {
		return fmt.Errorf("subcommand %q: a CLI subcommand is a bare word - flags are the module's own argv, a leading / names the TUI seam", name)
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
			api.reg.subcmds[i].Summary = summary // same tenant re-registers = replace (Q7)
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

// RegisterEmitter registers the capsule's finished-things hook: at
// mount end the mounted Notifications seam is handed to it (nil when
// the notifications module is absent - emitters stay nil-safe). The
// the hook is the registration-shape
// shape (phase C).
func (api *ExtensionAPI) RegisterEmitter(fn func(n core.NotificationsProvider)) error {
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

// sameFunc compares two func values by code pointer - the identity
// the emitter undo matches on. Same function, same hook.
func sameFunc(a, b any) bool {
	return reflect.ValueOf(a).Pointer() == reflect.ValueOf(b).Pointer()
}

// RegisterRenderer maps the TUI ownership seam (phase E mounts
// renderers as hosts): the provider appends to ctx.TUIProviders, the
// same slice - and the same law (a plugin left out of the build never
// renders) - the classic mount appends to.
func (api *ExtensionAPI) RegisterRenderer(r core.TUIProvider) error {
	if r == nil {
		return fmt.Errorf("renderer %q: a registration carries its provider", api.module)
	}
	api.writeSlot(
		func(c *core.Context) { c.TUIProviders = append(c.TUIProviders, r) },
		func(c *core.Context) { c.TUIProviders = removeOne(c.TUIProviders, r) },
	)
	return nil
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

// RegisterCommand maps the command seam: the same append onto
// ctx.Commands and the same chain-on-miss CommandHandler wrapper the
// classic modules build (byte-equal shape - parity holds). The
// command name keeps its slash (user-facing identity); the registry
// records the owner and rejects a cross-tenant claim (Q5).
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
				if err == nil {
					return out, nil
				}
				if inner != nil {
					return inner(ctx, n, a)
				}
				return out, err
			}
		},
		func(c *core.Context) { c.CommandHandler = prev },
	)
	return nil
}

// RegisterDelegation maps the delegation pipe (both fields of the
// classic delegate seam in one registration).
func (api *ExtensionAPI) RegisterDelegation(injected <-chan core.InjectedMessage, pending func() int) error {
	prevInj := api.ctx.InjectedMessages
	api.writeSlot(
		func(c *core.Context) { c.InjectedMessages = injected },
		func(c *core.Context) { c.InjectedMessages = prevInj },
	)
	prevPending := api.ctx.PendingDelegations
	api.writeSlot(
		func(c *core.Context) { c.PendingDelegations = pending },
		func(c *core.Context) { c.PendingDelegations = prevPending },
	)
	return nil
}

// RegisterLifecycle records one Start/Stop fence handle for the
// capsule (the goroutine fence, v0.12 E1): Mount starts nothing - the
// handle waits for the host's StartAll after the commit. The
// registration journals the fence entry, so a dismount stops and
// unregisters it and a rejected remount leaves the live run-group
// untouched (the scoped proxy holds its own group).
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

// mountSeam maps one exclusive slot: the one-tenant claim at
// registration time (Q5, journaled for release), then the same field
// assignment the classic mount writes - journaled with the previous
// value so the dismount restores exactly what it found.
func (api *ExtensionAPI) mountSeam(seam string, apply func(*core.Context), undo func(*core.Context)) error {
	if err := api.claimRecorded(seam); err != nil {
		return err
	}
	api.writeSlot(apply, undo)
	return nil
}

// RegisterProvider maps the provider seam.
func (api *ExtensionAPI) RegisterProvider(p core.Provider) error {
	if p == nil {
		return fmt.Errorf("provider %q: a registration carries its provider", api.module)
	}
	prev := api.ctx.Provider
	return api.mountSeam("provider",
		func(c *core.Context) { c.Provider = p },
		func(c *core.Context) { c.Provider = prev })
}

// RegisterCompactor maps the compactor seam.
func (api *ExtensionAPI) RegisterCompactor(cp core.CompactionProvider) error {
	if cp == nil {
		return fmt.Errorf("compactor %q: a registration carries its compactor", api.module)
	}
	prev := api.ctx.Compactor
	return api.mountSeam("compactor",
		func(c *core.Context) { c.Compactor = cp },
		func(c *core.Context) { c.Compactor = prev })
}

// RegisterStore maps the store seam.
func (api *ExtensionAPI) RegisterStore(s core.StoreProvider) error {
	if s == nil {
		return fmt.Errorf("store %q: a registration carries its store", api.module)
	}
	prev := api.ctx.Store
	return api.mountSeam("store",
		func(c *core.Context) { c.Store = s },
		func(c *core.Context) { c.Store = prev })
}

// RegisterSkills maps the skills seam.
func (api *ExtensionAPI) RegisterSkills(s core.SkillsProvider) error {
	if s == nil {
		return fmt.Errorf("skills %q: a registration carries its provider", api.module)
	}
	prev := api.ctx.Skills
	return api.mountSeam("skills",
		func(c *core.Context) { c.Skills = s },
		func(c *core.Context) { c.Skills = prev })
}

// RegisterLoop maps the loop seam.
func (api *ExtensionAPI) RegisterLoop(l core.LoopProvider) error {
	if l == nil {
		return fmt.Errorf("loop %q: a registration carries its loop", api.module)
	}
	prev := api.ctx.Loop
	return api.mountSeam("loop",
		func(c *core.Context) { c.Loop = l },
		func(c *core.Context) { c.Loop = prev })
}

// RegisterLogger maps the logging seam.
func (api *ExtensionAPI) RegisterLogger(l core.LoggerProvider) error {
	if l == nil {
		return fmt.Errorf("logging %q: a registration carries its logger", api.module)
	}
	prev := api.ctx.Logger
	return api.mountSeam("logging",
		func(c *core.Context) { c.Logger = l },
		func(c *core.Context) { c.Logger = prev })
}

// RegisterMemory maps the memory seam.
func (api *ExtensionAPI) RegisterMemory(m core.MemoryProvider) error {
	if m == nil {
		return fmt.Errorf("memory %q: a registration carries its provider", api.module)
	}
	prev := api.ctx.Memory
	return api.mountSeam("memory",
		func(c *core.Context) { c.Memory = m },
		func(c *core.Context) { c.Memory = prev })
}

// RegisterPromptBuilder maps the prompt_builder seam.
func (api *ExtensionAPI) RegisterPromptBuilder(p core.PromptBuilder) error {
	if p == nil {
		return fmt.Errorf("prompt_builder %q: a registration carries its builder", api.module)
	}
	prev := api.ctx.PromptBuilder
	return api.mountSeam("prompt_builder",
		func(c *core.Context) { c.PromptBuilder = p },
		func(c *core.Context) { c.PromptBuilder = prev })
}

// RegisterFrontend maps the frontend seam.
func (api *ExtensionAPI) RegisterFrontend(f core.Frontend) error {
	if f == nil {
		return fmt.Errorf("frontend %q: a registration carries its frontend", api.module)
	}
	prev := api.ctx.Frontend
	return api.mountSeam("frontend",
		func(c *core.Context) { c.Frontend = f },
		func(c *core.Context) { c.Frontend = prev })
}

// RegisterTrust maps the trust seam.
func (api *ExtensionAPI) RegisterTrust(t core.TrustProvider) error {
	if t == nil {
		return fmt.Errorf("trust %q: a registration carries its provider", api.module)
	}
	prev := api.ctx.Trust
	return api.mountSeam("trust",
		func(c *core.Context) { c.Trust = t },
		func(c *core.Context) { c.Trust = prev })
}

// RegisterCron maps the cron seam.
func (api *ExtensionAPI) RegisterCron(cr core.CronProvider) error {
	if cr == nil {
		return fmt.Errorf("cron %q: a registration carries its provider", api.module)
	}
	prev := api.ctx.Cron
	return api.mountSeam("cron",
		func(c *core.Context) { c.Cron = cr },
		func(c *core.Context) { c.Cron = prev })
}

// RegisterNotifications maps the notifications seam.
func (api *ExtensionAPI) RegisterNotifications(n core.NotificationsProvider) error {
	if n == nil {
		return fmt.Errorf("notifications %q: a registration carries its provider", api.module)
	}
	prev := api.ctx.Notifications
	return api.mountSeam("notifications",
		func(c *core.Context) { c.Notifications = n },
		func(c *core.Context) { c.Notifications = prev })
}

// RegisterVerdicts maps the verdicts seam.
func (api *ExtensionAPI) RegisterVerdicts(v core.VerdictsProvider) error {
	if v == nil {
		return fmt.Errorf("verdicts %q: a registration carries its provider", api.module)
	}
	prev := api.ctx.Verdicts
	return api.mountSeam("verdicts",
		func(c *core.Context) { c.Verdicts = v },
		func(c *core.Context) { c.Verdicts = prev })
}
