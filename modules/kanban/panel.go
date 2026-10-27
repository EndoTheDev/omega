package kanban

// The canvas surface (arc 5, t_7bd3dc06): the kanban capsule ships its
// view-model as canvas panels - the task list panel (rows with a
// complete action through the action pipe) and the board switcher
// panel (rows with a switch action). Both frontends render from the
// canvas; nothing outside the module holds a reference. The REST
// surface (GET/POST /boards, GET/PATCH /tasks, the threads) stays
// where it is (settled Q6a) and the panel surface is additive.
//
// The push law is the notifications capsule's law (arc 4b): mutations
// that run through the module's own faces (the task tools, the /board
// command) refresh the panels through the sink the canvas runtime
// installed at registration. Mutations from outside the module (an
// HTTP PATCH from a frontend pressing its own board UI) do not push -
// the snapshot is the catch-up, the same law the delta fan-out obeys.
// A nil sink or a failed push never fails the mutation: the store is
// already updated.

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/EndoTheDev/omega/core"
)

// Panel ids and action ids of the capsule. Panel ids are natural
// (user-facing view names); action ids are machine-facing and carry
// the module.verb namespace (Q10, enforced at registration -
// core.PanelAction.Module names the tenant on the wire).
const (
	listPanelID    = "tasks"
	boardsPanelID  = "boards"
	actionComplete = "kanban.complete_task"
	actionSwitch   = "kanban.switch_board"
)

// panelRows caps the task panel's payload: the view-model shows the
// newest rows (the store orders created_at DESC), the store keeps the
// history.
const panelRows = 20

// rowAction is one action binding a row carries: the action-pipe call
// the renderer should make for this row. The generic renderers stay
// dumb - they render the declared label and route the declared action
// and args; nothing in a renderer knows the word "complete" (the same
// vocabulary arc 4b introduced, reused per the settled shapes).
type rowAction struct {
	Action string `json:"action"`
	Label  string `json:"label"`
	Args   any    `json:"args,omitempty"`
}

// taskRow is one task-panel line: the task fields verbatim plus the
// row's complete binding while the task is unsettled (a done or
// archived row needs no action).
type taskRow struct {
	ID        string     `json:"id"`
	Status    string     `json:"status"`
	Title     string     `json:"title"`
	Assignee  string     `json:"assignee"`
	CreatedAt string     `json:"created_at"`
	RowAction *rowAction `json:"action,omitempty"`
}

// taskListPayload is the task panel's view-model: the active board it
// covers and the newest rows over that board (the badge is the total).
type taskListPayload struct {
	Board string    `json:"board"`
	Total int       `json:"total"`
	Rows  []taskRow `json:"rows"`
}

// boardRow is one board-switcher line: the registry fields verbatim
// (the pointer marker) plus the session's active marker and the row's
// switch binding when the board is not the active one.
type boardRow struct {
	Name      string     `json:"name"`
	Current   bool       `json:"current"`
	Active    bool       `json:"active"`
	RowAction *rowAction `json:"action,omitempty"`
}

// boardsPayload is the switcher's view-model.
type boardsPayload struct {
	Active string     `json:"active"`
	Rows   []boardRow `json:"rows"`
}

// capsule is the kanban module's canvas surface. It is the
// PanelProvider the plugin appends to Context.PanelProviders during
// Mount - the registration IS the plugin (an unregistered plugin
// ships no panels, no actions, no push wiring; the tools and the
// /board command are the same Mount's face). It also implements
// core.PanelUpdateSink: the canvas runtime installs the update
// closure at registration with the capsule's own module namespace,
// and every module-face mutation pushes through it. The board
// accessor and setter are the plugin's session board (the tools'
// active board) - the panel mirrors what the tools and the command
// already agree on, it does not keep a second truth.
type capsule struct {
	store    *Store
	board    func() string
	setBoard func(string)

	mu     sync.Mutex
	update func(core.Panel) error
}

var _ core.PanelProvider = (*capsule)(nil)
var _ core.PanelUpdateSink = (*capsule)(nil)

// newCapsule builds the capsule over the store and the session board.
func newCapsule(s *Store, board func() string, setBoard func(string)) *capsule {
	return &capsule{store: s, board: board, setBoard: setBoard}
}

// SetPanelUpdate installs the canvas closure (core.PanelUpdateSink).
// The closure is a routing decision the runtime owns; nil means no
// canvas built (or none left after a re-registration) - pushes stay
// silent and frontends catch up on their next snapshot.
func (c *capsule) SetPanelUpdate(update func(core.Panel) error) {
	c.mu.Lock()
	c.update = update
	c.mu.Unlock()
}

// push routes one panel through the installed closure. A missing sink
// or a failed push is not a mutation failure: the store already
// recorded, the snapshot is the catch-up.
func (c *capsule) push(p core.Panel) {
	c.mu.Lock()
	up := c.update
	c.mu.Unlock()
	if up == nil {
		return
	}
	_ = up(p)
}

// activeBoard returns the session's active board ("" = the default
// board). Nil-safe: the accessor is nil only before the plugin wired
// it, and a nil accessor just means the default board.
func (c *capsule) activeBoard() string {
	if c.board == nil {
		return ""
	}
	return c.board()
}

