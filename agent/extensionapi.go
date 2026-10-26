// The universal mount surface (v0.12 phase B, the dual-mount phase).
// One typed registration vocabulary in the pi style: a capsule calls
// api.Register*(...) instead of assigning Context slots directly. Both
// mount styles live simultaneously this phase - classic capsules keep
// assigning seams in Mount, zero behavior change - and the adapters
// below make both styles converge on the same Context, so the parity
// test mounts one capsule both ways and demands identical behavior.
// Migrating the capsules themselves is phase C.
//
// Adaptation shape: every registration lands on the exact field the
// classic seam assignment writes (same slice, same append, same
// chain-on-miss command wrapper). An APIPlugin mounting is therefore
// behaviorally indistinguishable from the same capsule's classic
// Mount writing the same fields by hand.
//
// THE SEAM CENSUS (23 surfaces on the post-B0 tree; the adapters map
// every one of them to a registration):
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
//	 Kanban                        RegisterKanban
//	 Notifications                 RegisterNotifications
//	 Verdicts                      RegisterVerdicts
//	 Captioner                     RegisterCaptioner (host-built after
//	                               MountAll today; carried for census
//	                               completeness)
//
//	Additive surfaces (8)          Registration
//	 ToolProviders                 RegisterTool
//	 PromptSectioners              RegisterPromptSectioner
//	 TUIProviders                  RegisterRenderer
//	RegisterPanel / RegisterAction   (declarative: static descriptors
//	                                + checked action-pipe handlers)
//	RegisterPanelProvider            (live: a stateful core.PanelProvider
//	                                - Panels() computed per snapshot, a
//	                                core.PanelUpdateSink pushing through
//	                                mutations; kanban's capsule is the
//	                                reference)
//	 Channels                      RegisterChannel
//	 Commands + CommandHandler     RegisterCommand
//	 CLI subcommands (phase D)     RegisterSubcommand
//	 InjectedMessages              RegisterDelegation
//	 PendingDelegations            RegisterDelegation
//
// Two families have no classic seam and carry no adapter: routes
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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/EndoTheDev/omega/core"
)

// APIPlugin is the optional capability MountAll dispatches on (the
// same pattern as Describer). A plugin implementing it mounts through
// the pi-style registration surface instead of the classic Mount -
// both methods may exist on the migration path; the API one wins
// (strangler: adapters first, the classic body retires in phase F).
// Plugins implementing only Plugin mount exactly as before.
type APIPlugin interface {
	// MountWithAPI mounts the plugin through its registrations.
	MountWithAPI(ctx *Context, api *ExtensionAPI) error
}

// MountAll keeps its classic signature: every existing call site and
// test mounts unchanged. Hosts that consume registrations (module
// routes, the refusal ledger) call MountAllAPI instead.
func MountAll(plugins []Plugin, ctx *Context) error {
	_, err := MountAllAPI(plugins, ctx, nil)
	return err
}

// MountAllAPI mounts the plugin set like MountAll and returns the
// registration registry: claims, module routes and the refusal
// ledger. The refusals sink is host-supplied; nil builds the default
// in-process refusal ledger.
func MountAllAPI(plugins []Plugin, ctx *Context, refusals core.RefusalSink) (*Registry, error) {
	if refusals == nil {
		refusals = NewRefusalLedger()
	}
	reg := newRegistry(refusals)
	provides, err := buildProvidesMap(plugins, reg)
	if err != nil {
		return nil, err
	}
	ordered, err := topoSort(plugins, provides)
	if err != nil {
		return nil, err
	}
	if err := mountPlugins(ordered, reg, ctx); err != nil {
		return nil, err
	}
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
	routes   []Route      // module REST, consumed in phase D
	subcmds  []Subcommand // module CLI subcommands, dispatched by the omega binary
	refusals core.RefusalSink
}

