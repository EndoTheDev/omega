package agent

// Tree derivation tests (t_17365247): the store is a linear append
// list, the tree derives from parent pointers riding the message
// payloads. Pure functions - no store.

import (
	"reflect"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/ai"
)

func treeUser(content string) ai.User {
	return ai.User{Content: content, Timestamp: time.Now().UTC().Format(time.RFC3339Nano)}
}

func treeAssistant(content string) ai.Assistant {
	return ai.Assistant{Content: content, Timestamp: time.Now().UTC().Format(time.RFC3339Nano)}
}

func treeRows(t *testing.T, msgs ...ai.Message) []StoredMessage {
	t.Helper()
	rows := make([]StoredMessage, len(msgs))
	for i, m := range msgs {
		rows[i] = StoredMessage{ID: int64(i + 1), Message: m}
	}
	return rows
}

// withPtr returns a copy of the message carrying the parent pointer.
func withPtr(m ai.Message, parent string) ai.Message { return WithParentID(m, parent) }

// ids renders a path as the row-id order the consumers reason in.
func pathIDs(view ActiveView) []string {
	out := make([]string, len(view.Path))
	for i, sm := range view.Path {
		out[i] = MsgID(sm.ID)
	}
	return out
}

// TestDeriveLinearLegacySinglePath: messages persisted before pointers
// existed carry none - the derivation falls back to row-order
// adjacency and the transcript renders exactly as it always did.
func TestDeriveLinearLegacySinglePath(t *testing.T) {
	rows := treeRows(t, treeUser("u"), treeAssistant("a"), treeUser("u2"), treeAssistant("a2"))
	view := DeriveTree(rows)
	want := []string{"1", "2", "3", "4"}
	if got := pathIDs(view); !reflect.DeepEqual(got, want) {
		t.Fatalf("legacy path = %v, want %v (linear, migration-free)", got, want)
	}
	if len(view.Branches) != 0 {
		t.Fatalf("legacy branches = %+v, want none", view.Branches)
	}
}

// TestDeriveRetryCreatesSiblingNoAppendSwap: the retry hop parents two
// assistant heads to the same user; default active = the newest head.
func TestDeriveRetryCreatesSiblingNoAppendSwap(t *testing.T) {
	// row order: u(1), first(2), second(3)
	rows := treeRows(t,
		withPtr(treeUser("u"), ""),
		withPtr(treeAssistant("first answer"), "1"),
		withPtr(treeAssistant("second answer"), "1"),
	)
	view := DeriveTree(rows)
	if got := pathIDs(view); !reflect.DeepEqual(got, []string{"1", "3"}) {
		t.Fatalf("active path after retry = %v, want [1 3] (ONE response + variants meta)", got)
	}
	if len(view.Branches) != 1 {
		t.Fatalf("branches = %+v, want 1", view.Branches)
	}
	b := view.Branches[0]
	if b.ParentID != "1" || b.Count != 2 || b.ActiveIndex != 1 {
		t.Fatalf("branch = %+v, want parent 1, count 2, active 1", b)
	}
	if !reflect.DeepEqual(b.Siblings, []string{"2", "3"}) {
		t.Fatalf("siblings = %v, want [2 3]", b.Siblings)
	}
}

// TestDeriveVariantSelectPersistsAndWins: the selection entry flips the
// active variant and survives; the last entry per branch wins.
func TestDeriveVariantSelectPersistsAndWins(t *testing.T) {
	rows := treeRows(t,
		withPtr(treeUser("u"), ""),
		withPtr(treeAssistant("first answer"), "1"),
		withPtr(treeAssistant("second answer"), "1"),
		ai.NewVariantSelect("1", "2"),
	)
	view := DeriveTree(rows)
	if got := pathIDs(view); !reflect.DeepEqual(got, []string{"1", "2"}) {
		t.Fatalf("path after select variant 1 = %v, want [1 2]", got)
	}
	if view.Branches[0].ActiveIndex != 0 {
		t.Fatalf("active index = %d, want 0", view.Branches[0].ActiveIndex)
	}
	// a later select entry re-flips the same branch
	rows = append(rows, StoredMessage{ID: 5, Message: ai.NewVariantSelect("1", "3")})
	view = DeriveTree(rows)
	if got := pathIDs(view); !reflect.DeepEqual(got, []string{"1", "3"}) {
		t.Fatalf("path after re-select = %v, want [1 3] (last entry wins)", got)
	}
}