// Panels declares the snapshot: the task list and the board
// switcher, both seeded with the store's current state at
// registration.
func (c *capsule) Panels() []core.Panel {
	return []core.Panel{c.taskPanel(), c.boardsPanel()}
}

// displayBoard names the board a board-less view-model line refers to.
func displayBoard(board string) string {
	if board == "" {
		return DefaultBoardName
	}
	return board
}

// taskPanel builds the task panel descriptor from the live store: the
// session's active board, its newest tasks, a complete binding on
// every unsettled row.
func (c *capsule) taskPanel() core.Panel {
	board := c.activeBoard()
	tasks, err := c.store.ListTasks(context.Background(), board, "")
	if err != nil {
		// A dead store serves an empty panel: the surface degrades,
		// the seam error surfaces through the REST routes instead.
		return core.Panel{ID: listPanelID, Title: "Tasks", Kind: "list"}
	}
	rows := make([]taskRow, 0, len(tasks))
	for _, t := range tasks {
		if len(rows) == panelRows {
			break
		}
		r := taskRow{
			ID:        t.ID,
			Status:    t.Status,
			Title:     t.Title,
			Assignee:  t.Assignee,
			CreatedAt: t.CreatedAt,
		}
		if !IsSettled(t.Status) {
			r.RowAction = &rowAction{Action: actionComplete, Label: "complete", Args: map[string]any{"id": t.ID}}
		}
		rows = append(rows, r)
	}
	return core.Panel{
		ID:      listPanelID,
		Title:   "Tasks",
		Kind:    "list",
		Payload: taskListPayload{Board: displayBoard(board), Total: len(tasks), Rows: rows},
		Actions: []core.PanelAction{
			{ID: actionComplete, Label: "complete", Module: "kanban"},
		},
	}
}

// boardsPanel builds the switcher descriptor: the registry (the
// pointer marker), the session's active board marked, a switch
// binding on every row that is not the active one.
func (c *capsule) boardsPanel() core.Panel {
	active := displayBoard(c.activeBoard())
	boards, err := c.store.Boards(context.Background())
	if err != nil {
		return core.Panel{ID: boardsPanelID, Title: "Boards", Kind: "list"}
	}
	rows := make([]boardRow, 0, len(boards))
	for _, b := range boards {
		r := boardRow{Name: b.Name, Current: b.Current, Active: b.Name == active}
		if !r.Active {
			r.RowAction = &rowAction{Action: actionSwitch, Label: "switch", Args: map[string]any{"board": b.Name}}
		}
		rows = append(rows, r)
	}
	return core.Panel{
		ID:      boardsPanelID,
		Title:   "Boards",
		Kind:    "list",
		Payload: boardsPayload{Active: active, Rows: rows},
		Actions: []core.PanelAction{
			{ID: actionSwitch, Label: "switch", Module: "kanban"},
		},
	}
}

// refreshTasks pushes a fresh task panel - the wiring the tool ops
// ride (any mutation the session's tools apply shows in the panel).
func (c *capsule) refreshTasks() { c.push(c.taskPanel()) }

// refreshAll pushes both panels - the wiring the /board switch rides
// (the active marker in the switcher AND the covered board in the
// task panel both move together).
func (c *capsule) refreshAll() {
	c.push(c.taskPanel())
	c.push(c.boardsPanel())
}

// HandleAction routes one action-pipe call (core.PanelProvider). The
// runtime checked module and action id already. Complete mutates the
// store and refreshes the task panel; switch validates the board the
// same way /board does and refreshes both panels - every canvas
// consumer converges on the same state.
func (c *capsule) HandleAction(call core.ActionCall) (any, error) {
	switch call.Action {
	case actionComplete:
		var args struct {
			ID *string `json:"id"`
		}
		if len(call.Args) > 0 {
			if err := json.Unmarshal(call.Args, &args); err != nil {
				return nil, fmt.Errorf("complete task: %v", err)
			}
		}
		if args.ID == nil || *args.ID == "" {
			return nil, fmt.Errorf("complete task: id is required")
		}
		if _, err := c.store.UpdateTask(context.Background(), c.activeBoard(), *args.ID, KanbanPatch{Status: StatusDone}); err != nil {
			return nil, err
		}
		tp := c.taskPanel()
		c.push(tp)
		return tp.Payload, nil
	case actionSwitch:
		var args struct {
			Board *string `json:"board"`
		}
		if len(call.Args) > 0 {
			if err := json.Unmarshal(call.Args, &args); err != nil {
				return nil, fmt.Errorf("switch board: %v", err)
			}
		}
		if args.Board == nil || *args.Board == "" {
			return nil, fmt.Errorf("switch board: board is required")
		}
		name := *args.Board
		if name != DefaultBoardName && !ValidBoardName(name) {
			return nil, fmt.Errorf("illegal board name %q", name)
		}
		if _, err := c.store.handleFor(name); err != nil {
			return nil, err
		}
		if c.setBoard != nil {
			c.setBoard(name)
		}
		bp := c.boardsPanel()
		c.refreshAll() // task first, then the moved marker
		return bp.Payload, nil
	default:
		return nil, fmt.Errorf("%w: %q", core.ErrUnknownAction, call.Action)
	}
}
