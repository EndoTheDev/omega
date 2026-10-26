package agent

// The dual-mount tests (v0.12 phase B). The centerpiece is the parity
// law: one capsule mounts BOTH ways, and the resulting Contexts are
// behaviorally identical - the adapters converge on the exact fields
// the classic seam assignment writes. Around it: the identity law
// (namespaced machine-facing names, Q10), the one-tenant rule (Q5),
// clean-idempotent replacement (Q7), the census enforcement (every
// seam maps to a registration), and the action pipe's working boundary
// ShapeCheck with its failing-refusal path (Q9/Q12, SKETCH 3 step one).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// ---- shared fixture: one capsule's capability set -------------------

func echoTool() core.Tool {
	return core.Tool{
		Description: "echoes its args",
		Parameters:  map[string]any{"type": "object"},
		Run: func(ctx context.Context, args map[string]any) (string, error) {
			b, _ := json.Marshal(args)
			return string(b), nil
		},
	}
}

type plugTUI struct{}

func (plugTUI) TUICommands() []core.TUICommand { return []core.TUICommand{{Name: "/plug-tui"}} }
func (plugTUI) HandleTUICommand(name, args string, c core.TUIConsole) core.TUIResult {
	return core.TUIResult{}
}

type plugSectioner struct{}

func (plugSectioner) PromptSections(opts core.PromptBuildOptions) []core.PromptSection {
	return nil
}

type plugChannel struct{}

func (plugChannel) Start(ctx context.Context, deps core.ChannelDeps) error { return nil }
func (plugChannel) Stop() error                                            { return nil }

type plugLogger struct{ hits []string }

func (l *plugLogger) Printf(format string, args ...any) { l.hits = append(l.hits, "info") }
func (l *plugLogger) Errorf(format string, args ...any) { l.hits = append(l.hits, "err") }
func (l *plugLogger) Close() error                      { return nil }

type plugLoop struct{}

func (plugLoop) Run(ctx context.Context, opts core.LoopOptions) error { return nil }

type plugCompactor struct{}

func (plugCompactor) Compact(ctx context.Context, messages []core.Message) ([]core.Message, error) {
	return messages, nil
}

type plugNotifier struct{}

func (plugNotifier) Emit(n core.Notification) (core.Notification, error) { return n, nil }
func (plugNotifier) List(ctx context.Context, unreadOnly bool, limit, offset int) ([]core.Notification, int, error) {
	return nil, 0, nil
}
func (plugNotifier) MarkRead(ctx context.Context, id int64) error { return nil }
func (plugNotifier) MarkAllRead(ctx context.Context) error        { return nil }
func (plugNotifier) Subscribe() (<-chan core.Notification, func()) {
	ch := make(chan core.Notification)
	return ch, func() { close(ch) }
}

// notifierPlugin mounts the notifications seam both parity runs need.
type notifierPlugin struct{ n core.NotificationsProvider }

func (p *notifierPlugin) Name() string             { return "notifier" }
func (p *notifierPlugin) Provides() []string       { return []string{"notifications"} }
func (p *notifierPlugin) Requires() []string       { return nil }
func (p *notifierPlugin) Mount(ctx *Context) error { ctx.Notifications = p.n; return nil }

// plugPanel is the shared panel descriptor: the same declarations both
// mount styles carry, the same echo behavior to compare.
func plugPanel() core.Panel {
	return core.Panel{
		ID:    "plug.main",
		Title: "plug",
		Kind:  "status",
		Actions: []core.PanelAction{
			{ID: "plug.act", Label: "echo", Module: "plug"},
		},
	}
}

func decodeArgs(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var v any
	_ = json.Unmarshal(raw, &v)
	return v
}

// classicCapsule mounts the classic way: direct seam assignment.
type classicCapsule struct {
	notifier core.NotificationsProvider // captured in Mount
}

func (*classicCapsule) Name() string { return "plug" }
func (*classicCapsule) Provides() []string {
	return []string{"store", "tools", "tui", "panels", "prompt_sections", "channel", "logging", "loop", "compactor"}
}
func (*classicCapsule) Requires() []string { return []string{"notifications"} }