// Route is one module-registered REST route. Nothing serves it in
// phase B (zero behavior change); the http channel reads these off
// the registry when the route seam migrates (phase D).
type Route struct {
	Module  string
	Pattern string // an http.ServeMux pattern ("GET /x/{id}")
	Handler func(http.ResponseWriter, *http.Request)
}

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
	return &Registry{owner: make(map[string]string), refusals: refusals}
}

// claimSlot records the one-tenant rule on a bare seam name ("store",
// "kanban" - the exclusive slots; the same key the provides sweep
// recorded for classic capsules, so classic and API styles conflict
// together). The owner map holds seam names and "module.verb" pairs;
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
// checked - it never errors, it only lands the tenant.
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
// every hook at mount end.
func (r *Registry) deliverEmitters(ctx *Context) {
	r.mu.Lock()
	hooks := make([]emitterReg, len(r.emitter))
	copy(hooks, r.emitter)
	r.mu.Unlock()
	for _, h := range hooks {
		h.fn(ctx.Notifications)
	}
}

// ExtensionAPI is one capsule's namespaced view of the mount surface,
// created by MountAll for a plugin that implements APIPlugin. Every
// enforcement below carries the capsule's plugin name as the tenant;
// the methods adapt each registration onto the exact Context field
// the classic mount would assign.
type ExtensionAPI struct {
	module string
	ctx    *Context
	reg    *Registry
	tools  map[string]core.Tool
	wired  bool // the one tool provider is already on ctx.ToolProviders
	panels map[string]core.Panel
	order  []string
	acts   map[string]registeredAction
	// panelProviders holds the live registered providers (the dynamic
	// capsule shape) - commit lands them on ctx.PanelProviders.
	panelProviders []core.PanelProvider
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
	return nil
}

// RegisterTool adds one tool (ToolProviders, additive). The name is
// the model-facing identity and carries the module prefix (Q10) -
// existing classic tool names migrate with the capsules (phase C);
// the payload contract is unchanged raw JSON (core.Tool.Run takes
// map[string]any, Q9).
func (api *ExtensionAPI) RegisterTool(name string, t core.Tool) error {
	if err := api.claim(name); err != nil {
		return err
	}
	if api.tools == nil {
		api.tools = make(map[string]core.Tool)
	}
	api.tools[name] = t
	if !api.wired {
		api.wired = true
		api.ctx.ToolProviders = append(api.ctx.ToolProviders, toolMap(api.tools))
	}
	return nil
}

// toolMap is the assembled provider the registrations build; the map
// lives with the view, so later registrations merge into it.
type toolMap map[string]core.Tool

func (m toolMap) Tools() map[string]core.Tool { return m }

// RegisterPanel declares one panel descriptor (PanelProviders,
// additive). The api path assembles one panel provider per module at
// commit - the shape the canvas registers must be single per module.
// Actions are stamped with the capsule's namespace: a descriptor
// carrying another module's name (or an unprefixed action id) is a
// fail-loud error.
func (api *ExtensionAPI) RegisterPanel(p core.Panel) error {
	if p.ID == "" {
		return fmt.Errorf("panel %q: a registered panel carries its id", api.module)
	}
	if api.panels == nil {
		api.panels = make(map[string]core.Panel)
	}
	if _, dup := api.panels[p.ID]; !dup {
		api.order = append(api.order, p.ID)
	}
	stamped := make([]core.PanelAction, len(p.Actions))
	for i, a := range p.Actions {
		if a.Module == "" {
			a.Module = api.module
		}
		if a.Module != api.module {
			return fmt.Errorf("panel %q action %q claims module %q - the api mounts %q only", p.ID, a.ID, a.Module, api.module)
		}
		if _, _, err := core.ParseSeamName(a.ID); err != nil {
			return err
		}
		stamped[i] = a
	}
	p.Actions = stamped
	api.panels[p.ID] = p
	return nil
}

// registeredAction is one action-pipe registration: the handler plus
// its optional boundary ShapeCheck (Q9), keyed by the namespaced
// action id.
type registeredAction struct {
	check   core.ShapeCheck
	handler func(v any) (any, error)
}

