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