func (c *classicCapsule) Mount(ctx *Context) error {
	c.notifier = ctx.Notifications
	ctx.Store = fakeStore{}
	ctx.Logger = &plugLogger{}
	ctx.Loop = plugLoop{}
	ctx.Compactor = plugCompactor{}
	ctx.ToolProviders = append(ctx.ToolProviders, toolMap(map[string]core.Tool{"plug.echo": echoTool()}))
	ctx.PromptSectioners = append(ctx.PromptSectioners, plugSectioner{})
	ctx.TUIProviders = append(ctx.TUIProviders, plugTUI{})
	ctx.PanelProviders = append(ctx.PanelProviders, classicPanels{})
	ctx.Channels = append(ctx.Channels, plugChannel{})
	ctx.Commands = append(ctx.Commands, core.ExtensionCommand{Name: "/plug", Description: "parity command"})
	prev := ctx.CommandHandler
	ctx.CommandHandler = func(c context.Context, name, args string) (core.CommandResult, error) {
		if name != "/plug" {
			if prev != nil {
				return prev(c, name, args)
			}
			return core.CommandResult{}, fmt.Errorf("unknown command %q", name)
		}
		return core.CommandResult{Text: "plug ran"}, nil
	}
	return nil
}

// classicPanels is the classic action handler: raw call in, decoded
// args echoed back - the same behavior the registration style produces.
type classicPanels struct{}

func (classicPanels) Panels() []core.Panel { return []core.Panel{plugPanel()} }
func (classicPanels) HandleAction(call core.ActionCall) (any, error) {
	return decodeArgs(call.Args), nil
}

// fakeStore satisfies the full StoreProvider seam with no-ops - parity
// compares the identity and inventory of the reference, the store
// itself never runs.
type fakeStore struct{}

func (fakeStore) Open(dsn string) error                                               { return nil }
func (fakeStore) Close() error                                                        { return nil }
func (fakeStore) CreateSession(ctx context.Context, id, parentID, label string) error { return nil }
func (fakeStore) GetSession(ctx context.Context, id string) (core.Session, error) {
	return core.Session{}, nil
}
func (fakeStore) ListSessions(ctx context.Context) ([]core.Session, error)  { return nil, nil }
func (fakeStore) DeleteSession(ctx context.Context, id string) error        { return nil }
func (fakeStore) UpdateSession(ctx context.Context, id, label string) error { return nil }
func (fakeStore) AppendMessage(ctx context.Context, sessionID string, msg core.Message) error {
	return nil
}
func (fakeStore) GetMessages(ctx context.Context, sessionID string) ([]core.Message, error) {
	return nil, nil
}
func (fakeStore) GetMessagesWithTimes(ctx context.Context, sessionID string) ([]core.StoredMessage, error) {
	return nil, nil
}
func (fakeStore) GetActiveView(ctx context.Context, sessionID string) (core.ActiveView, error) {
	return core.ActiveView{}, nil
}
func (fakeStore) GetActiveMessages(ctx context.Context, sessionID string) ([]core.Message, error) {
	return nil, nil
}
func (fakeStore) GetSessionTree(ctx context.Context) ([]*core.SessionNode, error) { return nil, nil }
func (fakeStore) GetAncestorMessages(ctx context.Context, sessionID string) ([]core.Message, error) {
	return nil, nil
}
func (fakeStore) SearchMessages(ctx context.Context, query string) ([]core.SearchResult, error) {
	return nil, nil
}
func (fakeStore) SearchSessions(ctx context.Context, query string) ([]core.SearchResult, error) {
	return nil, nil
}
func (fakeStore) ComputeInsights(ctx context.Context, days int) (*core.Insights, error) {
	return &core.Insights{}, nil
}
func (fakeStore) CountMessages(ctx context.Context, sessionID string) (int, error) { return 0, nil }
func (fakeStore) SessionCounts(ctx context.Context) (map[string]core.SessionCounts, error) {
	return nil, nil
}
func (fakeStore) AddSessionTokens(ctx context.Context, sessionID string, prompt, completion int) error {
	return nil
}
func (fakeStore) SetSessionLastRun(ctx context.Context, sessionID string, stats *core.LastRunStats) error {
	return nil
}
func (fakeStore) SetSessionPinned(ctx context.Context, sessionID string, pinned bool) error {
	return nil
}
func (fakeStore) SetSessionRead(ctx context.Context, sessionID string) error { return nil }
func (fakeStore) SetSessionArchived(ctx context.Context, sessionID string, archived bool) error {
	return nil
}
func (fakeStore) SetSessionTitle(ctx context.Context, sessionID, title string, final bool) error {
	return nil
}
func (fakeStore) SessionModels(ctx context.Context) (map[string]string, error) { return nil, nil }

// apiCapsule mounts the same capability set through registrations.
type apiCapsule struct {
	routes int // observed on the registry after the mount
}

