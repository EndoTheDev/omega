package notifications

// The canvas surface: the notifications capsule ships its view-model as canvas panels.

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/EndoTheDev/omega/core"
)

// Panel ids and action ids of the capsule.
const (
	listPanelID  = "notifications"
	toastPanelID = "toast"
	actionRead   = "notifications.mark_read"
	actionAll    = "notifications.mark_all"
)

// listPanelRows caps the list panel's payload.
const listPanelRows = 20

// rowAction is one action binding a row carries.
type rowAction struct {
	Action string `json:"action"`
	Label  string `json:"label"`
	Args   any    `json:"args,omitempty"`
}

// row is one list-panel line.
type row struct {
	ID        int64      `json:"id"`
	Kind      string     `json:"kind"`
	SourceID  string     `json:"source_id"`
	Title     string     `json:"title"`
	Verdict   string     `json:"verdict"`
	CreatedAt string     `json:"created_at"`
	Read      bool       `json:"read"`
	RowAction *rowAction `json:"action,omitempty"`
}

// listPayload is the list panel's view-model.
type listPayload struct {
	Unread int   `json:"unread"`
	Rows   []row `json:"rows"`
}

// capsule is the notifications module's canvas surface.
type capsule struct {
	store *Store

	mu     sync.Mutex
	update func(core.Panel) error
}

var _ core.PanelProvider = (*capsule)(nil)
var _ core.PanelUpdateSink = (*capsule)(nil)

func newCapsule(s *Store) *capsule { return &capsule{store: s} }

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

// emittingStore is the seam implementation the plugin mounts.
type emittingStore struct {
	store   *Store
	capsule *capsule
}

// Emit records one notification and pushes the canvas surface. The push is best-effort
// by the capsule's law - a nil sink or a failed route never fails the emit (the record
// is already in the store).
func (e *emittingStore) Emit(n core.Notice) (core.Notice, error) {
	got, err := e.store.Emit(n)
	if err == nil {
		e.capsule.pushEmit(got)
	}
	return got, err
}

func (e *emittingStore) List(ctx context.Context, unreadOnly bool, limit, offset int) ([]core.Notice, int, error) {
	return e.store.List(ctx, unreadOnly, limit, offset)
}

func (e *emittingStore) MarkRead(ctx context.Context, id int64) error {
	return e.store.MarkRead(ctx, id)
}

func (e *emittingStore) MarkAllRead(ctx context.Context) error {
	return e.store.MarkAllRead(ctx)
}

// Clear drops every record and pushes the refreshed (empty) list panel so canvas.
func (e *emittingStore) Clear(ctx context.Context) (int64, error) {
	n, err := e.store.Clear(ctx)
	if err == nil {
		e.capsule.push(e.capsule.listPanel())
	}
	return n, err
}

func (e *emittingStore) Subscribe() (<-chan core.Notice, func()) {
	return e.store.Subscribe()
}

// Panels declares the snapshot.
func (c *capsule) Panels() []core.Panel {
	return []core.Panel{c.listPanel(), c.toastPanel()}
}

// listPanel builds the list panel descriptor from the live store.
func (c *capsule) listPanel() core.Panel {
	notes, unread, err := c.store.List(context.Background(), false, listPanelRows, 0)
	if err != nil {
		// A dead store serves an empty panel.
		return core.Panel{ID: listPanelID, Title: "Notifications", Kind: "list"}
	}
	rows := make([]row, 0, len(notes))
	for _, n := range notes {
		r := row{
			ID:        n.ID,
			Kind:      n.Kind,
			SourceID:  n.SourceID,
			Title:     n.Title,
			Verdict:   n.Verdict,
			CreatedAt: n.CreatedAt,
			Read:      n.Read,
		}
		if !n.Read {
			r.RowAction = &rowAction{Action: actionRead, Label: "mark read", Args: map[string]any{"id": n.ID}}
		}
		rows = append(rows, r)
	}
	return core.Panel{
		ID:      listPanelID,
		Title:   "Notifications",
		Kind:    "list",
		Payload: listPayload{Unread: unread, Rows: rows},
		Actions: []core.PanelAction{
			{ID: actionRead, Label: "mark read", Module: "notifications"},
			{ID: actionAll, Label: "mark all read", Module: "notifications"},
		},
	}
}

// toastPanel builds the toast descriptor.
func (c *capsule) toastPanel() core.Panel {
	notes, _, err := c.store.List(context.Background(), false, 1, 0)
	if err != nil || len(notes) == 0 {
		return core.Panel{ID: toastPanelID, Title: "Toast", Kind: "toast"}
	}
	return core.Panel{
		ID:      toastPanelID,
		Title:   "Toast",
		Kind:    "toast",
		Payload: noteRow(notes[0]),
	}
}

// noteRow strips a record to its view-model row (no mark binding.
func noteRow(n core.Notice) row {
	return row{
		ID:        n.ID,
		Kind:      n.Kind,
		SourceID:  n.SourceID,
		Title:     n.Title,
		Verdict:   n.Verdict,
		CreatedAt: n.CreatedAt,
		Read:      n.Read,
	}
}

// pushEmit is the emitter wiring.
func (c *capsule) pushEmit(n core.Notice) {
	c.push(c.toastPanelFor(n))
	c.push(c.listPanel())
}

// toastPanelFor builds the toast descriptor for an emitted record directly from the
// record.
func (c *capsule) toastPanelFor(n core.Notice) core.Panel {
	return core.Panel{
		ID:      toastPanelID,
		Title:   "Toast",
		Kind:    "toast",
		Payload: noteRow(n),
	}
}

// HandleAction routes one action-pipe call (core.PanelProvider).
func (c *capsule) HandleAction(call core.ActionCall) (any, error) {
	switch call.Action {
	case actionRead:
		var args struct {
			ID *int64 `json:"id"`
		}
		if len(call.Args) > 0 {
			if err := json.Unmarshal(call.Args, &args); err != nil {
				return nil, fmt.Errorf("mark read: %v", err)
			}
		}
		if args.ID == nil {
			return nil, fmt.Errorf("mark read: id is required")
		}
		if err := c.store.MarkRead(context.Background(), *args.ID); err != nil {
			return nil, err
		}
	case actionAll:
		if err := c.store.MarkAllRead(context.Background()); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("%w: %q", core.ErrUnknownAction, call.Action)
	}
	lp := c.listPanel()
	c.push(lp)
	return lp.Payload, nil
}
