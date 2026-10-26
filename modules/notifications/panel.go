package notifications

// The canvas surface (arc 4b, t_9339ad05): the notifications capsule
// ships its view-model as canvas panels - the list panel (rows with
// mark-read actions through the action pipe) and the toast panel (the
// toast as its own panel/event). Emitters (kanban, cron, delegate)
// call Emit on the seam; the mounted seam implementation pushes both
// panels through the update sink the canvas runtime installed at
// registration. The toast is a canvas event on the one spine - both
// frontends render from the canvas, no per-frontend toast code (settled
// answer 2). The REST surface (GET /notifications, POST
// /notifications/read, SSE /notifications/events) stays where it is
// (settled Q6a); the panel surface is additive. The capsule law is the
// whole point: removing the module removes the panels, the toast
// events and this emitter wiring together - the registration is the
// plugin, nothing else holds a reference (settled answer 3).

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/EndoTheDev/omega/core"
)

// Panel ids and action ids of the capsule. Panel ids keep their
// natural shape; action ids are machine-facing and namespaced pairs
// (notifications.verb, Q10 - the registration API claims each one,
// v0.12 phase C2). core.PanelAction.Module carries "notifications".
const (
	listPanelID  = "notifications"
	toastPanelID = "toast"
	actionRead   = "notifications.mark_read"
	actionAll    = "notifications.mark_all"
)

// listPanelRows caps the list panel's payload: the view-model shows
// the newest rows, the store keeps the history.
const listPanelRows = 20

// rowAction is one action binding a row carries: the action-pipe call
// the renderer should make for this row. The generic renderers stay
// dumb - they render the declared label and route the declared action
// and args; nothing in a renderer knows the word "mark".
type rowAction struct {
	Action string `json:"action"`
	Label  string `json:"label"`
	Args   any    `json:"args,omitempty"`
}

// row is one list-panel line: the record fields verbatim plus the row's
// action binding when the record is unread (a read row needs no
// action). The binding name is the generic vocabulary this migration
// introduces: rows may carry one action binding, renderers render the
// declared label and route the declared call.
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

// listPayload is the list panel's view-model: the newest rows and the
// unread count over the whole set (the badge, not the page length).
type listPayload struct {
	Unread int   `json:"unread"`
	Rows   []row `json:"rows"`
}

// capsule is the notifications module's canvas surface. It is the
// PanelProvider the plugin appends to Context.PanelProviders during
// Mount - the registration IS the plugin (an unregistered plugin ships
// no panels, no actions, no toast events, no emitter wiring). It also
// implements core.PanelUpdateSink: the canvas runtime installs the
// update closure at registration with the capsule's own module
// namespace, and every seam emit pushes through it.
type capsule struct {
	store *Store

	mu     sync.Mutex
	update func(core.Panel) error
}

var _ core.PanelProvider = (*capsule)(nil)
var _ core.PanelUpdateSink = (*capsule)(nil)

func newCapsule(s *Store) *capsule { return &capsule{store: s} }

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
// or a failed push is not an emit failure: the emit already recorded,
// the snapshot is the catch-up (the same law the delta fan-out obeys).
func (c *capsule) push(p core.Panel) {
	c.mu.Lock()
	up := c.update
	c.mu.Unlock()
	if up == nil {
		return
	}
	_ = up(p)
}

// emittingStore is the seam implementation the plugin mounts: the
// NotificationsProvider face the emitters (kanban, cron, delegate) and
// the HTTP channel hold. Every emit records through the store and
// pushes the capsule's panels - the emitter wiring in the module
// (capsule law, settled answer 3). Mark* go straight through; the
// action pipe carries the canvas-side mutations, the REST routes keep
// theirs (settled Q6a), and neither path needs to notify the other's
// consumers (the snapshot is the catch-up).
type emittingStore struct {
	store   *Store
	capsule *capsule
}

var _ core.NotificationsProvider = (*emittingStore)(nil)

// Emit records one notification and pushes the canvas surface: the
// toast panel carries the new record, the list panel refreshes. The
// push is best-effort by the capsule's law - a nil sink or a failed
// route never fails the emit (the record is already in the store).
func (e *emittingStore) Emit(n core.Notification) (core.Notification, error) {
	got, err := e.store.Emit(n)
	if err == nil {
		e.capsule.pushEmit(got)
	}
	return got, err
}

func (e *emittingStore) List(ctx context.Context, unreadOnly bool, limit, offset int) ([]core.Notification, int, error) {
	return e.store.List(ctx, unreadOnly, limit, offset)
}

func (e *emittingStore) MarkRead(ctx context.Context, id int64) error {
	return e.store.MarkRead(ctx, id)
}

func (e *emittingStore) MarkAllRead(ctx context.Context) error {
	return e.store.MarkAllRead(ctx)
}

func (e *emittingStore) Subscribe() (<-chan core.Notification, func()) {
	return e.store.Subscribe()
}

// Panels declares the snapshot: the list panel and the toast panel,
// both seeded with the store's current state at registration.
func (c *capsule) Panels() []core.Panel {
	return []core.Panel{c.listPanel(), c.toastPanel()}
}

// listPanel builds the list panel descriptor from the live store.
func (c *capsule) listPanel() core.Panel {
	notes, unread, err := c.store.List(context.Background(), false, listPanelRows, 0)
	if err != nil {
		// A dead store serves an empty panel: the surface degrades,
		// the seam error surfaces through the REST routes instead.
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

// toastPanel builds the toast descriptor: the newest record as its own
// panel/event (settled answer 1). Empty payload = nothing to toast;
// the panel still exists so the snapshot shape is stable.
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

// noteRow strips a record to its view-model row (no mark binding -
// the toast is a notice, not a control surface; the list panel holds
// the actions).
func noteRow(n core.Notification) row {
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

// pushEmit is the emitter wiring: one recorded emit pushes the toast
// panel (the canvas event) and refreshes the list panel (the new row
// and unread badge) - both on the sink the runtime installed. Called
// from the mounted seam implementation, not from the store.
func (c *capsule) pushEmit(n core.Notification) {
	c.push(c.toastPanelFor(n))
	c.push(c.listPanel())
}

// toastPanelFor builds the toast descriptor for an emitted record
// directly from the record - a push carries the record verbatim
// instead of re-reading the store (the store's newest row is the same
// record only until the next emit lands).
func (c *capsule) toastPanelFor(n core.Notification) core.Panel {
	return core.Panel{
		ID:      toastPanelID,
		Title:   "Toast",
		Kind:    "toast",
		Payload: noteRow(n),
	}
}

// HandleAction routes one action-pipe call (core.PanelProvider). The
// runtime checked module and action id already; both actions mutate
// the store and then refresh the list panel through the sink so every
// canvas consumer converges on the same state.
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