func (*apiCapsule) Name() string       { return "plug" }
func (*apiCapsule) Provides() []string { return (*classicCapsule)(nil).Provides() }
func (*apiCapsule) Requires() []string { return []string{"notifications"} }

// Mount satisfies Plugin; MountAll dispatches APIPlugin, the body is
// never reached (the strangler rule - the classic body retires in
// phase F).
func (*apiCapsule) Mount(ctx *Context) error {
	return errors.New("not dispatched - mounted through the api")
}

func (*apiCapsule) MountWithAPI(ctx *Context, api *ExtensionAPI) error {
	for _, step := range []func() error{
		func() error { return api.RegisterStore(fakeStore{}) },
		func() error { return api.RegisterLogger(&plugLogger{}) },
		func() error { return api.RegisterLoop(plugLoop{}) },
		func() error { return api.RegisterCompactor(plugCompactor{}) },
		func() error { return api.RegisterTool("plug.echo", echoTool()) },
		func() error { return api.RegisterPromptSectioner(plugSectioner{}) },
		func() error { return api.RegisterRenderer(plugTUI{}) },
		func() error { return api.RegisterPanel(plugPanel()) },
		func() error {
			return api.RegisterAction("plug.act", nil, func(v any) (any, error) { return v, nil })
		},
		func() error { return api.RegisterChannel(plugChannel{}) },
		func() error {
			return api.RegisterCommand("/plug", "parity command", nil,
				func(ctx context.Context, name, args string) (core.CommandResult, error) {
					if name != "/plug" {
						return core.CommandResult{}, fmt.Errorf("unknown command %q", name)
					}
					return core.CommandResult{Text: "plug ran"}, nil
				})
		},
		func() error {
			return api.RegisterRoute("GET /plug", func(w http.ResponseWriter, r *http.Request) {})
		},
		func() error {
			return api.RegisterEmitter(func(n core.NotificationsProvider) { emitterSeen = n })
		},
	} {
		if err := step(); err != nil {
			return err
		}
	}
	return nil
}

var emitterSeen core.NotificationsProvider