// RegisterAction binds an action-pipe handler, optionally with its
// boundary ShapeCheck: the proof seam of the payload contract (Q9).
// The handler receives the decoded raw JSON; a failing check refusal
// (Q12, the handler never fires). Registration resolves against the
// declared panels at commit - an action id declared on no panel is a
// fail-loud error.
func (api *ExtensionAPI) RegisterAction(id string, check core.ShapeCheck, handler func(v any) (any, error)) error {
	if err := api.claim(id); err != nil {
		return err
	}
	if handler == nil {
		return fmt.Errorf("action %q: a registration carries its handler", id)
	}
	if api.acts == nil {
		api.acts = make(map[string]registeredAction)
	}
	api.acts[id] = registeredAction{check: check, handler: handler}
	return nil
}

// RegisterPanelProvider registers a live panel provider - the dynamic
// capsule shape the declarative registrations cannot express: its
// Panels() computes the snapshot from its own state on every read and
// a core.PanelUpdateSink (installed by the canvas at registration)
// pushes deltas around module-face mutations. Kanban's capsule is the
// reference (phase C1, the migration that surfaced this gap). The
// validation is the declarative path's, applied to the provider's
// current declaration: every panel carries its id, every action id is
// a namespaced pair claimed by this capsule. The real provider lands
// on ctx.PanelProviders at commit - the exact append the classic
// mount makes.
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

// commit assembles the module's panel surface and appends it to
// ctx.PanelProviders - the exact slice the classic mount appends to:
// the declarative registrations assemble one provider, the live
// registered providers land as themselves. Called by the mount loop
// after MountWithAPI returns; validation failures abort the mount
// (fail loud, Q5).
func (api *ExtensionAPI) commit() error {
	if len(api.panels) > 0 {
		for id := range api.acts {
			declared := false
			for _, panel := range api.panels {
				for _, pa := range panel.Actions {
					if pa.ID == id {
						declared = true
					}
				}
			}
			if !declared {
				return fmt.Errorf("action %q is declared on no panel of %q - register it on the panel descriptor", id, api.module)
			}
		}
		provider := &apiPanels{
			module: api.module,
			reg:    api.reg,
			panels: api.panels,
			order:  api.order,
			acts:   api.acts,
		}
		api.ctx.PanelProviders = append(api.ctx.PanelProviders, provider)
	}
	for _, p := range api.panelProviders {
		api.ctx.PanelProviders = append(api.ctx.PanelProviders, p)
	}
	return nil
}

// apiPanels is the assembled provider of the declarative panel
// registrations - the action pipe's boundary. Every call crossing it
// decodes raw JSON and runs the registered ShapeCheck before the
// handler; a failure is refused (Q12) and the handler never sees the
// payload.
type apiPanels struct {
	module string
	reg    *Registry
	panels map[string]core.Panel
	order  []string
	acts   map[string]registeredAction
}

// Panels implements core.PanelProvider.
func (p *apiPanels) Panels() []core.Panel {
	out := make([]core.Panel, 0, len(p.order))
	for _, id := range p.order {
		out = append(out, p.panels[id])
	}
	return out
}

// HandleAction implements core.PanelProvider: the one seam with a
// working boundary ShapeCheck in phase B (SKETCH 3's first step).
func (p *apiPanels) HandleAction(call core.ActionCall) (any, error) {
	entry, ok := p.acts[call.Action]
	if !ok {
		// The canvas route-checks before Dispatch; this defense names
		// the shape for direct callers.
		return nil, fmt.Errorf("%w: %q", core.ErrUnknownAction, call.Action)
	}
	var v any
	if msg := bytes.TrimSpace(call.Args); len(msg) > 0 {
		if err := json.Unmarshal(msg, &v); err != nil {
			p.refuse(call, err)
			return nil, fmt.Errorf("action %q: args are not valid JSON: %w", call.Action, err)
		}
	}
	if entry.check != nil {
		if err := entry.check(v); err != nil {
			p.refuse(call, err)
			return nil, fmt.Errorf("action %q: refused by %s: %w", call.Action, call.Module, err)
		}
	}
	return entry.handler(v)
}

