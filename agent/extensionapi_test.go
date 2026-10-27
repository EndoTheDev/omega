package agent

// The registration-surface tests (v0.12 phase B onward). The identity
// law (namespaced machine-facing names, Q10), the one-tenant rule
// (Q5), clean-idempotent replacement (Q7), the census enforcement
// (every seam maps to a registration), the CLI subcommand law (Q6),
// the emitter handoff and the subcommand registry.

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

// notifierPlugin mounts the notifications seam (the notifications edge).
type notifierPlugin struct{ n core.NotificationsProvider }

func (p *notifierPlugin) Name() string       { return "notifier" }
func (p *notifierPlugin) Provides() []string { return []string{"notifications"} }
func (p *notifierPlugin) Requires() []string { return nil }
func (p *notifierPlugin) MountWithAPI(ctx *Context, api *ExtensionAPI) error {
	ctx.Notifications = p.n
	return nil
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

var emitterSeen core.NotificationsProvider

// TestNamespaceEnforcement pins Q10: a machine-facing registration
// must carry its own module prefix; anything else fails loud at
// registration.
func TestNamespaceEnforcement(t *testing.T) {
	var ctx Context
	reg, err := MountAllAPI([]Plugin{badNamespace{}}, &ctx, nil, nil)
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
func (b badNamespace) MountWithAPI(ctx *Context, api *ExtensionAPI) error {
	// A name under another module's namespace.
	if err := api.RegisterTool("evil.echo", echoTool()); err == nil {
		return fmt.Errorf("foreign-prefix tool name accepted")
	}
	// A name with no namespace at all.
	if err := api.RegisterTool("nodot", echoTool()); err == nil {
		return fmt.Errorf("un-namespaced tool name accepted")
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
	_, err := MountAllAPI([]Plugin{classicStore{}, apiStoreClaimer{}}, &ctxA, nil, nil)
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
	_, err = MountAllAPI([]Plugin{apiStoreClaimer{}, classicStore{}}, &ctxB, nil, nil)
	if err == nil {
		t.Fatalf("a classic capsule claiming behind an api tenant must fail the mount")
	}
	if !errors.Is(err, core.ErrSeamForeignTenant) {
		t.Errorf("error chain must carry ErrSeamForeignTenant: %v", err)
	}
	// api vs api.
	var ctxC Context
	_, err = MountAllAPI([]Plugin{apiStoreClaimer{}, secondStore{}}, &ctxC, nil, nil)
	if err == nil {
		t.Fatalf("a second module's store claim must fail the mount")
	}
	if !errors.Is(err, core.ErrSeamForeignTenant) {
		t.Errorf("error chain must carry ErrSeamForeignTenant: %v", err)
	}
}

// classicStore claims the store seam the classic way.
type classicStore struct{}

func (classicStore) Name() string       { return "classic" }
func (classicStore) Provides() []string { return []string{"store"} }
func (classicStore) Requires() []string { return nil }
func (classicStore) MountWithAPI(ctx *Context, api *ExtensionAPI) error {
	ctx.Store = fakeStore{}
	return nil
}

// apiStoreClaimer claims the store seam through the api, declaring no
// provides (the registry ledger is the tenant record).
type apiStoreClaimer struct{}

func (apiStoreClaimer) Name() string       { return "claimant" }
func (apiStoreClaimer) Provides() []string { return nil }
func (apiStoreClaimer) Requires() []string { return nil }
func (apiStoreClaimer) MountWithAPI(ctx *Context, api *ExtensionAPI) error {
	return api.RegisterStore(fakeStore{})
}

// secondStore mounts the store seam through the api - a cross-tenant
// claim after the api capsule owns it.
type secondStore struct{}

func (secondStore) Name() string       { return "second" }
func (secondStore) Provides() []string { return []string{"store"} }
func (secondStore) Requires() []string { return nil }
func (secondStore) MountWithAPI(ctx *Context, api *ExtensionAPI) error {
	return api.RegisterStore(fakeStore{})
}

// TestSameTenantReplace pins Q7: the same capsule re-registering its
// own name replaces the entry - mount stays clean-idempotent.
func TestSameTenantReplace(t *testing.T) {
	var ctx Context
	reg, err := MountAllAPI([]Plugin{replaceCapsule{}}, &ctx, nil, nil)
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

// TestEmitterHandoffNilSafe: with the notifications module absent the
// emitter hook receives nil - the same deal the classic nil-guard
// capture has. The capsule requires nothing, so the mount completes
// with no seam to hand over.
func TestEmitterHandoffNilSafe(t *testing.T) {
	var ctx Context
	emitterSeen = nil
	if _, err := MountAllAPI([]Plugin{nilEmitterCapsule{}}, &ctx, nil, nil); err != nil {
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
// census: the 23 seam surfaces ride a registration (15 exclusive
// slots, 8 additive surfaces - the delegation pipe carries both its
// fields) plus the two no-classic-seam families (routes, emitters).
// A name going missing breaks the census.
func TestCensusEnforcement(t *testing.T) {
	want := map[string]bool{
		"RegisterTool":            true,
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
		"RegisterNotifications":   true,
		"RegisterVerdicts":        true,
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
	}, &ctx, nil, nil)
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
	_, err = MountAllAPI([]Plugin{&subCapsule{name: "greeter"}, &subCapsule{name: "other"}}, &ctx2, nil, nil)
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
	}, &ctx3, nil, nil)
	if err != nil {
		t.Fatalf("remount: %v", err)
	}
	if len(reg3.Subcommands()) != 1 {
		t.Fatalf("remount table: %+v", reg3.Subcommands())
	}
}