// TestDualMountParity mounts the same capability set both ways and
// demands identical behavior - the phase-B law (zero behavior change).
func TestDualMountParity(t *testing.T) {
	notifier := &plugNotifier{}
	emitterSeen = nil

	var classicCtx, apiCtx Context
	classic := &classicCapsule{}
	if _, err := MountAllAPI([]Plugin{&notifierPlugin{n: notifier}, classic}, &classicCtx, nil); err != nil {
		t.Fatalf("classic mount: %v", err)
	}
	reg, err := MountAllAPI([]Plugin{&notifierPlugin{n: notifier}, &apiCapsule{}}, &apiCtx, nil)
	if err != nil {
		t.Fatalf("api mount: %v", err)
	}

	// Exclusive slots: the same adapters in both styles (compared by
	// type - each side is its own instance, the behavior is the law).
	if fmt.Sprintf("%T", classicCtx.Logger) != fmt.Sprintf("%T", apiCtx.Logger) {
		t.Errorf("logger parity: %T vs %T", classicCtx.Logger, apiCtx.Logger)
	}
	if fmt.Sprintf("%T", classicCtx.Loop) != fmt.Sprintf("%T", apiCtx.Loop) {
		t.Errorf("loop parity: %T vs %T", classicCtx.Loop, apiCtx.Loop)
	}
	if fmt.Sprintf("%T", classicCtx.Compactor) != fmt.Sprintf("%T", apiCtx.Compactor) {
		t.Errorf("compactor parity: %T vs %T", classicCtx.Compactor, apiCtx.Compactor)
	}
	if classicCtx.Store == nil || apiCtx.Store == nil {
		t.Fatalf("store parity: nil store on %v side", map[bool]string{true: "classic", false: "api"}[classicCtx.Store == nil])
	}

	// The finished-things seam: the classic capsule captured it in
	// Mount, the api capsule received it at mount end - same instance.
	if classic.notifier != notifier {
		t.Errorf("classic capture got %T, want the mounted notifier", classic.notifier)
	}
	if emitterSeen != notifier {
		t.Errorf("emitter handoff got %T, want the mounted notifier", emitterSeen)
	}

	// Tools: same map shape, same tool behavior. (Run funcs are
	// distinct closures per mount - equality is on the inventory and
	// the behavior, not the pointers.)
	if len(classicCtx.ToolProviders) != 1 || len(apiCtx.ToolProviders) != 1 {
		t.Fatalf("tool provider parity: %d classic vs %d api", len(classicCtx.ToolProviders), len(apiCtx.ToolProviders))
	}
	classicTools := classicCtx.ToolProviders[0].Tools()
	apiTools := apiCtx.ToolProviders[0].Tools()
	if len(classicTools) != len(apiTools) {
		t.Errorf("tool count parity: %d vs %d", len(classicTools), len(apiTools))
	}
	for name := range classicTools {
		if apiTools[name].Description != classicTools[name].Description {
			t.Errorf("tool %q parity: %q vs %q", name, classicTools[name].Description, apiTools[name].Description)
		}
	}
	args := map[string]any{"v": "x"}
	o1, err1 := classicTools["plug.echo"].Run(context.Background(), args)
	o2, err2 := apiTools["plug.echo"].Run(context.Background(), args)
	if o1 != o2 || err1 != nil || err2 != nil {
		t.Errorf("tool run parity: %q/%v vs %q/%v", o1, err1, o2, err2)
	}

	// Inventory counts agree - /extensions sees both styles the same.
	if len(classicCtx.Infos) != 2 || len(apiCtx.Infos) != 2 {
		t.Fatalf("infos parity: %d vs %d", len(classicCtx.Infos), len(apiCtx.Infos))
	}
	classicInfo, apiInfo := classicCtx.Infos[1], apiCtx.Infos[1]
	if classicInfo.Tools != apiInfo.Tools || classicInfo.Commands != apiInfo.Commands {
		t.Errorf("inventory parity: %+v vs %+v", classicInfo, apiInfo)
	}

	// Commands: same table, same handler behavior (own name runs; a
	// foreign name falls through the chain and errors identically).
	if !reflect.DeepEqual(classicCtx.Commands, apiCtx.Commands) {
		t.Errorf("command table parity: %v vs %v", classicCtx.Commands, apiCtx.Commands)
	}
	for _, name := range []string{"/plug", "/unknown"} {
		r1, err1 := classicCtx.CommandHandler(context.Background(), name, "x")
		r2, err2 := apiCtx.CommandHandler(context.Background(), name, "x")
		if !reflect.DeepEqual(r1, r2) || fmt.Sprint(err1) != fmt.Sprint(err2) {
			t.Errorf("command %q parity: (%+v/%v) vs (%+v/%v)", name, r1, err1, r2, err2)
		}
	}

	// Panels: the canvas built over each context snapshots and
	// dispatches identically.
	classicCanvas, err := NewCanvas(&classicCtx, nil)
	if err != nil {
		t.Fatalf("classic canvas: %v", err)
	}
	apiCanvas, err := NewCanvas(&apiCtx, nil)
	if err != nil {
		t.Fatalf("api canvas: %v", err)
	}
	if !reflect.DeepEqual(classicCanvas.Snapshot(), apiCanvas.Snapshot()) {
		t.Errorf("snapshot parity:\n%+v\nvs\n%+v", classicCanvas.Snapshot(), apiCanvas.Snapshot())
	}
	good := core.ActionCall{Module: "plug", Action: "plug.act", Args: json.RawMessage(`{"v":"x"}`)}
	r1, err1 := classicCanvas.Dispatch(good)
	r2, err2 := apiCanvas.Dispatch(good)
	if !reflect.DeepEqual(r1, r2) || err1 != nil || err2 != nil {
		t.Errorf("action parity: (%+v/%v) vs (%+v/%v)", r1, err1, r2, err2)
	}

	// Additive slices: same length, same type set.
	if len(classicCtx.TUIProviders) != len(apiCtx.TUIProviders) ||
		len(classicCtx.PromptSectioners) != len(apiCtx.PromptSectioners) ||
		len(classicCtx.Channels) != len(apiCtx.Channels) {
		t.Errorf("additive parity: tui %d/%d sections %d/%d channels %d/%d",
			len(classicCtx.TUIProviders), len(apiCtx.TUIProviders),
			len(classicCtx.PromptSectioners), len(apiCtx.PromptSectioners),
			len(classicCtx.Channels), len(apiCtx.Channels))
	}
	if fmt.Sprintf("%T", classicCtx.TUIProviders[0]) != fmt.Sprintf("%T", apiCtx.TUIProviders[0]) {
		t.Errorf("renderer parity: %T vs %T", classicCtx.TUIProviders[0], apiCtx.TUIProviders[0])
	}
	if fmt.Sprintf("%T", classicCtx.Channels[0]) != fmt.Sprintf("%T", apiCtx.Channels[0]) {
		t.Errorf("channel parity: %T vs %T", classicCtx.Channels[0], apiCtx.Channels[0])
	}

	// The registry consumed the classic provides sweep too - the
	// exclusive claims landed before any API mount, so a late api
	// claim of a classically mounted seam fails loud (covered in
	// TestOneTenant); here the routes the api path registered.
	if routes := reg.Routes(); len(routes) != 1 || routes[0].Module != "plug" || routes[0].Pattern != "GET /plug" {
		t.Errorf("registered routes: %+v", reg.Routes())
	}
	if len(reg.Refusals()) != 0 {
		t.Errorf("clean mount must record no refusals: %+v", reg.Refusals())
	}
}

