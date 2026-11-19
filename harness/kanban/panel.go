package kanban

// The canvas surface: the kanban capsule ships its view-model as canvas panels. A nil
// sink or a failed push never fails the mutation: the store is already updated.

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/EndoTheDev/omega/core"
)

// Panel ids and action ids of the capsule.
const (
	listPanelID    = "tasks"
	boardsPanelID  = "boards"
	actionComplete = "kanban.complete_task"
	actionSwitch   = "kanban.switch_board"
)

// panelRows caps the task panel's payload.
const panelRows = 20

// rowAction is one action binding a row carries.
type rowAction struct {
	Action string `json:"action"`
	Label  string `json:"label"`
	Args   any    `json:"args,omitempty"`
}

// taskRow is one task-panel line.
type taskRow struct {
	ID        string     `json:"id"`
	Status    string     `json:"status"`
	Title     string     `json:"title"`
	Assignee  string     `json:"assignee"`
	CreatedAt string     `json:"created_at"`
	RowAction *rowAction `json:"action,omitempty"`
}

// taskListPayload is the task panel's view-model.
type taskListPayload struct {
	Board string    `json:"board"`
	Total int       `json:"total"`
	Rows  []taskRow `json:"rows"`
}

// boardRow is one board-switcher line.
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

// capsule is the kanban module's canvas surface.
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

// SetPanelUpdate installs the canvas closure (core.PanelUpdateSink). The closure is a
// routing decision the runtime owns; nil means no canvas built (or none left after a
// re-registration) - pushes stay silent and frontends catch up on their next snapshot.
func (c *capsule) SetPanelUpdate(update func(core.Panel) error) {
	c.mu.Lock()
	c.update = update
	c.mu.Unlock()
}

// push routes one panel through the installed closure.
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

// Panels declares the snapshot.
func (c *capsule) Panels() []core.Panel {
	return []core.Panel{c.taskPanel(), c.boardsPanel()}
}

// displayBoard names the board a board-less view-model line refers.
func displayBoard(board string) string {
	if board == "" {
		return DefaultBoardName
	}
	return board
}

// taskPanel builds the task panel descriptor from the live store.
func (c *capsule) taskPanel() core.Panel {
	board := c.activeBoard()
	tasks, err := c.store.ListTasks(context.Background(), board, "")
	if err != nil {
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

// boardsPanel builds the switcher descriptor.
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

// refreshTasks pushes a fresh task panel.
func (c *capsule) refreshTasks() { c.push(c.taskPanel()) }

// refreshAll pushes both panels.
func (c *capsule) refreshAll() {
	c.push(c.taskPanel())
	c.push(c.boardsPanel())
}

// HandleAction routes one action-pipe call (core.PanelProvider).
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
		if !c.store.BoardExists(name) {
			return nil, fmt.Errorf("no such board %q", name)
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
