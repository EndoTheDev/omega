package notifications

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// newTestCapsule builds a memory-backed store wrapped in the capsule
// seam exactly the way Mount does.
func newTestCapsule(t *testing.T) (*Store, *capsule, *emittingStore) {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	c := newCapsule(s)
	return s, c, &emittingStore{store: s, capsule: c}
}

// TestCapsulePanelsShape: the provider declares exactly two panels -
// the list with actions registered module-qualified, and the toast -
// and the list payload carries the unread count plus per-row mark
// bindings on unread rows only.
func TestCapsulePanelsShape(t *testing.T) {
	_, _, e := newTestCapsule(t)
	if _, err := e.Emit(core.Notification{Kind: "cron", SourceID: "n1", Title: "cron n1 finished", Verdict: "failed"}); err != nil {
		t.Fatalf("emit: %v", err)
	}
	if _, err := e.Emit(core.Notification{Kind: "task", SourceID: "t1", Title: "task t1 done", Verdict: "ok"}); err != nil {
		t.Fatalf("emit 2: %v", err)
	}

	var capsule core.PanelProvider = newCapsule(e.store)
	panels := capsule.Panels()
	if len(panels) != 2 {
		t.Fatalf("_panels: got %d, want 2", len(panels))
	}
	var list, toast core.Panel
	for _, p := range panels {
		switch p.ID {
		case listPanelID:
			list = p
		case toastPanelID:
			toast = p
		}
	}
	if list.ID == "" || toast.ID == "" {
		t.Fatalf("missing panels: %+v", panels)
	}
	if list.Kind != "list" || toast.Kind != "toast" {
		t.Fatalf("kinds wrong: %q %q", list.Kind, toast.Kind)
	}
	// actions are module-qualified (settled Q4b)
	for _, a := range list.Actions {
		if a.Module != "notifications" {
			t.Fatalf("action %q carries module %q", a.ID, a.Module)
		}
	}
	if len(list.Actions) != 2 {
		t.Fatalf("list actions: got %d, want 2", len(list.Actions))
	}

	lp, ok := list.Payload.(listPayload)
	if !ok {
		t.Fatalf("list payload type: %T", list.Payload)
	}
	if lp.Unread != 2 {
		t.Fatalf("_unread: got %d, want 2", lp.Unread)
	}
	if len(lp.Rows) != 2 {
		t.Fatalf("_rows: got %d, want 2", len(lp.Rows))
	}
	// newest first; unread rows carry action bindings, read rows do not
	if lp.Rows[0].Kind != "task" || lp.Rows[0].RowAction == nil || lp.Rows[0].RowAction.Action != actionRead {
		t.Fatalf("newest row lost its action binding: %+v", lp.Rows[0])
	}
	if _, ok := lp.Rows[0].RowAction.Args.(map[string]any)["id"]; !ok {
		t.Fatalf("action binding carries no id: %+v", lp.Rows[0].RowAction)
	}
	// toast carries the newest record verbatim (a record-based push)
	tr, ok := toast.Payload.(row)
	if !ok || tr.SourceID != "t1" || tr.Verdict != "ok" {
		t.Fatalf("toast payload wrong: %+v", toast.Payload)
	}
	if tr.RowAction != nil {
		t.Fatalf("toast is a notice, not a control surface: %+v", tr)
	}
}