// TestDeriveToolChainStaysInGroup: a run's tool span (assistant with
// calls, results, follow-on segment) remains one branch under the
// user; selecting a variant group swaps the WHOLE run.
func TestDeriveToolChainStaysInGroup(t *testing.T) {
	a1 := treeAssistant("")
	a1.ToolCalls = []ai.ToolCall{{ID: "c1", Name: "web"}}
	rows := treeRows(t,
		withPtr(treeUser("u"), ""),
		withPtr(a1, "1"),                 // head + calls
		withPtr(ai.NewToolResult("r", "c1", false), "2"), // result chains off its assistant
		withPtr(treeAssistant("final"), "3"),             // follow-on segment
	)
	view := DeriveTree(rows)
	if got := pathIDs(view); !reflect.DeepEqual(got, []string{"1", "2", "3", "4"}) {
		t.Fatalf("tool chain path = %v, want the whole run [1 2 3 4]", got)
	}
	// a retry head joins the same branch point; the tool group stays whole
	rows = append(rows, StoredMessage{ID: 5, Message: withPtr(treeAssistant("retry answer"), "1")})
	view = DeriveTree(rows)
	if got := pathIDs(view); !reflect.DeepEqual(got, []string{"1", "5"}) {
		t.Fatalf("retry path = %v, want [1 5] (whole prior run swaps out)", got)
	}
	if b := view.Branches[0]; b.Siblings[0] != "2" || b.Siblings[1] != "5" {
		t.Fatalf("run-group siblings = %v, want heads [2 5]", b.Siblings)
	}
}

// TestDeriveContinueFromSelected: the next message's chain continues
// the selected branch's leaf - the fork-send message chains onto the
// forked point and the path order reflects it.
func TestDeriveContinueFromSelected(t *testing.T) {
	// legacy stack (no pointers) forked mid-way: u1(1), a1(2), fork
	// bubble u_new(3, parent=2), its run head a_new(4, parent=3)
	rows := treeRows(t,
		withPtr(treeUser("u1"), ""),
		withPtr(treeAssistant("a1"), "1"),
		withPtr(treeUser("fork text"), "2"),
		withPtr(treeAssistant("fork answer"), "3"),
	)
	view := DeriveTree(rows)
	if got := pathIDs(view); !reflect.DeepEqual(got, []string{"1", "2", "3", "4"}) {
		t.Fatalf("fork path = %v, want [1 2 3 4]", got)
	}
	// the pre-existing continuation under u1... a sibling head (5,
	// parent 1) exists: leaf by chain validity skips it when the fork
	// row is newest? The fork row (4) validates (chain: a_new->u_new->
	// a1 head active) and is row-max - it wins the leaf.
	rows = treeRows(t,
		withPtr(treeUser("u1"), ""),
		withPtr(treeAssistant("a1"), "1"),
		withPtr(treeUser("fork text"), "2"),
		withPtr(treeAssistant("fork answer"), "3"),
		withPtr(treeAssistant("later retry"), "1"),
	)
	view = DeriveTree(rows)
	if got := pathIDs(view); !reflect.DeepEqual(got, []string{"1", "5"}) {
		t.Fatalf("mixed leaf = %v, want [1 5] (row-max valid chain wins)", got)
	}
	if len(view.Branches) != 1 || view.Branches[0].Count != 2 {
		t.Fatalf("branches = %+v, want one branch with 2 heads", view.Branches)
	}
}

// TestDeriveMetaEntriesInvisible: bookkeeping rows never join the tree
// and never break chains.
func TestDeriveMetaEntriesInvisible(t *testing.T) {
	rows := treeRows(t,
		ai.NewModelChange("glm-5.3"),
		treeUser("u"),
		treeAssistant("a"),
		ai.NewThinkingLevelChange("high"),
		ai.NewVariantSelect("1", "2"), // dangling selection ignored
	)
	view := DeriveTree(rows)
	if got := pathIDs(view); !reflect.DeepEqual(got, []string{"2", "3"}) {
		t.Fatalf("path with meta entries = %v, want [2 3]", got)
	}
	if len(view.Branches) != 0 {
		t.Fatalf("dangling select created branches: %+v", view.Branches)
	}
}

// TestDeriveResolutionMaps: the variants endpoint's resolution ground
// truth - which run a message belongs to, and every branch point.
func TestDeriveResolutionMaps(t *testing.T) {
	rows := treeRows(t,
		withPtr(treeUser("u"), ""),
		withPtr(treeAssistant("a1"), "1"),
		withPtr(ai.NewToolResult("r", "c1", false), "2"),
		withPtr(treeAssistant("a2"), "1"), // retry head
		ai.NewVariantSelect("1", "4"),
	)
	view := DeriveTree(rows)
	if view.ActiveHeads["1"] != "4" {
		t.Fatalf("active head = %q, want 4", view.ActiveHeads["1"])
	}
	if view.BranchByHead["4"] != "1" || view.BranchByHead["2"] != "1" {
		t.Fatalf("branchByHead = %+v, want heads 2+4 under branch 1", view.BranchByHead)
	}
	if view.HeadByMessage["3"] != "2" || view.HeadByMessage["2"] != "2" || view.HeadByMessage["4"] != "4" {
		t.Fatalf("headByMessage = %+v, want results follow their run's head", view.HeadByMessage)
	}
}