// refuse records the rejection in the ledger (Q12): module, seam,
// failed predicate (the action id carries the check) and the error.
func (p *apiPanels) refuse(call core.ActionCall, err error) {
	if p.reg == nil || p.reg.refusals == nil {
		return
	}
	p.reg.refusals.Log(core.Refusal{
		Module:    call.Module,
		Seam:      "panels",
		Predicate: call.Action,
		Err:       err,
		At:        time.Now(),
	})
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
	api.reg.mu.Lock()
	defer api.reg.mu.Unlock()
	if owner, ok := api.reg.owner["route:"+pattern]; ok && owner != api.module {
		return fmt.Errorf("%w: route %q mounted by %q", core.ErrSeamForeignTenant, pattern, owner)
	}
	api.reg.owner["route:"+pattern] = api.module
	for i, r := range api.reg.routes {
		if r.Pattern == pattern {
			api.reg.routes[i].Handler = h // same tenant re-registers = replace (Q7)
			return nil
		}
	}
	api.reg.routes = append(api.reg.routes, Route{Module: api.module, Pattern: pattern, Handler: h})
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
	if err := api.reg.claimSlot("cli:"+name, api.module); err != nil {
		return err
	}
	api.reg.mu.Lock()
	defer api.reg.mu.Unlock()
	for i, s := range api.reg.subcmds {
		if s.Name == name {
			api.reg.subcmds[i].Summary = summary // same tenant re-registers = replace (Q7)
			api.reg.subcmds[i].Handler = handler
			return nil
		}
	}
	api.reg.subcmds = append(api.reg.subcmds, Subcommand{Module: api.module, Name: name, Summary: summary, Handler: handler})
	return nil
}

// RegisterEmitter registers the capsule's finished-things hook: at
// mount end the mounted Notifications seam is handed to it (nil when
// the notifications module is absent - emitters stay nil-safe). The
// classic direct capture keeps working; the hook is the migrated
// shape (phase C).
func (api *ExtensionAPI) RegisterEmitter(fn func(n core.NotificationsProvider)) error {
	if fn == nil {
		return fmt.Errorf("emitter %q: a registration carries its hook", api.module)
	}
	api.reg.mu.Lock()
	api.reg.emitter = append(api.reg.emitter, emitterReg{module: api.module, fn: fn})
	api.reg.mu.Unlock()
	return nil
}

// RegisterRenderer maps the TUI ownership seam (phase E mounts
// renderers as hosts): the provider appends to ctx.TUIProviders, the
// same slice - and the same law (a plugin left out of the build never
// renders) - the classic mount appends to.
func (api *ExtensionAPI) RegisterRenderer(r core.TUIProvider) error {
	if r == nil {
		return fmt.Errorf("renderer %q: a registration carries its provider", api.module)
	}
	api.ctx.TUIProviders = append(api.ctx.TUIProviders, r)
	return nil
}

// RegisterPromptSectioner maps the additive prompt-section seam.
func (api *ExtensionAPI) RegisterPromptSectioner(p core.PromptSectioner) error {
	if p == nil {
		return fmt.Errorf("prompt sectioner %q: a registration carries its provider", api.module)
	}
	api.ctx.PromptSectioners = append(api.ctx.PromptSectioners, p)
	return nil
}