// TestEmitPushesToastAndList: the mounted seam implementation pushes
// both panels on every emit - the emitter wiring lives in the module
// (settled answer 3). Before registration installs the sink the pushes
// stay silent (no nil dereference), after it they land.
func TestEmitPushesToastAndList(t *testing.T) {
	_, c, e := newTestCapsule(t)
	var updates []core.Panel

	// no sink installed yet: emit must not panic and must record
	if _, err := e.Emit(core.Notification{Kind: "chat", SourceID: "s1", Title: "chat s1 finished"}); err != nil {
		t.Fatalf("emit without sink: %v", err)
	}

	c.SetPanelUpdate(func(p core.Panel) error {
		updates = append(updates, p)
		return nil
	})
	if _, err := e.Emit(core.Notification{Kind: "cron", SourceID: "n2", Title: "cron n2 finished", Verdict: "failed"}); err != nil {
		t.Fatalf("emit: %v", err)
	}
	if len(updates) != 2 {
		t.Fatalf("_pushes per emit: got %d, want 2 (toast + list)", len(updates))
	}
	if updates[0].ID != toastPanelID || updates[0].Payload == nil {
		t.Fatalf("first push is the toast with the record: %+v", updates[0])
	}
	if r, ok := updates[0].Payload.(row); !ok || r.SourceID != "n2" {
		t.Fatalf("toast carries the emitted record: %+v", updates[0].Payload)
	}
	if updates[1].ID != listPanelID {
		t.Fatalf("second push is the list refresh: %+v", updates[1])
	}
}

// TestHandleActionMarksRead: the action pipe mutates the store and
// refreshes the list panel through the sink; a row binding drives the
// same call the renderers make.
func TestHandleActionMarksRead(t *testing.T) {
	_, c, e := newTestCapsule(t)
	got, err := e.Emit(core.Notification{Kind: "task", SourceID: "t9", Title: "task t9 done"})
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	var pushes int
	c.SetPanelUpdate(func(p core.Panel) error {
		pushes++
		return nil
	})
	pushes = 0 // drop the emit's own pushes before the action call

	// the row binding's args are exactly what the action pipe takes
	binding := &rowAction{Action: actionRead, Args: map[string]any{"id": got.ID}}
	raw, err := json.Marshal(binding.Args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	if _, err := e.capsule.HandleAction(core.ActionCall{Module: "notifications", Action: binding.Action, Args: raw}); err != nil {
		t.Fatalf("handle mark_read: %v", err)
	}
	if pushes != 1 {
		t.Fatalf("_list refresh per action: got %d, want 1", pushes)
	}
	notes, unread, err := e.store.List(context.Background(), false, 10, 0)
	if err != nil || unread != 0 || len(notes) != 1 || !notes[0].Read {
		t.Fatalf("mark did not land: unread=%d err=%v rows=%+v", unread, err, notes)
	}

	// no args: the action refuses instead of guessing
	if _, err := e.capsule.HandleAction(core.ActionCall{Module: "notifications", Action: actionRead}); err == nil {
		t.Fatalf("mark_read without id must fail")
	}

	// mark_all clears the rest through the same pipe
	if _, err := e.Emit(core.Notification{Kind: "cron", SourceID: "n3", Title: "cron n3 finished"}); err != nil {
		t.Fatalf("emit: %v", err)
	}
	if _, err := e.capsule.HandleAction(core.ActionCall{Module: "notifications", Action: actionAll}); err != nil {
		t.Fatalf("handle mark_all: %v", err)
	}
	if _, unread, err := e.store.List(context.Background(), false, 10, 0); err != nil || unread != 0 {
		t.Fatalf("mark_all did not land: unread=%d err=%v", unread, err)
	}
}

// TestEmitAfterMarkCarriesNewestRecord: a toast push carries the
// emitted record verbatim, not a re-read of the store - two emits in a
// row push two distinct toasts.
func TestEmitAfterMarkCarriesNewestRecord(t *testing.T) {
	_, c, e := newTestCapsule(t)
	var toasts []row
	c.SetPanelUpdate(func(p core.Panel) error {
		if p.ID == toastPanelID {
			if r, ok := p.Payload.(row); ok {
				toasts = append(toasts, r)
			}
		}
		return nil
	})
	e.Emit(core.Notification{Kind: "task", SourceID: "a", Title: "task a done"})
	e.Emit(core.Notification{Kind: "cron", SourceID: "b", Title: "cron b finished"})
	if len(toasts) != 2 || toasts[0].SourceID != "a" || toasts[1].SourceID != "b" {
		t.Fatalf("toasts must ride their own records: %+v", toasts)
	}
}
