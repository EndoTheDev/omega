package tui

// Tests for the TUI's native canvas consumption (arc 3d): the
// in-process subscription over a fake panel spine, the plain v1
// rendering of mounted panels, and the numbered-key action trigger
// through the one action pipe.

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/EndoTheDev/omega/core"
)

// fakePanelProvider is a fake spine participant: it declares panels
// and records the action calls the pipe routes to it.
type fakePanelProvider struct {
	panels []core.Panel
	calls  []core.ActionCall
	result any
	err    error
}

func (f *fakePanelProvider) Panels() []core.Panel { return f.panels }

func (f *fakePanelProvider) HandleAction(call core.ActionCall) (any, error) {
	f.calls = append(f.calls, call)
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

// kanbanFake builds a one-panel fake with one toggle action.
func kanbanFake() *fakePanelProvider {
	return &fakePanelProvider{
		panels: []core.Panel{{
			ID:      "board",
			Title:   "Kanban board",
			Kind:    "summary",
			Payload: map[string]any{"cards": 3},
			Actions: []core.PanelAction{{ID: "toggle", Label: "Toggle done", Module: "kanban"}},
		}},
		result: "toggled",
	}
}

// panelConsole renders styles unchanged and records errors - the same
// shape the module screen tests use.
type panelConsole struct{ errs []string }

func (c *panelConsole) Labeled(label, body string) {}
func (c *panelConsole) Error(msg string)           { c.errs = append(c.errs, msg) }
func (c *panelConsole) ClearError()                {}
func (c *panelConsole) Style(kind string) core.TUIStyle {
	return func(s string) string { return s }
}
func (c *panelConsole) CWD() string        { return "" }
func (c *panelConsole) Ext() *core.Context { return nil }

// runeKey wraps a rune press for screen driving.
func runeKey(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// panelModel builds a shell model over a context with the given
// providers - the same construction the host's TUI path runs.
func panelModel(t *testing.T, pctx *core.Context) model {
	t.Helper()
	return NewModel(pctx, core.FrontendOptions{})
}

// TestCanvasFeedSubscription covers the in-process spine: a mounted
// provider's Update emits one delta into the feed channel carrying
// the full descriptor, and unknown modules error without a delta.
func TestCanvasFeedSubscription(t *testing.T) {
	fake := kanbanFake()
	feed := openCanvasFeed(&core.Context{PanelProviders: []core.PanelProvider{fake}})
	if feed.err != nil {
		t.Fatalf("feed registration errored: %v", feed.err)
	}
	if !feed.hasPanels() {
		t.Fatal("feed carries no canvas over a mounted provider")
	}

	fake.panels[0].Title = "Kanban board v2"
	if err := feed.canvas.Update("kanban", fake.panels[0]); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got := <-feed.events
	if got.ID != "board" || got.Title != "Kanban board v2" {
		t.Fatalf("delta = %+v, want board/Kanban board v2", got)
	}
	if len(got.Actions) != 1 || got.Actions[0].Module != "kanban" {
		t.Fatalf("delta lost the descriptor actions: %+v", got.Actions)
	}

	if err := feed.canvas.Update("ghost", fake.panels[0]); !errors.Is(err, core.ErrUnknownModule) {
		t.Fatalf("unknown module err = %v, want ErrUnknownModule", err)
	}
	select {
	case extra := <-feed.events:
		t.Fatalf("ghost module emitted a delta: %+v", extra)
	default:
	}
}

// TestCanvasFeedWiringBug covers the degraded path: registry errors
// leave the canvas off and keep the shell running with the notice.
func TestCanvasFeedWiringBug(t *testing.T) {
	a := kanbanFake()
	b := kanbanFake() // same module name: a registration wiring bug
	feed := openCanvasFeed(&core.Context{PanelProviders: []core.PanelProvider{a, b}})
	if feed.err == nil {
		t.Fatal("double module registration left no feed error")
	}
	if feed.hasPanels() {
		t.Fatal("wiring bug still built a canvas")
	}
	m := panelModel(t, nil)
	m.canvasFeed = feed
	out, _ := m.handlePanels()
	view := out.(model).screen.(*canvasScreen).View(80, 24)
	if !strings.Contains(view, "canvas unavailable") {
		t.Fatalf("wiring-bug view = %q, want the unavailable notice", view)
	}
}

// TestPanelsScreenRendersRegistration covers the minimal v1 render:
// presence from the registration (Q5a), title, payload view, and the
// numbered actions with module-qualified ids.
func TestPanelsScreenRendersRegistration(t *testing.T) {
	fake := kanbanFake()
	fake.panels[0].Payload = "board: 3 cards open"
	m := panelModel(t, &core.Context{PanelProviders: []core.PanelProvider{fake}})
	out, _ := m.handlePanels()
	view := out.(model).screen.(*canvasScreen).View(80, 24)

	if !strings.Contains(view, "Kanban board") {
		t.Fatalf("view lost the panel title: %q", view)
	}
	if !strings.Contains(view, "board: 3 cards open") {
		t.Fatalf("string payload not rendered as text: %q", view)
	}
	if !strings.Contains(view, "[1] Toggle done") {
		t.Fatalf("view lost the numbered action: %q", view)
	}
	if !strings.Contains(view, "kanban.toggle") {
		t.Fatalf("view lost the module-qualified id: %q", view)
	}
}

// TestCanvasRowsTruncateToWidth: the list-kind renderer's row proof -
// one line per row, key fields only, every line clipped to the width
// with an ellipsis. The JSON dump never appears as the default body.
func TestCanvasRowsTruncateToWidth(t *testing.T) {
	long := strings.Repeat("deploy step ", 12) + "final"
	panels := []core.Panel{{
		ID:    "notifications",
		Title: "Notifications",
		Kind:  "list",
		Payload: map[string]any{
			"unread": 2,
			"rows": []any{
				map[string]any{"id": 7, "kind": "kanban", "source_id": "src-a",
					"title": long, "created_at": "2026-10-24T01:02:03Z", "read": false,
					"action": map[string]any{"action": "mark_read", "label": "mark read", "args": map[string]any{"id": 7}}},
				map[string]any{"id": 6, "kind": "cron", "source_id": "src-b",
					"title": "nightly sync", "read": true},
			},
		},
		Actions: []core.PanelAction{
			{ID: "mark_read", Label: "mark read", Module: "notifications"},
			{ID: "mark_all", Label: "mark all read", Module: "notifications"},
		},
	}}

	// Full fields with room to spare: the row line carries every key
	// field - title, id, kind, read-state, binding hint.
	body := renderCanvasPanels(200, panels)
	if strings.Contains(body, "source_id") || strings.Contains(body, `"created_at"`) {
		t.Fatalf("list body still carries the JSON dump: %q", body)
	}
	if !strings.Contains(body, "2 unread") {
		t.Fatalf("list header lost the unread badge: %q", body)
	}
	if !strings.Contains(body, "● "+long+" #7 · kanban · mark read") {
		t.Fatalf("unread row not rendered with fields: %q", body)
	}
	if !strings.Contains(body, "○ nightly sync #6 · cron") {
		t.Fatalf("read row not rendered with fields: %q", body)
	}

	// At the pane's width the same body clips to the width with an
	// ellipsis; every line fits, and nothing past the width rides.
	body = renderCanvasPanels(28, panels)
	lines := strings.Split(body, "\n")
	for _, l := range lines {
		if got := lipgloss.Width(l); got > 28 {
			t.Fatalf("row line overruns the width: %d %q", got, l)
		}
	}
	if !strings.Contains(body, "● deploy step") || !strings.Contains(body, "…") {
		t.Fatalf("long title not truncated with an ellipsis: %q", body)
	}
	if strings.Contains(body, "#7 · kanban") || strings.Contains(body, "final") {
		t.Fatalf("truncation kept content beyond the width: %q", body)
	}
}

// TestPanelsScreenRawJSONToggle: the JSON dump is a debug view behind
// the 'd' key - default body renders rows, 'd' flips to raw, 'd' back.
func TestPanelsScreenRawJSONToggle(t *testing.T) {
	fake := kanbanFake()
	fake.panels[0].Kind = "list"
	fake.panels[0].Payload = map[string]any{
		"unread": 1,
		"rows": []any{
			map[string]any{"id": 3, "kind": "kanban", "source_id": "src-c", "title": "row three", "read": false},
		},
	}
	m := panelModel(t, &core.Context{PanelProviders: []core.PanelProvider{fake}})
	out, _ := m.handlePanels()
	cs := out.(model).screen.(*canvasScreen)

	view := cs.View(80, 24)
	if !strings.Contains(view, "· 1 unread") {
		t.Fatalf("default body not the rows view: %q", view)
	}
	if !strings.Contains(view, "● row three #3 · kanban") {
		t.Fatalf("unread row missing from the rows view: %q", view)
	}
	if strings.Contains(view, "source_id") {
		t.Fatalf("raw JSON visible before 'd': %q", view)
	}

	cs.Update(runeKey("d"), &panelConsole{})
	raw := cs.View(80, 24)
	if !strings.Contains(raw, `"source_id"`) {
		t.Fatalf("'d' did not flip to the raw JSON view: %q", raw)
	}
	if !strings.Contains(raw, "1-9 invoke · j/k scroll · d raw · q/esc close") {
		t.Fatalf("hint footer lost the raw key: %q", raw)
	}

	cs.Update(runeKey("d"), &panelConsole{})
	if got := cs.View(80, 24); strings.Contains(got, "source_id") || !strings.Contains(got, "● row three") {
		t.Fatalf("second 'd' did not flip back to rows: %q", got)
	}
}

// kanbanListFake builds a fake spine carrying the arc 5 kanban task
// shape: string row ids, a status field, no unread - the same field
// tags modules/kanban ships on the wire, no kanban import (layer law).
func kanbanListFake() *fakePanelProvider {
	return &fakePanelProvider{
		panels: []core.Panel{{
			ID:    "tasks",
			Title: "Tasks",
			Kind:  "list",
			Payload: map[string]any{
				"board": "build",
				"total": 2,
				"rows": []any{
					map[string]any{"id": "t_abc123", "status": "running", "title": "ship the decoder",
						"assignee": "cody", "created_at": "2026-10-24T01:02:03Z",
						"action": map[string]any{"action": "complete_task", "label": "complete",
							"args": map[string]any{"id": "t_abc123"}}},
					map[string]any{"id": "t_def456", "status": "done", "title": "settle the shape",
						"assignee": "cody", "created_at": "2026-10-24T00:00:00Z"},
				},
			},
			Actions: []core.PanelAction{{ID: "complete_task", Label: "complete", Module: "kanban"}},
		}},
		result: "completed",
	}
}

// TestCanvasKanbanListRowsRender: the arc 5 kanban task shape (string
// ids, status, no unread) renders one line per row at the default
// body - no JSON dump. Marker fallback is status-derived: settled rows
// read, live rows unread with the binding hint. The total badge rides
// the header, 'd' still flips the dump, truncation still holds.
func TestCanvasKanbanListRowsRender(t *testing.T) {
	fake := kanbanListFake()
	m := panelModel(t, &core.Context{PanelProviders: []core.PanelProvider{fake}})
	out, _ := m.handlePanels()
	cs := out.(model).screen.(*canvasScreen)

	view := cs.View(80, 24)
	if !strings.Contains(view, "· 2 total") {
		t.Fatalf("header lost the total badge: %q", view)
	}
	if !strings.Contains(view, "● ship the decoder t_abc123 · running · complete") {
		t.Fatalf("live kanban row not rendered: %q", view)
	}
	if !strings.Contains(view, "○ settle the shape t_def456 · done") {
		t.Fatalf("settled kanban row not rendered: %q", view)
	}
	if strings.Contains(view, `"assignee"`) || strings.Contains(view, `"board"`) {
		t.Fatalf("kanban body still the JSON dump: %q", view)
	}

	// 'd' still flips to the raw dump and back over a kanban payload.
	cs.Update(runeKey("d"), &panelConsole{})
	raw := cs.View(80, 24)
	if !strings.Contains(raw, `"assignee"`) {
		t.Fatalf("'d' did not flip to the raw JSON view: %q", raw)
	}
	cs.Update(runeKey("d"), &panelConsole{})
	if got := cs.View(80, 24); strings.Contains(got, `"assignee"`) || !strings.Contains(got, "● ship the decoder") {
		t.Fatalf("second 'd' did not flip back to rows: %q", got)
	}

	// Truncation still holds: narrow lines clip to the width with an
	// ellipsis.
	body := renderCanvasPanels(24, fake.panels)
	for _, l := range strings.Split(body, "\n") {
		if got := lipgloss.Width(l); got > 24 {
			t.Fatalf("row line overruns the width: %d %q", got, l)
		}
	}
	if !strings.Contains(body, "…") {
		t.Fatalf("narrow body lost the ellipsis: %q", body)
	}
}

// TestCanvasBoardsSwitcherStaysJSON: the board switcher payload shares
// no vocabulary field with the wire row (name/current/active only, no
// title, no id, no status). The decode does not force it - the
// switcher keeps the JSON dump as its default body.
func TestCanvasBoardsSwitcherStaysJSON(t *testing.T) {
	panels := []core.Panel{{
		ID:    "boards",
		Title: "Boards",
		Kind:  "list",
		Payload: map[string]any{
			"active": "main",
			"rows": []any{
				map[string]any{"name": "main", "current": true, "active": true},
				map[string]any{"name": "spike", "current": false, "active": false,
					"action": map[string]any{"action": "switch_board", "label": "switch"}},
			},
		},
	}}
	body := renderCanvasPanels(80, panels)
	if !strings.Contains(body, `"name": "spike"`) {
		t.Fatalf("switcher body lost its JSON dump: %q", body)
	}
	if strings.Contains(body, "● ") || strings.Contains(body, "○ ") {
		t.Fatalf("board rows were forced through the row vocabulary: %q", body)
	}
}

// TestToastPanelRendersRowLine: the toast body is the notice row, not
// a JSON dump of one record.
func TestToastPanelRendersRowLine(t *testing.T) {
	fake := kanbanFake()
	fake.panels[0].Kind = "toast"
	fake.panels[0].Payload = map[string]any{
		"id": 6, "kind": "cron", "source_id": "nightly-insights",
		"title": "insights run finished: 12 sessions", "read": true,
	}
	m := panelModel(t, &core.Context{PanelProviders: []core.PanelProvider{fake}})
	out, _ := m.handlePanels()
	view := out.(model).screen.(*canvasScreen).View(80, 24)
	if !strings.Contains(view, "○ insights run finished: 12 sessions #6 · cron") {
		t.Fatalf("toast body not the notice row: %q", view)
	}
	if strings.Contains(view, "source_id") {
		t.Fatalf("toast body dumped the record JSON: %q", view)
	}
	// An empty toast payload (nothing emitted yet) degrades to the
	// plain branch instead of a fake row.
	empty := core.Panel{ID: "toast", Title: "Toast", Kind: "toast"}
	body := renderCanvasPanels(80, []core.Panel{empty})
	if body != strings.TrimSpace("Toast  [toast]") {
		t.Fatalf("empty toast body = %q", body)
	}
}

// TestPanelsScreenJSONPayload covers the non-string, non-list payload
// branch: plain v1 renders it as indented JSON even with a kind set.
func TestPanelsScreenJSONPayload(t *testing.T) {
	fake := kanbanFake()
	m := panelModel(t, &core.Context{PanelProviders: []core.PanelProvider{fake}})
	out, _ := m.handlePanels()
	view := out.(model).screen.(*canvasScreen).View(80, 24)
	if !strings.Contains(view, `"cards": 3`) {
		t.Fatalf("JSON payload not rendered: %q", view)
	}
}

// TestPanelsScreenNoPanels covers the empty registration (Q5a: the
// snapshot lists whatever is mounted - here nothing).
func TestPanelsScreenNoPanels(t *testing.T) {
	m := panelModel(t, &core.Context{})
	out, _ := m.handlePanels()
	view := out.(model).screen.(*canvasScreen).View(80, 24)
	if !strings.Contains(view, "(no panels mounted)") {
		t.Fatalf("empty view = %q", view)
	}
}

// TestPanelsScreenActionRoundTrip drives the numbered key through the
// whole pipe at both levels: the screen's own Update and the shell's
// key routing to the open screen. The call carries the
// module-qualified id, no args (v1 bindings).
func TestPanelsScreenActionRoundTrip(t *testing.T) {
	fake := kanbanFake()
	fake.result = map[string]any{"toggled": true}
	m := panelModel(t, &core.Context{PanelProviders: []core.PanelProvider{fake}})
	out, _ := m.handlePanels()
	shell := out.(model)
	cs := shell.screen.(*canvasScreen)

	// Level 1: the screen's own Update.
	cs.Update(runeKey("1"), &panelConsole{})
	if len(fake.calls) != 1 {
		t.Fatalf("handler got %d calls, want 1", len(fake.calls))
	}
	call := fake.calls[0]
	if call.Module != "kanban" || call.Action != "toggle" || len(call.Args) != 0 {
		t.Fatalf("call = %+v, want kanban/toggle/no args", call)
	}
	view := cs.View(80, 24)
	if !strings.Contains(view, "kanban.toggle: ok") {
		t.Fatalf("result line missing: %q", view)
	}
	if !strings.Contains(view, `"toggled": true`) {
		t.Fatalf("result value missing: %q", view)
	}

	// Level 2: the shell itself routes the key to the open screen -
	// the keybinding law the whole TUI obeys.
	fake.calls = nil
	shell.routeModuleScreen(runeKey("1"))
	if len(fake.calls) != 1 || fake.calls[0].Action != "toggle" {
		t.Fatalf("shell routing missed the screen: calls = %+v", fake.calls)
	}
}

// TestPanelsScreenHandlerError surfaces a handler error verbatim in
// the result line and the status line.
func TestPanelsScreenHandlerError(t *testing.T) {
	fake := kanbanFake()
	fake.err = errors.New("board locked")
	m := panelModel(t, &core.Context{PanelProviders: []core.PanelProvider{fake}})
	out, _ := m.handlePanels()
	cs := out.(model).screen.(*canvasScreen)
	pc := &panelConsole{}
	cs.Update(runeKey("1"), pc)
	view := cs.View(80, 24)
	if !strings.Contains(view, "kanban.toggle: board locked") {
		t.Fatalf("handler error not surfaced: %q", view)
	}
	if len(pc.errs) == 0 || !strings.Contains(pc.errs[0], "board locked") {
		t.Fatalf("console error records = %+v", pc.errs)
	}
}

// TestPanelsScreenUnboundDigit covers the no-canvas and out-of-range
// bindings: the view keeps its result line instead of panicking.
func TestPanelsScreenUnboundDigit(t *testing.T) {
	// no canvas: opening over a bare model, the dispatch is nil.
	s := openCanvasScreen(model{}, &panelConsole{})
	s.invokeAction("1", &panelConsole{})
	if s.result != "no canvas mounted" || !s.failed {
		t.Fatalf("no-canvas result = %q (failed=%v)", s.result, s.failed)
	}

	// out of range over a live canvas: an actionless provider
	// registers silently (arc 3a), so nothing is bound.
	fake := kanbanFake()
	fake.panels[0].Actions = nil
	feed := openCanvasFeed(&core.Context{PanelProviders: []core.PanelProvider{fake}})
	m := model{canvasFeed: feed}
	out, _ := m.handlePanels()
	pc := &panelConsole{}
	out.(model).screen.(*canvasScreen).Update(runeKey("1"), pc)
	if len(pc.errs) == 0 || !strings.Contains(pc.errs[0], "no bound action 1") {
		t.Fatalf("unbound digit errs = %+v", pc.errs)
	}
}

// TestPanelsScreenClose is the screen seam law: q/esc closes.
func TestPanelsScreenClose(t *testing.T) {
	m := panelModel(t, &core.Context{})
	out, _ := m.handlePanels()
	cs := out.(model).screen.(*canvasScreen)
	cs.Update(runeKey("q"), &panelConsole{})
	if !cs.Closed() {
		t.Fatal("q left the screen open")
	}
}

// TestTUIPanelRoundTrip is the full probe: a shell model built the
// host way over a fake spine has the Init arm armed, sees a runtime
// delta on its in-process subscription, repaints the open view, and
// round-trips an action to the capsule handler.
func TestTUIPanelRoundTrip(t *testing.T) {
	fake := kanbanFake()
	m := panelModel(t, &core.Context{PanelProviders: []core.PanelProvider{fake}})

	if m.Init() == nil {
		t.Fatal("Init returned no cmd batch")
	}

	// Open /panels, then drive a delta: the run must continue on the
	// model the shell actually keeps (handlePanels returns the copy
	// bubbletea would carry); the runtime Update feeds the channel,
	// the drain cmd surfaces it into the loop, the re-arm keeps the
	// subscription alive.
	out, _ := m.handlePanels()
	shell := out.(model)
	fake.panels[0].Title = "Kanban board (live)"
	fake.panels[0].Payload = "board: 4 cards open"
	if err := m.canvasFeed.canvas.Update("kanban", fake.panels[0]); err != nil {
		t.Fatalf("Update: %v", err)
	}
	msg := m.waitForCanvas()()
	delta, ok := msg.(canvasDeltaMsg)
	if !ok {
		t.Fatalf("drain msg = %T, want canvasDeltaMsg", msg)
	}
	if delta.panel.ID != "board" {
		t.Fatalf("delta panel = %+v", delta.panel)
	}
	out2, arm := shell.Update(msg)
	if arm == nil {
		t.Fatal("delta Update left no re-arm")
	}
	shell2 := out2.(model)
	if shell2.screen == nil {
		t.Fatal("delta Update closed the screen")
	}
	view := shell2.screen.View(80, 24)
	if !strings.Contains(view, "Kanban board (live)") || !strings.Contains(view, "board: 4 cards open") {
		t.Fatalf("open view did not repaint from the delta: %q", view)
	}

	// Invoke the action against the updated state: the pipe routes the
	// module-qualified call to the capsule handler.
	shell2.screen.Update(runeKey("1"), &panelConsole{})
	if len(fake.calls) != 1 || fake.calls[0].Action != "toggle" {
		t.Fatalf("round-trip calls = %+v", fake.calls)
	}
}

// TestToastLineFromCanvasDelta: the toast panel (arc 4b) renders its
// payload row through the wire shape - title rides the bar, empty
// payload and wrong kinds are plain panel deltas. The failed verdict
// drives the longer lifetime.
func TestToastLineFromCanvasDelta(t *testing.T) {
	row := map[string]any{"title": "task t9 done", "verdict": "ok"}
	p := core.Panel{ID: "toast", Title: "Toast", Kind: "toast", Payload: row}
	line, failed := toastLine(p)
	if line != "task t9 done" || failed {
		t.Fatalf("ok toast: %q failed=%v", line, failed)
	}
	p.Payload = map[string]any{"title": "cron n2 finished", "verdict": "failed"}
	if line, failed = toastLine(p); line != "cron n2 finished" || !failed {
		t.Fatalf("failed toast: %q failed=%v", line, failed)
	}
	// a list panel is a persistent surface, not a toast
	p = core.Panel{ID: "notifications", Kind: "list", Payload: row}
	if line, _ := toastLine(p); line != "" {
		t.Fatalf("list panel must not toast: %q", line)
	}
	// empty toast payload (nothing emitted yet): plain delta path
	p = core.Panel{ID: "toast", Kind: "toast"}
	if line, _ := toastLine(p); line != "" {
		t.Fatalf("empty toast must render nothing: %q", line)
	}
}

// TestCanvasDeltaToastRunsStatusLine: the model Update consumes a toast
// delta - the status line carries the notice until the tick clears it.
func TestCanvasDeltaToastRunsStatusLine(t *testing.T) {
	m := model{}
	p := core.Panel{ID: "toast", Title: "Toast", Kind: "toast",
		Payload: map[string]any{"title": "chat s1 finished", "verdict": "ok"}}
	next, cmd := m.Update(canvasDeltaMsg{panel: p})
	m = next.(model)
	if !strings.Contains(m.statusLine(), "toast: chat s1 finished") {
		t.Fatalf("toast did not land on the status line: %q", m.statusLine())
	}
	if cmd == nil {
		t.Fatalf("toast must arm its expire tick")
	}
	// the drain re-arms in the same batch: the tick batch carries both
	next, cmd2 := m.Update(toastExpiredMsg{})
	m = next.(model)
	if strings.Contains(m.statusLine(), "toast:") {
		t.Fatalf("expired toast still on the status line: %q", m.statusLine())
	}
	if cmd2 != nil {
		t.Fatalf("expiry carries no arm")
	}
}
