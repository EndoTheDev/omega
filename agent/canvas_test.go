package agent

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// panelProviderStub is one module: declared panels plus a
// handler stub. name is what all its action rows declare.
type panelProviderStub struct {
	name    string
	panels  []core.Panel
	handler func(call core.ActionCall) (any, error)
	// nil handler means a bare (registered, never-called) surface.
}

func (f *panelProviderStub) Panels() []core.Panel { return f.panels }

func (f *panelProviderStub) HandleAction(call core.ActionCall) (any, error) {
	if f.handler == nil {
		return nil, errors.New("no handler registered")
	}
	return f.handler(call)
}

func panel(id, kind string, actions ...core.PanelAction) core.Panel {
	return core.Panel{ID: id, Title: id, Kind: kind, Actions: actions}
}

func act(id string, module string) core.PanelAction {
	return core.PanelAction{ID: id, Label: id, Module: module}
}

// twoProviders is the standard mount: kanban first, then files.
func twoProviders() []core.PanelProvider {
	return []core.PanelProvider{
		&panelProviderStub{name: "kanban", panels: []core.Panel{
			panel("board", "kanban.board", act("refresh", "kanban")),
			panel("stats", "kanban.stats", act("recalc", "kanban")),
		}},
		&panelProviderStub{name: "files", panels: []core.Panel{
			panel("browser", "files.browser", act("reload", "files")),
		}},
	}
}

func mountedCtx(t *testing.T, providers []core.PanelProvider, emit func(core.Event)) (*Canvas, error) {
	t.Helper()
	ctx := &core.Context{PanelProviders: providers}
	return NewCanvas(ctx, emit)
}

func TestRegistrationSeedsSnapshot(t *testing.T) {
	c, err := mountedCtx(t, twoProviders(), nil)
	if err != nil {
		t.Fatalf("NewCanvas: %v", err)
	}
	snap := c.Snapshot()
	if len(snap) != 3 {
		t.Fatalf("snapshot panels = %d, want 3", len(snap))
	}
	// Mount order governs modules; panel ids sort within a module.
	want := []string{"board", "stats", "browser"}
	for i, id := range want {
		if snap[i].ID != id {
			t.Errorf("snapshot[%d] = %q, want %q", i, snap[i].ID, id)
		}
	}
}