// RegisterChannel maps the additive channel seam.
func (api *ExtensionAPI) RegisterChannel(c core.Channel) error {
	if c == nil {
		return fmt.Errorf("channel %q: a registration carries its channel", api.module)
	}
	api.ctx.Channels = append(api.ctx.Channels, c)
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
	if err := api.reg.claimSlot(strings.TrimPrefix(name, "/"), api.module); err != nil {
		return err
	}
	api.ctx.Commands = append(api.ctx.Commands, core.ExtensionCommand{Name: name, Description: description, Args: args})
	prev := api.ctx.CommandHandler
	api.ctx.CommandHandler = func(c context.Context, n, a string) (core.CommandResult, error) {
		out, err := h(c, n, a)
		if err == nil {
			return out, nil
		}
		if prev != nil {
			return prev(c, n, a)
		}
		return out, err
	}
	return nil
}

// RegisterDelegation maps the delegation pipe (both fields of the
// classic delegate seam in one registration).
func (api *ExtensionAPI) RegisterDelegation(injected <-chan core.InjectedMessage, pending func() int) error {
	api.ctx.InjectedMessages = injected
	api.ctx.PendingDelegations = pending
	return nil
}

// mountSeam maps one exclusive slot: the one-tenant claim at
// registration time (Q5) and then the same field assignment the
// classic mount writes.
func (api *ExtensionAPI) mountSeam(seam string, apply func()) error {
	if err := api.reg.claimSlot(seam, api.module); err != nil {
		return err
	}
	apply()
	return nil
}

// RegisterProvider maps the provider seam.
func (api *ExtensionAPI) RegisterProvider(p core.Provider) error {
	if p == nil {
		return fmt.Errorf("provider %q: a registration carries its provider", api.module)
	}
	return api.mountSeam("provider", func() { api.ctx.Provider = p })
}

// RegisterCompactor maps the compactor seam.
func (api *ExtensionAPI) RegisterCompactor(c core.CompactionProvider) error {
	if c == nil {
		return fmt.Errorf("compactor %q: a registration carries its compactor", api.module)
	}
	return api.mountSeam("compactor", func() { api.ctx.Compactor = c })
}

// RegisterStore maps the store seam.
func (api *ExtensionAPI) RegisterStore(s core.StoreProvider) error {
	if s == nil {
		return fmt.Errorf("store %q: a registration carries its store", api.module)
	}
	return api.mountSeam("store", func() { api.ctx.Store = s })
}

// RegisterSkills maps the skills seam.
func (api *ExtensionAPI) RegisterSkills(s core.SkillsProvider) error {
	if s == nil {
		return fmt.Errorf("skills %q: a registration carries its provider", api.module)
	}
	return api.mountSeam("skills", func() { api.ctx.Skills = s })
}

// RegisterLoop maps the loop seam.
func (api *ExtensionAPI) RegisterLoop(l core.LoopProvider) error {
	if l == nil {
		return fmt.Errorf("loop %q: a registration carries its loop", api.module)
	}
	return api.mountSeam("loop", func() { api.ctx.Loop = l })
}

// RegisterLogger maps the logging seam.
func (api *ExtensionAPI) RegisterLogger(l core.LoggerProvider) error {
	if l == nil {
		return fmt.Errorf("logging %q: a registration carries its logger", api.module)
	}
	return api.mountSeam("logging", func() { api.ctx.Logger = l })
}

// RegisterMemory maps the memory seam.
func (api *ExtensionAPI) RegisterMemory(m core.MemoryProvider) error {
	if m == nil {
		return fmt.Errorf("memory %q: a registration carries its provider", api.module)
	}
	return api.mountSeam("memory", func() { api.ctx.Memory = m })
}

// RegisterPromptBuilder maps the prompt_builder seam.
func (api *ExtensionAPI) RegisterPromptBuilder(p core.PromptBuilder) error {
	if p == nil {
		return fmt.Errorf("prompt_builder %q: a registration carries its builder", api.module)
	}
	return api.mountSeam("prompt_builder", func() { api.ctx.PromptBuilder = p })
}

// RegisterFrontend maps the frontend seam.
func (api *ExtensionAPI) RegisterFrontend(f core.Frontend) error {
	if f == nil {
		return fmt.Errorf("frontend %q: a registration carries its frontend", api.module)
	}
	return api.mountSeam("frontend", func() { api.ctx.Frontend = f })
}