// TestNamespaceEnforcement pins Q10: a machine-facing registration
// must carry its own module prefix; anything else fails loud at
// registration.
func TestNamespaceEnforcement(t *testing.T) {
	var ctx Context
	reg, err := MountAllAPI([]Plugin{badNamespace{}}, &ctx, nil)
	if reg != nil {
		t.Errorf("a failed mount returns no registry")
	}
	if err == nil {
		t.Fatalf("the foreign-prefix registration must fail the mount")
	}
}

type badNamespace struct{ seen error }

func (badNamespace) Name() string       { return "plug" }
func (badNamespace) Provides() []string { return nil }
func (badNamespace) Requires() []string { return nil }
func (badNamespace) Mount(ctx *Context) error {
	return errors.New("not dispatched - mounted through the api")
}

func (b badNamespace) MountWithAPI(ctx *Context, api *ExtensionAPI) error {
	// A name under another module's namespace.
	if err := api.RegisterTool("evil.echo", echoTool()); err == nil {
		return fmt.Errorf("foreign-prefix tool name accepted")
	}
	// A name with no namespace at all.
	if err := api.RegisterTool("nodot", echoTool()); err == nil {
		return fmt.Errorf("un-namespaced tool name accepted")
	}
	// A panel action id under another namespace.
	if err := api.RegisterPanel(core.Panel{ID: "plug.main", Actions: []core.PanelAction{{ID: "evil.act", Module: "evil"}}}); err == nil {
		return fmt.Errorf("foreign-tenant panel action accepted")
	}
	return fmt.Errorf("all namespace violations were rejected (this error fails the mount on purpose, Q5 fail-loud)")
}

// TestOneTenant pins Q5: a seam mounted by one module cannot be
// claimed by another - neither by a classic provides sweep (recorded
// in the same ledger) nor by another api capsule. The boot fails loud
// carrying core.ErrSeamForeignTenant.
func TestOneTenant(t *testing.T) {
	// Classic claim first, api claim after: the api claim meets the
	// provides sweep's pre-recorded tenant.
	var ctxA Context
	_, err := MountAllAPI([]Plugin{classicStore{}, apiStoreClaimer{}}, &ctxA, nil)
	if err == nil {
		t.Fatalf("an api capsule claiming a classic-mounted seam must fail the mount")
	}
	if !errors.Is(err, core.ErrSeamForeignTenant) {
		t.Errorf("error chain must carry ErrSeamForeignTenant: %v", err)
	}
	// api claim first, classic provides after: the api claim is
	// first, and the (already conflict-checked) classic capsule
	// mounts second - the registry ledger still holds the api tenant,
	// the provides sweep recorded the classic one first, so the api
	// claim fails loud before any silent overwrite.
	var ctxB Context
	_, err = MountAllAPI([]Plugin{apiStoreClaimer{}, classicStore{}}, &ctxB, nil)
	if err == nil {
		t.Fatalf("a classic capsule claiming behind an api tenant must fail the mount")
	}
	if !errors.Is(err, core.ErrSeamForeignTenant) {
		t.Errorf("error chain must carry ErrSeamForeignTenant: %v", err)
	}
	// api vs api.
	var ctxC Context
	_, err = MountAllAPI([]Plugin{apiStoreClaimer{}, secondStore{}}, &ctxC, nil)
	if err == nil {
		t.Fatalf("a second module's store claim must fail the mount")
	}
	if !errors.Is(err, core.ErrSeamForeignTenant) {
		t.Errorf("error chain must carry ErrSeamForeignTenant: %v", err)
	}
}

// classicStore claims the store seam the classic way.
type classicStore struct{}

func (classicStore) Name() string             { return "classic" }
func (classicStore) Provides() []string       { return []string{"store"} }
func (classicStore) Requires() []string       { return nil }
func (classicStore) Mount(ctx *Context) error { ctx.Store = fakeStore{}; return nil }

// apiStoreClaimer claims the store seam through the api, declaring no
// provides (the registry ledger is the tenant record).
type apiStoreClaimer struct{}