func TestSnapshotReflectsCurrentState(t *testing.T) {
	c, err := mountedCtx(t, twoProviders(), nil)
	if err != nil {
		t.Fatalf("NewCanvas: %v", err)
	}
	if err := c.Update("kanban", core.Panel{
		ID: "board", Title: "Kanban", Kind: "kanban.board",
		Payload: map[string]int{"lanes": 3},
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	var found bool
	for _, p := range c.Snapshot() {
		if p.ID != "board" {
			continue
		}
		found = true
		payload, ok := p.Payload.(map[string]int)
		if !ok || payload["lanes"] != 3 {
			t.Errorf("board payload = %#v, want {lanes:3}", p.Payload)
		}
	}
	if !found {
		t.Error("updated panel missing from snapshot")
	}
}

func TestUpdateEmitsDelta(t *testing.T) {
	var deltas []core.ViewModelDelta
	c, err := mountedCtx(t, twoProviders(), func(e core.Event) {
		if d, ok := e.(core.ViewModelDelta); ok {
			deltas = append(deltas, d)
		}
	})
	if err != nil {
		t.Fatalf("NewCanvas: %v", err)
	}
	if err := c.Update("kanban", core.Panel{ID: "stats", Kind: "kanban.stats", Payload: "fresh"}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(deltas) != 1 {
		t.Fatalf("deltas = %d, want 1", len(deltas))
	}
	d := deltas[0]
	if d.Type != "view_model_delta" {
		t.Errorf("delta type = %q, want view_model_delta", d.Type)
	}
	if d.Panel.ID != "stats" || d.Panel.Payload != "fresh" {
		t.Errorf("delta panel = %+v, want stats/fresh", d.Panel)
	}
	// The registered actions travel with every delta - descriptors are
	// self-describing and the transport never re-resolves them.
	if len(d.Panel.Actions) != 1 || d.Panel.Actions[0].ID != "recalc" {
		t.Errorf("delta actions = %+v, want the registered recalc row", d.Panel.Actions)
	}
}

func TestUpdateUnknownModule(t *testing.T) {
	c, err := mountedCtx(t, twoProviders(), nil)
	if err != nil {
		t.Fatalf("NewCanvas: %v", err)
	}
	err = c.Update("nowhere", core.Panel{ID: "x", Kind: "k"})
	if !errors.Is(err, core.ErrUnknownModule) {
		t.Errorf("err = %v, want ErrUnknownModule", err)
	}
}

func TestRegistrationWiringBugs(t *testing.T) {
	cases := []struct {
		name      string
		providers []core.PanelProvider
		wantErr   string
	}{
		{"duplicate panel id", []core.PanelProvider{&panelProviderStub{
			panels: []core.Panel{
				panel("board", "a", core.PanelAction{ID: "go", Module: "kanban"}),
				panel("board", "b", core.PanelAction{ID: "go", Module: "kanban"}),
			},
		}}, "declared twice"},
		{"duplicate module", []core.PanelProvider{
			&panelProviderStub{
				panels: []core.Panel{panel("p", "k", core.PanelAction{ID: "a", Module: "kanban"})},
			},
			&panelProviderStub{
				panels: []core.Panel{panel("q", "k", core.PanelAction{ID: "b", Module: "kanban"})},
			},
		}, "registered twice"},
		{"conflicting module names", []core.PanelProvider{
			&panelProviderStub{
				panels: []core.Panel{
					panel("p", "k", core.PanelAction{ID: "a", Module: "kanban"}),
					panel("q", "k", core.PanelAction{ID: "b", Module: "files"}),
				},
			},
		}, "one provider declares"},
		{"actionless module name", []core.PanelProvider{
			&panelProviderStub{panels: []core.Panel{panel("bare", "k")}},
		}, ""},
	}
	for _, tc := range cases {
		_, err := mountedCtx(t, tc.providers, nil)
		if tc.wantErr == "" {
			if err != nil {
				t.Errorf("%s: err = %v, want silent skip", tc.name, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("%s: err = %v, want match %q", tc.name, err, tc.wantErr)
		}
	}
}

// TestUpdateRejectsUndeclaredPanelId: a push naming a panel the module
// never declared is a phantom panel (it would sit in the namespace
// forever) - the runtime rejects it and the snapshot grows nothing.
func TestUpdateRejectsUndeclaredPanelId(t *testing.T) {
	c, err := mountedCtx(t, twoProviders(), nil)
	if err != nil {
		t.Fatalf("NewCanvas: %v", err)
	}
	err = c.Update("kanban", core.Panel{ID: "ghost", Kind: "kanban.ghost"})
	if err == nil || !strings.Contains(err.Error(), "never declared") {
		t.Errorf("phantom panel update = %v, want a never-declared rejection", err)
	}
	if got := len(c.Snapshot()); got != 3 {
		t.Errorf("snapshot = %d panels, want 3 (nothing minted)", got)
	}
	// A declared id still updates.
	if err := c.Update("kanban", core.Panel{ID: "board", Kind: "kanban.board", Payload: "ok"}); err != nil {
		t.Errorf("declared panel update = %v, want nil", err)
	}
}

// TestDispatchHappyPath: the dispatch hands the call to the handler
// verbatim and its result passes through.
func TestDispatchHappyPath(t *testing.T) {
	var got core.ActionCall
	c, err := mountedCtx(t, []core.PanelProvider{&panelProviderStub{
		panels: []core.Panel{panel("browser", "files.browser", act("reload", "files"))},
		handler: func(call core.ActionCall) (any, error) {
			got = call
			return map[string]int{"reloaded": 2}, nil
		},
	}}, nil)
	if err != nil {
		t.Fatalf("NewCanvas: %v", err)
	}
	res, err := c.Dispatch(core.ActionCall{
		Module: "files",
		Action: "reload",
		Args:   json.RawMessage(`{"path":"/tmp"}`),
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if got.Action != "reload" || string(got.Args) != `{"path":"/tmp"}` {
		t.Errorf("handler received %+#v, want reload/{\"path\":\"/tmp\"}", got)
	}
	if res == nil {
		t.Error("handler result lost")
	}
}

func TestDispatch404Shapes(t *testing.T) {
	c, err := mountedCtx(t, twoProviders(), nil)
	if err != nil {
		t.Fatalf("NewCanvas: %v", err)
	}
	cases := []struct {
		name string
		call core.ActionCall
		want error
	}{
		{"unknown module", core.ActionCall{Module: "weblogs", Action: "refresh"}, core.ErrUnknownModule},
		{"empty module", core.ActionCall{Action: "refresh"}, core.ErrUnknownModule},
		{"unknown action", core.ActionCall{Module: "kanban", Action: "explode"}, core.ErrUnknownAction},
		{"cross-module action id", core.ActionCall{Module: "kanban", Action: "reload"}, core.ErrUnknownAction},
	}
	for _, tc := range cases {
		_, err := c.Dispatch(tc.call)
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
}

func TestConcurrentSnapshotUpdateDispatch(t *testing.T) {
	// The -race tripwire for the same law the runtime serves: module
	// goroutines update while a frontend snapshots and dispatches.
	c, err := mountedCtx(t, []core.PanelProvider{&panelProviderStub{
		panels: []core.Panel{panel("board", "kanban.board", act("refresh", "kanban"))},
		handler: func(call core.ActionCall) (any, error) {
			return nil, nil
		},
	}}, func(e core.Event) {})
	if err != nil {
		t.Fatalf("NewCanvas: %v", err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(3)
		// updater
		go func(i int) {
			defer wg.Done()
			_ = c.Update("kanban", core.Panel{ID: "board", Kind: "kanban.board", Payload: i})
		}(i)
		// reader
		go func() {
			defer wg.Done()
			_ = c.Snapshot()
		}()
		// dispatcher
		go func() {
			defer wg.Done()
			_, _ = c.Dispatch(core.ActionCall{Module: "kanban", Action: "refresh"})
		}()
	}
	wg.Wait()
}

// sinkProviderStub is a module whose panel state changes outside the
// action pipe: it records the update closure NewCanvas installs.
type sinkProviderStub struct {
	*panelProviderStub
	mu     sync.Mutex
	update func(core.Panel) error
	got    bool
}

func (s *sinkProviderStub) SetPanelUpdate(update func(core.Panel) error) {
	s.mu.Lock()
	s.update, s.got = update, update != nil
	s.mu.Unlock()
}

// TestSinkInstallationAndRoute: a PanelUpdateSink provider gets the
// update closure installed at registration, bound to its own module
// namespace - a push updates the snapshot and rides the delta emitter
// exactly like an action-pipe refresh.
func TestSinkInstallationAndRoute(t *testing.T) {
	s := &sinkProviderStub{panelProviderStub: &panelProviderStub{name: "notifications", panels: []core.Panel{
		panel("notifications", "list", act("mark_all", "notifications")),
	}}}
	var deltas []core.Panel
	c, err := mountedCtx(t, []core.PanelProvider{s}, func(ev core.Event) {
		if d, ok := ev.(core.ViewModelDelta); ok {
			deltas = append(deltas, d.Panel)
		}
	})
	if err != nil {
		t.Fatalf("new canvas: %v", err)
	}
	s.mu.Lock()
	got, update := s.got, s.update
	s.mu.Unlock()
	if !got || update == nil {
		t.Fatalf("sink not installed at registration")
	}
	if err := update(core.Panel{ID: "notifications", Kind: "list", Payload: "pushed"}); err != nil {
		t.Fatalf("push: %v", err)
	}
	snap := c.Snapshot()
	if len(snap) != 1 || snap[0].Payload != "pushed" {
		t.Fatalf("push did not land in the namespace: %+v", snap)
	}
	if len(deltas) != 1 || deltas[0].Payload != "pushed" {
		t.Fatalf("push did not emit a delta: %+v", deltas)
	}
}

// TestSinksStayInTheirNamespace: each sink's closure is bound to its
// own module namespace - two providers pushing the same panel id land
// as two distinct panels, not one shared slot.
func TestSinksStayInTheirNamespace(t *testing.T) {
	mk := func(name string) *sinkProviderStub {
		return &sinkProviderStub{panelProviderStub: &panelProviderStub{name: name, panels: []core.Panel{
			panel("status", "plain", act("refresh", name)),
		}}}
	}
	a, b := mk("notifications"), mk("kanban")
	c, err := mountedCtx(t, []core.PanelProvider{a, b}, nil)
	if err != nil {
		t.Fatalf("new canvas: %v", err)
	}
	a.mu.Lock()
	updateA := a.update
	a.mu.Unlock()
	b.mu.Lock()
	updateB := b.update
	b.mu.Unlock()
	if updateA == nil || updateB == nil {
		t.Fatalf("sinks not installed")
	}
	_ = updateA(core.Panel{ID: "status", Kind: "plain", Payload: "from-a"})
	_ = updateB(core.Panel{ID: "status", Kind: "plain", Payload: "from-b"})
	snap := c.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("two namespaces, two panels: %+v", snap)
	}
	// snapshot order: mount order - notifications' seed panel replaced
	// by its push, kanban's likewise; the payloads must not cross
	if snap[0].Payload != "from-a" || snap[1].Payload != "from-b" {
		t.Fatalf("namespace closures leaked: %+v", snap)
	}
}

// TestActionlessProviderGetsNoSink: a provider without actions
// contributes nothing (silent registration) and is not handed a sink -
// no namespace exists to bind to.
func TestActionlessProviderGetsNoSink(t *testing.T) {
	s := &sinkProviderStub{panelProviderStub: &panelProviderStub{name: "silent", panels: []core.Panel{
		panel("muted", "plain"),
	}}}
	if _, err := mountedCtx(t, []core.PanelProvider{s}, nil); err != nil {
		t.Fatalf("new canvas: %v", err)
	}
	s.mu.Lock()
	got := s.got
	s.mu.Unlock()
	if got {
		t.Fatalf("actionless provider must not receive a sink")
	}
}

// TestCanvasResyncDetachesAndRestores pins the live-toggle contract:
// Resync drops a module whose provider left the Context (panels and
// actions die without a restart) and registers a provider the scoped
// remount appended. Untouched modules keep their state.
func TestCanvasResyncDetachesAndRestores(t *testing.T) {
	c, err := mountedCtx(t, twoProviders(), nil)
	if err != nil {
		t.Fatalf("NewCanvas: %v", err)
	}

	// kanban's provider leaves the Context (the disable cascade).
	offCtx := &core.Context{PanelProviders: []core.PanelProvider{
		twoProviders()[1], // files stays mounted
	}}
	if err := c.Resync(offCtx); err != nil {
		t.Fatalf("Resync (disable): %v", err)
	}
	for _, p := range c.Snapshot() {
		if strings.HasPrefix(p.Kind, "kanban.") {
			t.Fatalf("kanban panel %q survived the dismount resync", p.ID)
		}
	}
	if _, err := c.Dispatch(core.ActionCall{Module: "kanban", Action: "refresh"}); !errors.Is(err, core.ErrUnknownModule) {
		t.Fatalf("kanban dispatch after disable = %v, want ErrUnknownModule", err)
	}

	// ... and the scoped remount appends a fresh provider: it
	// registers again, files stays exactly where it was.
	onCtx := &core.Context{PanelProviders: twoProviders()}
	if err := c.Resync(onCtx); err != nil {
		t.Fatalf("Resync (enable): %v", err)
	}
	var ids []string
	for _, p := range c.Snapshot() {
		ids = append(ids, p.ID)
	}
	if len(ids) != 3 {
		t.Fatalf("after enable re-register snapshot = %v, want 3 panels", ids)
	}
	if _, err := c.Dispatch(core.ActionCall{Module: "kanban", Action: "refresh"}); err != nil && !strings.Contains(err.Error(), "no handler registered") {
		// the stub registers no handler - the 404 shape would be the failure
		t.Fatalf("kanban dispatch after enable routed nowhere: %v", err)
	}
}
