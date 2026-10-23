package store

// Tree store tests (t_17365247): AppendMessage materializes the tree
// pointer (explicit wins + validated, derived = active-path leaf), the
// active view derives, selections persist as variant_select entries.

import (
	"context"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/ai"
)

// TestAppendDerivesLeafParent: a plain send's appends chain off the
// active path's leaf, then the head's parent hops to the user.
func TestAppendDerivesLeafParent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, "t1", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMessage(ctx, "t1", ai.NewUser("hi")); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMessage(ctx, "t1", ai.NewAssistant("one")); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMessage(ctx, "t1", ai.NewUser("q2")); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMessage(ctx, "t1", ai.NewAssistant("two")); err != nil {
		t.Fatal(err)
	}
	rows, err := s.loadRows(ctx, "t1")
	if err != nil {
		t.Fatal(err)
	}
	// u1, a1, u2(parent=2), a2(parent=3): linear chain materialized
	if p := core.ParentIDOf(rows[2].Message); p != core.MsgID(rows[1].ID) {
		t.Fatalf("u2 parent = %q, want the a1 row id", p)
	}
	if p := core.ParentIDOf(rows[3].Message); p != core.MsgID(rows[2].ID) {
		t.Fatalf("a2 parent = %q, want the u2 row id", p)
	}
	view, err := s.GetActiveView(ctx, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Path) != 4 || len(view.Branches) != 0 {
		t.Fatalf("linear session view: path %d, branches %+v", len(view.Path), view.Branches)
	}
}

// TestAppendExplicitParentValidated: the retry head's explicit pointer
// lands verbatim; a dangling pointer is REJECTED, not silently
// truncated into a chain.
func TestAppendExplicitParentValidated(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, "t2", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMessage(ctx, "t2", ai.NewUser("hi")); err != nil {
		t.Fatal(err)
	}
	rows, _ := s.loadRows(ctx, "t2")
	head := ai.NewAssistant("retry answer")
	head.ParentID = core.MsgID(rows[0].ID)
	if err := s.AppendMessage(ctx, "t2", head); err != nil {
		t.Fatal(err)
	}
	rows, _ = s.loadRows(ctx, "t2")
	if p := core.ParentIDOf(rows[1].Message); p != core.MsgID(rows[0].ID) {
		t.Fatalf("head parent = %q, want the user id", p)
	}
	// dangling: explicit pointer to a foreign row rejects
	bad := ai.NewAssistant("bad")
	bad.ParentID = "9999"
	err := s.AppendMessage(ctx, "t2", bad)
	if err == nil || !strings.Contains(err.Error(), "parent message 9999") {
		t.Fatalf("dangling parent err = %v, want parent-not-found", err)
	}
}

// TestRetrySelectPersistAndPathSwap: the full retry-variant round trip
// at store level - sibling, select, reload, path follows the selection.
func TestRetrySelectPersistAndPathSwap(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, "t3", "", ""); err != nil {
		t.Fatal(err)
	}
	// user + first run
	if err := s.AppendMessage(ctx, "t3", ai.NewUser("who")); err != nil {
		t.Fatal(err)
	}
	rows, _ := s.loadRows(ctx, "t3")
	userID := core.MsgID(rows[0].ID)

	retry := func(ans string) {
		t.Helper()
		h := ai.NewAssistant(ans)
		h.ParentID = userID
		if err := s.AppendMessage(ctx, "t3", h); err != nil {
			t.Fatal(err)
		}
	}
	retry("first")
	retry("second")
	// default active = the newest head
	view, err := s.GetActiveView(ctx, "t3")
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Path) != 2 || view.Path[1].Message.(ai.Assistant).Content != "second" {
		t.Fatalf("default path = %+v, want [user, second]", view.Path)
	}
	if len(view.Branches) == 0 || view.Branches[0].Count != 2 || view.Branches[0].ActiveIndex != 1 {
		t.Fatalf("branches = %+v, want count 2 active 1", view.Branches)
	}
	// select variant 1; the path flips and PERSISTS through reload
	rows, _ = s.loadRows(ctx, "t3")
	if err := s.AppendMessage(ctx, "t3", ai.NewVariantSelect(userID, core.MsgID(rows[1].ID))); err != nil {
		t.Fatal(err)
	}
	// a CONTINUATION message from the selected branch chains off the
	// selected leaf (the fork-send shape: explicit parent = the leaf)
	rows, _ = s.loadRows(ctx, "t3")
	if p := core.ParentIDOf(rows[2].Message); p != core.MsgID(rows[0].ID) {
		t.Fatalf("retry head parent wrong: %q", p)
	}
	view, err = s.GetActiveView(ctx, "t3")
	if err != nil {
		t.Fatal(err)
	}
	if view.Path[1].Message.(ai.Assistant).Content != "first" {
		t.Fatalf("path after select = %v content %+v, want [user, first]", pathOf(view), view.Path[1])
	}
	if view.Branches[0].ActiveIndex != 0 {
		t.Fatalf("active index = %d, want 0 after select", view.Branches[0].ActiveIndex)
	}
}

func pathOf(v core.ActiveView) []string {
	out := make([]string, len(v.Path))
	for i, sm := range v.Path {
		out[i] = core.MsgID(sm.ID)
	}
	return out
}

// TestLegacyRowsRenderLinear: rows seeded without pointers (a
// pre-branching database) derive one path and no variants block.
func TestLegacyRowsRenderLinear(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, "t4", "", ""); err != nil {
		t.Fatal(err)
	}
	// seed raw rows the way the pre-pointer store wrote them
	if err := s.AppendMessageRow(ctx, "t4", "user", `{"content":"legacy q"}`, ai.NowISO()); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMessageRow(ctx, "t4", "assistant", `{"content":"legacy a"}`, ai.NowISO()); err != nil {
		t.Fatal(err)
	}
	view, err := s.GetActiveView(ctx, "t4")
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Path) != 2 || len(view.Branches) != 0 {
		t.Fatalf("legacy view = path %d branches %+v, want 2 and none", len(view.Path), view.Branches)
	}
	if view.Path[0].Message.(ai.User).Content != "legacy q" ||
		view.Path[1].Message.(ai.Assistant).Content != "legacy a" {
		t.Fatalf("legacy order broken: %+v", view.Path)
	}
}