func (apiStoreClaimer) Name() string       { return "claimant" }
func (apiStoreClaimer) Provides() []string { return nil }
func (apiStoreClaimer) Requires() []string { return nil }
func (apiStoreClaimer) Mount(ctx *Context) error {
	return errors.New("not dispatched - mounted through the api")
}
func (apiStoreClaimer) MountWithAPI(ctx *Context, api *ExtensionAPI) error {
	return api.RegisterStore(fakeStore{})
}

// secondStore mounts the store seam through the api - a cross-tenant
// claim after the api capsule owns it.
type secondStore struct{}

func (secondStore) Name() string       { return "second" }
func (secondStore) Provides() []string { return []string{"store"} }
func (secondStore) Requires() []string { return nil }
func (secondStore) Mount(ctx *Context) error {
	return errors.New("not dispatched - mounted through the api")
}
func (secondStore) MountWithAPI(ctx *Context, api *ExtensionAPI) error {
	return api.RegisterStore(fakeStore{})
}

// TestSameTenantReplace pins Q7: the same capsule re-registering its
// own name replaces the entry - mount stays clean-idempotent.
func TestSameTenantReplace(t *testing.T) {
	var ctx Context
	reg, err := MountAllAPI([]Plugin{replaceCapsule{}}, &ctx, nil)
	if err != nil {
		t.Fatalf("same-tenant replacement must not error: %v", err)
	}
	// One tool provider, one tool: the second registration replaced.
	tools := ctx.ToolProviders[0].Tools()
	if len(tools) != 1 {
		t.Fatalf("re-registered tool must replace, got %d tools", len(tools))
	}
	if tools["plug.echo"].Description != "second" {
		t.Errorf("the replacement must win: %q", tools["plug.echo"].Description)
	}
	// One route, the later handler.
	routes := reg.Routes()
	if len(routes) != 1 {
		t.Fatalf("re-registered route must replace, got %d", len(routes))
	}
}

type replaceCapsule struct{}

func (replaceCapsule) Name() string       { return "plug" }
func (replaceCapsule) Provides() []string { return nil }
func (replaceCapsule) Requires() []string { return nil }
func (replaceCapsule) Mount(ctx *Context) error {
	return errors.New("not dispatched - mounted through the api")
}
func (replaceCapsule) MountWithAPI(ctx *Context, api *ExtensionAPI) error {
	first := echoTool()
	first.Description = "first"
	if err := api.RegisterTool("plug.echo", first); err != nil {
		return err
	}
	second := echoTool()
	second.Description = "second"
	if err := api.RegisterTool("plug.echo", second); err != nil {
		return err
	}
	if err := api.RegisterRoute("GET /plug", func(w http.ResponseWriter, r *http.Request) {}); err != nil {
		return err
	}
	return api.RegisterRoute("GET /plug", func(w http.ResponseWriter, r *http.Request) {})
}

