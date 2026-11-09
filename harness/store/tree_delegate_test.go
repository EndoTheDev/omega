package store

// Delegate-lineage split tests (t_6c4ab302): parent_id is the FORK
// lineage; delegate children carry delegate_parent and keep parent_id
// NULL, so they never see the caller's conversation. Both shapes are
// pinned here side by side - the ruling is "fork inherits, delegate does
// not", and a regression in either direction reds.

import (
	"context"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// TestDelegateChildHasFreshContext pins the delegate half: a delegate
// child's active view (transcript render) and active messages (context
// assembly) contain only its own rows, its first message starts a fresh
// tree chain, and its lineage survives as delegate_parent.
func TestDelegateChildHasFreshContext(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, "parent", "", "caller"); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMessage(ctx, "parent", core.NewUser("parent question")); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMessage(ctx, "parent", core.NewAssistant("parent answer")); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateDelegateSession(ctx, "kid", "parent", "delegate:task-1"); err != nil {
		t.Fatal(err)
	}

	// Lineage shape: delegate_parent set, fork parent_id NULL.
	sess, err := s.GetSession(ctx, "kid")
	if err != nil {
		t.Fatal(err)
	}
	if sess.ParentID != "" {
		t.Fatalf("delegate child parent_id = %q, want empty (fork lineage is fork-only)", sess.ParentID)
	}
	if sess.DelegateParent != "parent" {
		t.Fatalf("delegate child delegate_parent = %q, want parent", sess.DelegateParent)
	}

	// The child's own first message starts a fresh chain - it must NOT
	// chain onto the parent's leaf (that would mean the merged view
	// leaked the caller's history into the child's tree).
	if err := s.AppendMessage(ctx, "kid", core.NewUser("fresh prompt")); err != nil {
		t.Fatal(err)
	}
	rows, err := s.loadRows(ctx, "kid")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("delegate child own rows = %d, want 1", len(rows))
	}
	if p := core.ParentIDOf(rows[0].Message); p != "" {
		t.Fatalf("delegate child first message parent = %q, want a fresh chain root", p)
	}

	// Transcript render seam.
	view, err := s.GetActiveView(ctx, "kid")
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Path) != 1 {
		t.Fatalf("delegate child active path = %d rows, want only its own 1", len(view.Path))
	}
	if u, ok := view.Path[0].Message.(core.User); !ok || u.Content != "fresh prompt" {
		t.Fatalf("delegate child path[0] = %T %+v, want its own user message", view.Path[0].Message, view.Path[0].Message)
	}

	// Context assembly seam.
	msgs, err := s.GetActiveMessages(ctx, "kid")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("delegate child active messages = %d, want 1 (own only)", len(msgs))
	}
	if u, ok := msgs[0].(core.User); !ok || u.Content != "fresh prompt" {
		t.Fatalf("delegate child active messages[0] = %T %+v, want its own user message", msgs[0], msgs[0])
	}
}

// TestForkChildStillInherits pins the fork half (t_17365247 unchanged):
// a branch child created with a parent link still prepends the whole
// ancestor chain - transcript render and context assembly both.
func TestForkChildStillInherits(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, "parent", "", "caller"); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMessage(ctx, "parent", core.NewUser("parent question")); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMessage(ctx, "parent", core.NewAssistant("parent answer")); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession(ctx, "fork", "parent", ""); err != nil {
		t.Fatal(err)
	}

	msgs, err := s.GetActiveMessages(ctx, "fork")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("fork child active messages = %d, want the 2 ancestor rows", len(msgs))
	}
	if u, ok := msgs[0].(core.User); !ok || u.Content != "parent question" {
		t.Fatalf("fork child messages[0] = %T %+v, want the parent user", msgs[0], msgs[0])
	}

	// The fork child's first own message chains onto the ancestor leaf
	// (the merged view's last row), which is what makes a fork a
	// continuation instead of a fresh conversation.
	if err := s.AppendMessage(ctx, "fork", core.NewUser("fork prompt")); err != nil {
		t.Fatal(err)
	}
	rows, err := s.loadRows(ctx, "fork")
	if err != nil {
		t.Fatal(err)
	}
	parentRows, err := s.loadRows(ctx, "parent")
	if err != nil {
		t.Fatal(err)
	}
	if p, want := core.ParentIDOf(rows[0].Message), core.MsgID(parentRows[len(parentRows)-1].ID); p != want {
		t.Fatalf("fork child first message parent = %q, want the ancestor leaf %q", p, want)
	}
	msgs, err = s.GetActiveMessages(ctx, "fork")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 3 {
		t.Fatalf("fork child active messages = %d, want 2 ancestors + own", len(msgs))
	}
}