// RegisterTrust maps the trust seam.
func (api *ExtensionAPI) RegisterTrust(t core.TrustProvider) error {
	if t == nil {
		return fmt.Errorf("trust %q: a registration carries its provider", api.module)
	}
	return api.mountSeam("trust", func() { api.ctx.Trust = t })
}

// RegisterCron maps the cron seam.
func (api *ExtensionAPI) RegisterCron(c core.CronProvider) error {
	if c == nil {
		return fmt.Errorf("cron %q: a registration carries its provider", api.module)
	}
	return api.mountSeam("cron", func() { api.ctx.Cron = c })
}

// RegisterKanban maps the kanban seam.
func (api *ExtensionAPI) RegisterKanban(k core.KanbanProvider) error {
	if k == nil {
		return fmt.Errorf("kanban %q: a registration carries its provider", api.module)
	}
	return api.mountSeam("kanban", func() { api.ctx.Kanban = k })
}

// RegisterNotifications maps the notifications seam.
func (api *ExtensionAPI) RegisterNotifications(n core.NotificationsProvider) error {
	if n == nil {
		return fmt.Errorf("notifications %q: a registration carries its provider", api.module)
	}
	return api.mountSeam("notifications", func() { api.ctx.Notifications = n })
}

// RegisterVerdicts maps the verdicts seam.
func (api *ExtensionAPI) RegisterVerdicts(v core.VerdictsProvider) error {
	if v == nil {
		return fmt.Errorf("verdicts %q: a registration carries its provider", api.module)
	}
	return api.mountSeam("verdicts", func() { api.ctx.Verdicts = v })
}

// RegisterCaptioner maps the captioner seam (host-built after
// MountAll today; carried so the census has no gaps).
func (api *ExtensionAPI) RegisterCaptioner(c core.Captioner) error {
	if c == nil {
		return fmt.Errorf("captioner %q: a registration carries its captioner", api.module)
	}
	return api.mountSeam("captioner", func() { api.ctx.Captioner = c })
}

// mountPlugins mounts each plugin in order, dispatching APIPlugin to
// the registration surface and Plugin to the classic Mount. Both land
// on the same Context fields, so the inventory counting below reads
// either style the same way.
func mountPlugins(ordered []Plugin, reg *Registry, ctx *Context) error {
	for _, p := range ordered {
		beforeTools := len(ctx.ToolProviders)
		beforeCmds := len(ctx.Commands)
		if ap, ok := p.(APIPlugin); ok {
			api := &ExtensionAPI{module: p.Name(), ctx: ctx, reg: reg}
			if err := ap.MountWithAPI(ctx, api); err != nil {
				return fmt.Errorf("mount %q: %w", p.Name(), err)
			}
			if err := api.commit(); err != nil {
				return fmt.Errorf("mount %q: %w", p.Name(), err)
			}
		} else if err := p.Mount(ctx); err != nil {
			return fmt.Errorf("mount %q: %w", p.Name(), err)
		}
		info := core.ExtensionInfo{
			Name:  p.Name(),
			Seams: p.Provides(),
		}
		if d, ok := p.(Describer); ok {
			info.Description = d.Description()
		}
		// Count tools this plugin added.
		for _, tp := range ctx.ToolProviders[beforeTools:] {
			if tp == nil {
				continue
			}
			for name, t := range tp.Tools() {
				// Alias entries are model-facing compat shims, not inventory:
				// they stay callable while hidden from the display counts.
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
		// Ensure deterministic order: map iteration is non-deterministic in Go.
		sort.SliceStable(info.ToolList, func(i, j int) bool { return info.ToolList[i].Name < info.ToolList[j].Name })
		// Count commands this plugin added.
		info.Commands = len(ctx.Commands) - beforeCmds
		ctx.Infos = append(ctx.Infos, info)
	}
	return nil
}