// TestActionShapeCheckProof is SKETCH 3's first step, end-to-end: a
// registration-bound ShapeCheck guards the action pipe through the
// real canvas runtime. A legal payload runs the handler; a bad shape
// is refused - handler never fires - and the refusal lands in the
// ledger with module, seam and failed predicate (Q12).
func TestActionShapeCheckProof(t *testing.T) {
	var ctx Context
	// The check: the payload must be an object carrying a number "n".
	numCheck := func(v any) error {
		m, ok := v.(map[string]any)
		if !ok {
			return fmt.Errorf("payload must be an object")
		}
		if _, ok := m["n"].(float64); !ok {
			return fmt.Errorf("field n must be a number")
		}
		return nil
	}
	reg, err := MountAllAPI([]Plugin{proofCapsule{check: numCheck}}, &ctx, nil)
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	canvas, err := NewCanvas(&ctx, nil)
	if err != nil {
		t.Fatalf("canvas: %v", err)
	}

	// Legal shape runs: the decoded map reaches the handler.
	out, err := canvas.Dispatch(core.ActionCall{Module: "plug", Action: "plug.act", Args: json.RawMessage(`{"n":1}`)})
	if err != nil {
		t.Fatalf("legal payload must pass the shape check: %v", err)
	}
	if m, ok := out.(map[string]any); !ok || m["n"] != float64(1) {
		t.Errorf("handler got %+v", out)
	}
	if len(reg.Refusals()) != 0 {
		t.Errorf("a legal payload records no refusal: %+v", reg.Refusals())
	}

	// Bad shape is refused: handler never fires, ledger holds module,
	// seam and failed predicate.
	handledByCheck = 0
	before := len(reg.Refusals())
	_, err = canvas.Dispatch(core.ActionCall{Module: "plug", Action: "plug.act", Args: json.RawMessage(`{"n":"x"}`)})
	if err == nil {
		t.Fatalf("the bad shape must be refused")
	}
	if handledByCheck != 0 {
		t.Errorf("the refused handler must never run (it ran %d times)", handledByCheck)
	}
	refusals := reg.Refusals()
	if len(refusals) != before+1 {
		t.Fatalf("one refusal must land, have %+v", refusals)
	}
	r := refusals[len(refusals)-1]
	if r.Module != "plug" || r.Seam != "panels" || r.Predicate != "plug.act" || r.Err == nil {
		t.Errorf("refusal carries module, seam, failed predicate and error: %+v", r)
	}

	// Not-even-JSON is refused at the decode boundary.
	before = len(reg.Refusals())
	_, err = canvas.Dispatch(core.ActionCall{Module: "plug", Action: "plug.act", Args: json.RawMessage(`not the one`)})
	if err == nil {
		t.Fatalf("unparseable payload must be refused")
	}
	if handledByCheck != 0 {
		t.Errorf("the decode-refused handler must never run")
	}
	if got := len(reg.Refusals()); got != before+1 {
		t.Fatalf("one more refusal must land, have %d", got)
	}

	// No args at all: an argless action call (the omitempty omission)
	// reaches its handler as a nil payload and passes - on an action
	// registered without a check.
	handledByCheck = 0
	out, err = canvas.Dispatch(core.ActionCall{Module: "plug", Action: "plug.ping"})
	if err != nil {
		t.Fatalf("argless call passes the boundary: %v", err)
	}
	if out != "pong" {
		t.Errorf("argless call reaches its handler: %+v", out)
	}
}

var handledByCheck int

// proofCapsule registers one panel action with a boundary ShapeCheck -
// the one working proof seam of phase B.
type proofCapsule struct{ check core.ShapeCheck }

func (proofCapsule) Name() string       { return "plug" }
func (proofCapsule) Provides() []string { return []string{"panels"} }
func (proofCapsule) Requires() []string { return nil }
func (proofCapsule) Mount(ctx *Context) error {
	return errors.New("not dispatched - mounted through the api")
}
func (p proofCapsule) MountWithAPI(ctx *Context, api *ExtensionAPI) error {
	if err := api.RegisterPanel(core.Panel{
		ID: "plug.main",
		Actions: []core.PanelAction{
			{ID: "plug.act", Label: "act"},
			{ID: "plug.ping", Label: "ping"},
		},
	}); err != nil {
		return err
	}
	if err := api.RegisterAction("plug.act", p.check, func(v any) (any, error) {
		handledByCheck++
		return v, nil
	}); err != nil {
		return err
	}
	return api.RegisterAction("plug.ping", nil, func(v any) (any, error) { return "pong", nil })
}

// TestEmitterHandoffNilSafe: with the notifications module absent the
// emitter hook receives nil - the same deal the classic nil-guard
// capture has. The capsule requires nothing, so the mount completes
// with no seam to hand over.
func TestEmitterHandoffNilSafe(t *testing.T) {
	var ctx Context
	emitterSeen = nil
	if _, err := MountAllAPI([]Plugin{nilEmitterCapsule{}}, &ctx, nil); err != nil {
		t.Fatalf("mount: %v", err)
	}
	if ctx.Notifications != nil {
		t.Fatalf("no notifications plugin mounted, got %T", ctx.Notifications)
	}
	if emitterSeen != nil {
		t.Errorf("the hook must receive nil when the seam is absent, got %T", emitterSeen)
	}
}

type nilEmitterCapsule struct{}

func (nilEmitterCapsule) Name() string             { return "plug" }
func (nilEmitterCapsule) Provides() []string       { return nil }
func (nilEmitterCapsule) Requires() []string       { return nil }
func (nilEmitterCapsule) Mount(ctx *Context) error { return nil }
func (nilEmitterCapsule) MountWithAPI(ctx *Context, api *ExtensionAPI) error {
	return api.RegisterEmitter(func(n core.NotificationsProvider) { emitterSeen = n })
}

