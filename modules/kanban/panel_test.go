package kanban

// The capsule's module tests (arc 5, t_7bd3dc06): the panels declare
// the store's current state, the action pipe mutates through the same
// grammar the tools and /board use, and every module-face mutation
// pushes the matching panel through the installed sink.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// testCapsule builds a capsule over a fresh registry-backed store
// ("main" is the pointer default - the plain single-board mode
// rejects named boards) with a session board the tests can move.
// Returns the capsule and the board setter the way Mount wires it.
func testCapsule(t *testing.T) (*capsule, *string) {
	t.Helper()
	s := testStore(t)
	s.ConfigureBoards(BoardsDir(t.TempDir()), DefaultBoardName, "")
	active := ""
	c := newCapsule(s, func() string { return active }, func(name string) { active = name })
	return c, &active
}

// capture installs the sink closure and records the pushed panels.
func capture(t *testing.T, c *capsule) *[]core.Panel {
	t.Helper()
	var pushed []core.Panel
	c.SetPanelUpdate(func(p core.Panel) error {
		pushed = append(pushed, p)
		return nil
	})
	return &pushed
}

// TestCapsulePanelsDeclare: the snapshot carries both panels, the
// complete binding rides unsettled rows only, and settled rows ship
// without an action.
func TestCapsulePanelsDeclare(t *testing.T) {
	c, _ := testCapsule(t)
	ctx := context.Background()
	open, err := c.store.CreateTask(ctx, "", KanbanTask{Title: "unsettled work"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	done, err := c.store.CreateTask(ctx, "", KanbanTask{Title: "already shipped"})
	if err != nil {
		t.Fatalf("create 2: %v", err)
	}
	if _, err := c.store.UpdateTask(ctx, "", done.ID, KanbanPatch{Status: StatusDone}); err != nil {
		t.Fatalf("close: %v", err)
	}

	panels := c.Panels()
	if len(panels) != 2 {
		t.Fatalf("Panels() = %d panels, want 2", len(panels))
	}
	tp := panels[0]
	if tp.ID != listPanelID || tp.Kind != "list" || tp.Title != "Tasks" {
		t.Fatalf("task panel descriptor wrong: %+v", tp)
	}
	if len(tp.Actions) != 1 || tp.Actions[0] != (core.PanelAction{ID: actionComplete, Label: "complete", Module: "kanban"}) {
		t.Fatalf("task panel actions wrong: %+v", tp.Actions)
	}
	payload, ok := tp.Payload.(taskListPayload)
	if !ok {
		t.Fatalf("task payload is %T, want taskListPayload", tp.Payload)
	}
	if payload.Board != DefaultBoardName || payload.Total != 2 || len(payload.Rows) != 2 {
		t.Fatalf("task payload wrong: %+v", payload)
	}
	// store order is created_at DESC: both draws land in the same
	// second, the id tiebreak descends - the newest is not stable, so
	// find the rows by id.
	actions := map[string]*rowAction{}
	for _, r := range payload.Rows {
		actions[r.ID] = r.RowAction
	}
	if actions[open.ID] == nil || actions[open.ID].Action != actionComplete {
		t.Fatalf("unsettled row carries no complete binding: %+v", actions[open.ID])
	}
	if actions[done.ID] != nil {
		t.Fatalf("done row carries an action binding: %+v", actions[done.ID])
	}

	bp := panels[1]
	if bp.ID != boardsPanelID || bp.Kind != "list" || bp.Title != "Boards" {
		t.Fatalf("boards panel descriptor wrong: %+v", bp)
	}
	if len(bp.Actions) != 1 || bp.Actions[0].Module != "kanban" || bp.Actions[0].ID != actionSwitch {
		t.Fatalf("boards panel actions wrong: %+v", bp.Actions)
	}
	bpayload, ok := bp.Payload.(boardsPayload)
	if !ok {
		t.Fatalf("boards payload is %T, want boardsPayload", bp.Payload)
	}
	if len(bpayload.Rows) != 1 || bpayload.Rows[0].Name != DefaultBoardName {
		t.Fatalf("boards payload wrong: %+v", bpayload)
	}
	if !bpayload.Rows[0].Active {
		t.Fatalf("the only board is the active board: %+v", bpayload)
	}
	if bpayload.Rows[0].RowAction != nil {
		t.Fatalf("active board row carries a switch binding: %+v", bpayload.Rows[0])
	}
}

// TestCapsuleCompleteAction: the pipe completes through the same
// grammar the tools use, pushes the refresh, and returns the payload.
func TestCapsuleCompleteAction(t *testing.T) {
	c, _ := testCapsule(t)
	pushed := capture(t, c)
	ctx := context.Background()
	in, err := c.store.CreateTask(ctx, "", KanbanTask{Title: "wire the drawer"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	payload, err := c.HandleAction(core.ActionCall{
		Module: "kanban",
		Action: actionComplete,
		Args:   json.RawMessage(`{"id": "` + in.ID + `"}`),
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	got, err := c.store.GetTask(ctx, "", in.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != StatusDone {
		t.Fatalf("task status = %q, want done", got.Status)
	}
	pl, ok := payload.(taskListPayload)
	if !ok {
		t.Fatalf("return is %T, want taskListPayload", payload)
	}
	for _, r := range pl.Rows {
		if r.ID == in.ID && r.RowAction != nil {
			t.Fatalf("completed row still carries a binding in the return: %+v", r)
		}
	}

	if len(*pushed) != 1 {
		t.Fatalf("complete pushed %d panels, want 1 task panel", len(*pushed))
	}
	if (*pushed)[0].ID != listPanelID {
		t.Fatalf("pushed %s, want %s", (*pushed)[0].ID, listPanelID)
	}
}

// TestCapsuleCompleteActionErrors: no id is a rejection, a bad action
// is the canvas 404.
func TestCapsuleCompleteActionErrors(t *testing.T) {
	c, _ := testCapsule(t)
	if _, err := c.HandleAction(core.ActionCall{Module: "kanban", Action: actionComplete}); err == nil || !strings.Contains(err.Error(), "id is required") {
		t.Fatalf("missing id error = %v", err)
	}
	if _, err := c.HandleAction(core.ActionCall{Module: "kanban", Action: "fly"}); err == nil {
		t.Fatal("unknown action accepted")
	}
}

// TestCapsuleSwitchAction: the pipe validates the board the way
// /board does, moves the session board, and pushes BOTH panels.
func TestCapsuleSwitchAction(t *testing.T) {
	c, active := testCapsule(t)
	pushed := capture(t, c)

	// the default single-board store: switching to main is legal
	payload, err := c.HandleAction(core.ActionCall{
		Module: "kanban",
		Action: actionSwitch,
		Args:   json.RawMessage(`{"board": "main"}`),
	})
	if err != nil {
		t.Fatalf("switch main: %v", err)
	}
	if *active != DefaultBoardName {
		t.Fatalf("session board = %q, want main", *active)
	}
	bp, ok := payload.(boardsPayload)
	if !ok {
		t.Fatalf("return is %T, want boardsPayload", payload)
	}
	if bp.Active != DefaultBoardName {
		t.Fatalf("payload active = %q, want main", bp.Active)
	}

	// an illegal name is rejected before the store sees it
	if _, err := c.HandleAction(core.ActionCall{
		Module: "kanban",
		Action: actionSwitch,
		Args:   json.RawMessage(`{"board": "../escape"}`),
	}); err == nil || !strings.Contains(err.Error(), "illegal board name") {
		t.Fatalf("illegal name error = %v", err)
	}
	if *active != DefaultBoardName {
		t.Fatalf("a rejected switch moved the session board: %q", *active)
	}

	// a missing board name is a rejection
	if _, err := c.HandleAction(core.ActionCall{Module: "kanban", Action: actionSwitch}); err == nil || !strings.Contains(err.Error(), "board is required") {
		t.Fatalf("missing board error = %v", err)
	}

	// the switch pushes both panels (task panel covers the board,
	// switcher carries the moved marker) - and nothing else in this
	// test pushed: the two rejected switches touched no state
	if len(*pushed) != 2 {
		t.Fatalf("pushed %d panels, want the switch's two", len(*pushed))
	}
	last := (*pushed)[len(*pushed)-1]
	prev := (*pushed)[len(*pushed)-2]
	if last.ID != boardsPanelID || prev.ID != listPanelID {
		t.Fatalf("switch pushed %s then %s, want task then boards", prev.ID, last.ID)
	}
}

// TestCapsuleSwitchOnRegistryBoard: with a second registry board the
// switcher lists both, the non-active row carries the switch binding,
// and switching moves the active marker.
func TestCapsuleSwitchOnRegistryBoard(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	if err := CreateBoard(home, "aux"); err != nil {
		t.Fatal(err)
	}
	s := testStore(t)
	s.ConfigureBoards(BoardsDir(home), "main", "")
	active := "main"
	c := newCapsule(s, func() string { return active }, func(name string) { active = name })

	bp := c.boardsPanel()
	rows, ok := bp.Payload.(boardsPayload)
	if !ok {
		t.Fatalf("payload is %T", bp.Payload)
	}
	if len(rows.Rows) != 2 {
		t.Fatalf("boards = %+v, want main + aux", rows.Rows)
	}
	bindings := map[string]*rowAction{}
	for _, r := range rows.Rows {
		bindings[r.Name] = r.RowAction
	}
	if bindings[active] != nil {
		t.Fatalf("active row carries a switch binding: %+v", bindings[active])
	}
	if bindings["aux"] == nil || bindings["aux"].Action != actionSwitch {
		t.Fatalf("non-active row carries no switch binding: %+v", bindings["aux"])
	}

	if _, err := c.HandleAction(core.ActionCall{
		Module: "kanban",
		Action: actionSwitch,
		Args:   json.RawMessage(`{"board": "aux"}`),
	}); err != nil {
		t.Fatalf("switch aux: %v", err)
	}
	if active != "aux" {
		t.Fatalf("session board = %q, want aux", active)
	}
	after := c.boardsPanel()
	afterRows := after.Payload.(boardsPayload)
	if afterRows.Active != "aux" {
		t.Fatalf("payload active = %q, want aux", afterRows.Active)
	}
	for _, r := range afterRows.Rows {
		if r.Name == "aux" && r.RowAction != nil {
			t.Fatalf("just-activated aux still carries a binding: %+v", r)
		}
		if r.Name == "main" && r.RowAction == nil {
			t.Fatalf("main lost its switch binding after the switch: %+v", r)
		}
	}
}

// TestCapsuleTaskPanelFollowsSessionBoard: the panel lists the
// session's active board, not the default (the switch moves it).
func TestCapsuleTaskPanelFollowsSessionBoard(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	if err := CreateBoard(home, "aux"); err != nil {
		t.Fatal(err)
	}
	s := testStore(t)
	s.ConfigureBoards(BoardsDir(home), "main", "")
	ctx := context.Background()
	if _, err := s.CreateTask(ctx, "aux", KanbanTask{Title: "aux-resident work"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	active := ""
	c := newCapsule(s, func() string { return active }, func(name string) { active = name })

	payload := c.taskPanel().Payload.(taskListPayload)
	if payload.Board != DefaultBoardName || payload.Total != 0 || len(payload.Rows) != 0 {
		t.Fatalf("default view shows aux work: %+v", payload)
	}
	active = "aux"
	payload = c.taskPanel().Payload.(taskListPayload)
	if payload.Board != "aux" || payload.Total != 1 || len(payload.Rows) != 1 || payload.Rows[0].Title != "aux-resident work" {
		t.Fatalf("session board view wrong: %+v", payload)
	}
}

// TestToolRefreshPushes: a tool create refreshes the task panel
// through the capsule (the tools' own face), a failed op refreshes
// the partial-batch state, and /board switch refreshes both.
func TestToolRefreshPushes(t *testing.T) {
	p, _, _ := mountPlugin(t)
	cctx := context.Background()
	pushed := capture(t, p.capsule)

	if _, err := p.tools.runTask(cctx, map[string]any{"action": "create", "title": "push me"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(*pushed) != 1 || (*pushed)[0].ID != listPanelID {
		t.Fatalf("a create pushed %+v, want one task panel", *pushed)
	}
	raw := payloadOf(t, (*pushed)[0].Payload)
	if !strings.Contains(raw, "push me") {
		t.Fatalf("refreshed panel lost the created task: %s", raw)
	}

	// a failed batch op still refreshes (earlier ops applied): the
	// batch is fail-forward, the failure rides the result string
	before := len(*pushed)
	out, err := p.tools.runTask(cctx, map[string]any{"operations": []any{
		map[string]any{"action": "create", "title": "then break"},
		map[string]any{"action": "complete", "id": "t_nope"},
	}})
	if err != nil {
		t.Fatalf("a failed batch op returned an error: %v", err)
	}
	if !strings.Contains(out, "failed at operations[1]") {
		t.Fatalf("the failed op is not reported: %q", out)
	}
	if len(*pushed) <= before {
		t.Fatal("a failed batch op left the panel stale")
	}

	// the /board switch refreshes both panels
	before = len(*pushed)
	if _, err := p.handleCommand("/board", "main"); err != nil {
		t.Fatalf("/board switch: %v", err)
	}
	if len(*pushed) != before+2 {
		t.Fatalf("a switch pushed %d panels, want 2", len(*pushed)-before)
	}
	if (*pushed)[len(*pushed)-1].ID != boardsPanelID || (*pushed)[len(*pushed)-2].ID != listPanelID {
		t.Fatalf("switch push order wrong: %+v %+v", (*pushed)[len(*pushed)-2], (*pushed)[len(*pushed)-1])
	}

	// /boards (the read-only listing) refreshes nothing
	before = len(*pushed)
	if _, err := p.handleCommand("/boards", ""); err != nil {
		t.Fatalf("/boards: %v", err)
	}
	if len(*pushed) != before {
		t.Fatal("read-only /boards pushed a panel")
	}
}

// payloadOf renders a payload through its wire serialization.
func payloadOf(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return string(b)
}

// TestCapsuleSilentWithoutSink: no canvas, no push - the store
// mutation stands, the snapshot is the catch-up.
func TestCapsuleSilentWithoutSink(t *testing.T) {
	c, _ := testCapsule(t)
	ctx := context.Background()
	in, err := c.store.CreateTask(ctx, "", KanbanTask{Title: "quiet work"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := c.HandleAction(core.ActionCall{
		Module: "kanban",
		Action: actionComplete,
		Args:   json.RawMessage(`{"id": "` + in.ID + `"}`),
	}); err != nil {
		t.Fatalf("complete without sink: %v", err)
	}
	got, err := c.store.GetTask(ctx, "", in.ID)
	if err != nil || got.Status != StatusDone {
		t.Fatalf("mutation did not stand without a sink: %+v %v", got, err)
	}
}