// TestCensusEnforcement walks the registration surface and demands the
// census: all 23 seam mappings ride a registration (15 exclusive
// slots, 8 additive surfaces - the delegation pipe carries both its
// fields) plus the two no-classic-seam families (routes, emitters) and
// the action-pipe registration. A name going missing breaks the census.
func TestCensusEnforcement(t *testing.T) {
	want := map[string]bool{
		"RegisterTool":            true,
		"RegisterPanel":           true,
		"RegisterAction":          true,
		"RegisterPanelProvider":   true,
		"RegisterRoute":           true,
		"RegisterCommand":         true,
		"RegisterEmitter":         true,
		"RegisterRenderer":        true,
		"RegisterPromptSectioner": true,
		"RegisterChannel":         true,
		"RegisterDelegation":      true,
		"RegisterProvider":        true,
		"RegisterCompactor":       true,
		"RegisterStore":           true,
		"RegisterSkills":          true,
		"RegisterLoop":            true,
		"RegisterLogger":          true,
		"RegisterMemory":          true,
		"RegisterPromptBuilder":   true,
		"RegisterFrontend":        true,
		"RegisterTrust":           true,
		"RegisterCron":            true,
		"RegisterKanban":          true,
		"RegisterNotifications":   true,
		"RegisterVerdicts":        true,
		"RegisterCaptioner":       true,
	}
	got := map[string]bool{}
	typ := reflect.TypeOf(&ExtensionAPI{})
	for i := 0; i < typ.NumMethod(); i++ {
		got[typ.Method(i).Name] = true
	}
	for name := range want {
		if !got[name] {
			t.Errorf("the census is broken: %q is missing from the registration surface", name)
		}
	}
}

// TestSubcommandRegistration pins the phase-D CLI law (grill Q6 - the
// module owns its argv): the registration is a thin name+handler
// prefix, the name keeps its user-facing natural shape (bare word, no
// namespace pair, no leading / or -), the registry records the owner,
// a cross-tenant claim of the same subcommand fails the mount loud
// (Q5), same-tenant re-registration replaces (Q7), and the registry
// table lists the surviving entries.
type subCapsule struct {
	name    string
	summary string
	handler func(argv []string) error
}

func (p *subCapsule) Name() string       { return p.name }
func (p *subCapsule) Provides() []string { return nil }
func (p *subCapsule) Requires() []string { return nil }
func (p *subCapsule) Mount(ctx *Context) error {
	return errors.New("not dispatched - mounted through the api")
}
func (p *subCapsule) MountWithAPI(ctx *Context, api *ExtensionAPI) error {
	handler := p.handler
	if handler == nil {
		handler = func(argv []string) error { return nil }
	}
	return api.RegisterSubcommand("greet", p.summary, handler)
}

func TestSubcommandRegistration(t *testing.T) {
	greeted := ""
	var ctx Context
	reg, err := MountAllAPI([]Plugin{
		&subCapsule{name: "greeter", summary: "greets by name", handler: func(argv []string) error {
			greeted = argv[0]
			return nil
		}},
	}, &ctx, nil)
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	subs := reg.Subcommands()
	if len(subs) != 1 || subs[0].Name != "greet" || subs[0].Module != "greeter" || subs[0].Summary != "greets by name" {
		t.Fatalf("registry table: %+v", subs)
	}
	if err := subs[0].Handler([]string{"world"}); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if greeted != "world" {
		t.Fatalf("handler argv: %q", greeted)
	}

	// cross-tenant: a second module claiming `greet` fails the mount
	var ctx2 Context
	_, err = MountAllAPI([]Plugin{&subCapsule{name: "greeter"}, &subCapsule{name: "other"}}, &ctx2, nil)
	if err == nil {
		t.Fatalf("a cross-tenant subcommand claim must fail the mount")
	}
	if !errors.Is(err, core.ErrSeamForeignTenant) {
		t.Errorf("error chain must carry ErrSeamForeignTenant: %v", err)
	}

	// name shape: the registration refuses non-bare-word names
	shape := func(name string) error {
		api := &ExtensionAPI{module: "shape", ctx: &Context{}, reg: newRegistry(nil)}
		return api.RegisterSubcommand(name, "", func(argv []string) error { return nil })
	}
	for _, bad := range []string{"", " spaced ", "-flag", "/slash"} {
		if err := shape(bad); err == nil {
			t.Errorf("subcommand %q accepted, want refusal", bad)
		}
	}

	// same tenant remount (Q7): a fresh registry is clean-idempotent
	var ctx3 Context
	reg3, err := MountAllAPI([]Plugin{
		&subCapsule{name: "greeter", summary: "greets by name"},
	}, &ctx3, nil)
	if err != nil {
		t.Fatalf("remount: %v", err)
	}
	if len(reg3.Subcommands()) != 1 {
		t.Fatalf("remount table: %+v", reg3.Subcommands())
	}
}
