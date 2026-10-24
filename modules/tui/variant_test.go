package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/ai"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/modules/agent_loop"
	tea "github.com/charmbracelet/bubbletea"
)

// branchedModel wires a store-backed model for variant tests.
func branchedModel(t *testing.T) model {
	t.Helper()
	ctx := testContext()
	if ctx.Store == nil {
		t.Fatalf("no store mounted")
	}
	return newChatModel("ollama", "llama3", nil, "", nil, "", ctx, nil, "dark", "", "bell", "dev", "")
}

// seedVariants builds a branched session in the model's store: one user
// turn with ONE sibling variant, then a VariantSelect pinning the
// FIRST sibling. Row layout:
//
//	1 user     "which lane?"      (root)
//	2 asst     "first answer"     parent 1
//	3 asst     "second answer"    parent 1  (retry sibling)
//	4 variant_select branch=1 child=2
//
// The select entry makes the FIRST sibling active: path [1, 2].
// The returned model carries sessionID + the loaded view.
func seedVariants(t *testing.T, m model) model {
	t.Helper()
	ctx := context.Background()
	sp := m.store
	if err := sp.CreateSession(ctx, "seeded", "", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}
	seed := []core.Message{
		core.NewUser("which lane?"),
		core.NewAssistant("first answer"),
		core.WithParentID(core.NewAssistant("second answer"), "1"),
		core.NewVariantSelect("1", "2"),
	}
	for _, msg := range seed {
		if err := sp.AppendMessage(ctx, "seeded", msg); err != nil {
			t.Fatalf("append seed: %v", err)
		}
	}
	view, err := sp.GetActiveView(ctx, "seeded")
	if err != nil {
		t.Fatalf("active view: %v", err)
	}
	m.sessionID = "seeded"
	m.autoNamed = true
	m.reloadView(view)
	return m
}

// withSeed is a small chain helper so tests read table-like.
func (m model) withSeed(t *testing.T) model {
	return seedVariants(t, m)
}

// TestPathMarkersDerive verifies variant markers come out index-aligned
// with the active path: the branch's active head lands at its position
// on the path, and renderTranscript surfaces it.
func TestPathMarkersDerive(t *testing.T) {
	m := branchedModel(t).withSeed(t)
	markers := pathMarkers(m.view, len(m.history))
	if len(markers) != 1 {
		t.Fatalf("expected one marker, got %v", markers)
	}
	if got := markers[1]; got != "[1/2]" {
		t.Fatalf("expected active head 1/2 at path index 1, got %q", got)
	}
	if out := ansiStrip(renderTranscript(m.history, 80, themes["dark"], markers)); !strings.Contains(out, "[1/2]") {
		t.Fatalf("marker missing from transcript: %q", out)
	}
}

// TestVariantSelectPersists runs /variant prev and /variant <n>
// against a real store: the selection lands as a variant_select row
// and the reloaded history follows the flipped branch.
func TestVariantSelectPersists(t *testing.T) {
	mm := branchedModel(t).withSeed(t)

	// No argument: lists branch state.
	out, _ := mm.handleCommand("/variant")
	if got := stripTranscript(out); !strings.Contains(got, "#1  variant 1/2") {
		t.Fatalf("variant list missing branch state: %q", got)
	}

	// prev flips 1/2 -> 2/2.
	out, _ = mm.handleCommand("/variant prev")
	flipped := out.(model)
	if got := stripTranscript(out); !strings.Contains(got, "[variant 2/2 at #1]") {
		t.Fatalf("variant flip not surfaced: %q", got)
	}
	// The persisted row is the ground truth: reload and re-derive.
	view, err := flipped.store.GetActiveView(context.Background(), flipped.sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if view.ActiveHeads["1"] != "3" {
		t.Fatalf("selection did not persist as variant_select: active=%q", view.ActiveHeads["1"])
	}
	if len(flipped.history) != 2 || flipped.history[1].(core.Assistant).Content != "second answer" {
		t.Fatalf("path not reloaded after select: %v", flipped.history)
	}
	if markers := flipped.markersForHistory(); markers == nil || markers[1] != "[2/2]" {
		t.Fatalf("markers not reloaded: %v", markers)
	}

	// Explicit number selects by 1-based variant index.
	out2, _ := flipped.handleCommand("/variant 1")
	picked := out2.(model)
	view2, err := picked.store.GetActiveView(context.Background(), picked.sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if view2.ActiveHeads["1"] != "2" {
		t.Fatalf("numeric select went to the wrong sibling: %q", view2.ActiveHeads["1"])
	}

	// Out of range is rejected without a store write...
	out3, _ := picked.handleCommand("/variant 5")
	denied := out3.(model)
	if denied.err == "" || !strings.Contains(denied.err, "usage: /variant") {
		t.Fatalf("expected usage error, got %q", denied.err)
	}
	// ...and selecting the active variant writes nothing either.
	rows, err := picked.store.GetMessagesWithTimes(context.Background(), "seeded")
	if err != nil {
		t.Fatal(err)
	}
	before := len(rows)
	picked.handleCommand("/variant 1")
	rows2, _ := picked.store.GetMessagesWithTimes(context.Background(), "seeded")
	if len(rows2) != before {
		t.Fatalf("no-op select wrote a row: %d -> %d", before, len(rows2))
	}
}

// TestForkPersists pins the continuation point: /fork <n> truncates
// the view to the forked perspective, and the next send chains onto
// the pinned row id exactly once.
func TestForkPersists(t *testing.T) {
	mm := branchedModel(t).withSeed(t)

	// No argument lists the numbered path.
	out, _ := mm.handleCommand("/fork")
	if got := stripTranscript(out); !strings.Contains(got, "user") || !strings.Contains(got, "which lane?") {
		t.Fatalf("fork list missing the numbered path: %q", got)
	}

	// /fork 1 pins the user row; the next send chains onto it.
	out2, _ := mm.handleCommand("/fork 1")
	pinned := out2.(model)
	if pinned.forkParent != "1" {
		t.Fatalf("fork pin not set, got %q", pinned.forkParent)
	}
	if len(pinned.history) != 1 {
		t.Fatalf("fork view not truncated to the pinned point: %d messages", len(pinned.history))
	}
	msg := applyForkPin(&pinned, core.NewUser("new branch, same question"))
	if pinned.forkParent != "" {
		t.Fatalf("pin not cleared after one send: %q", pinned.forkParent)
	}
	if core.ParentIDOf(msg) != "1" {
		t.Fatalf("send not chained onto the fork target: %v", core.ParentIDOf(msg))
	}
	if err := pinned.store.AppendMessage(context.Background(), "seeded", msg); err != nil {
		t.Fatalf("forked send rejected by store: %v", err)
	}
	// The continuation path runs through the fork point. Forking at a
	// USER row is retry semantics: the new user turn is that turn's
	// sibling (the pinned variant answer is not on the new path).
	fresh, err := pinned.store.GetActiveView(context.Background(), "seeded")
	if err != nil {
		t.Fatal(err)
	}
	if len(fresh.Path) != 2 { // fork target -> new user
		t.Fatalf("forked continuation not on the active path: %d entries", len(fresh.Path))
	}
	if core.ParentIDOf(fresh.Path[1].Message) != "1" {
		t.Fatalf("new user not chained onto the fork target: %q", core.ParentIDOf(fresh.Path[1].Message))
	}
}

// TestRetrySiblingsPins: /retry trims to the last user turn, the run
// head pins to the branch point, and the fresh response surfaces as a
// variant (marker + tree state + stale selection reset). The run is a
// real loop run - persistence is the loop's sink now (t_471f6d4a), so
// the sibling-variant store law needs the run to actually execute.
func TestRetrySiblingsPins(t *testing.T) {
	mm := branchedModel(t).withSeed(t)
	mountScriptedRun(&mm,
		// turn 1: partial response then a tool call - persists the
		// pinned head, then the tool result
		[]core.ProviderStreamEvent{
			core.ResponseChunk{Type: "response_chunk", Content: "fresh "},
			core.ToolCallEvent{Type: "tool_call", ToolCall: core.ToolCall{ID: "c1", Name: "nop"}},
			core.StreamEnd{Type: "stream_end", FinishReason: "tool_call"},
		},
		// turn 2: the unpinned follow-on assistant
		scriptedResponse("answer"),
	)

	out, _ := mm.handleCommand("/retry")
	retried := out.(model)
	if !retried.busy || retried.retryParent != "1" {
		t.Fatalf("retry state: busy=%v retryParent=%q", retried.busy, retried.retryParent)
	}
	if len(retried.history) != 1 { // trimmed back to the user turn
		t.Fatalf("history not trimmed: %d", len(retried.history))
	}
	if retried.retryMarker != "[3/3]" {
		t.Fatalf("expected fresh-head marker [3/3], got %q", retried.retryMarker)
	}

	// Drain the real run. The loop persists: head assistant pinned to
	// the branch-point user, the tool result, then the follow-on.
	for ev := range retried.events {
		retried.handleEvent(ev)
	}
	if retried.retryParent != "" || retried.headPinned || retried.retryMarker != "" {
		t.Fatal("retry state not cleared at AgentEnd")
	}
	if got := stripTranscript(retried); !strings.Contains(got, "[3/3]") {
		t.Fatalf("retry marker missing from transcript: %q", got)
	}

	// The run head pinned to the branch-point user; later messages of
	// the same run keep their own (derived) parent. History order:
	// user (trimmed), head assistant, tool result, follow-on assistant.
	if got := core.ParentIDOf(retried.history[1]); got != "1" {
		t.Fatalf("head not pinned: %q", got)
	}
	if got := core.ParentIDOf(retried.history[3]); got != "" {
		t.Fatalf("non-head message pinned: %q", got)
	}

	view, err := retried.store.GetActiveView(context.Background(), retried.sessionID)
	if err != nil {
		t.Fatal(err)
	}
	// Rows after the retry: 1 user, 2 asst, 3 asst, 4 select(first),
	// 5 select reset, 6 fresh head, 7 tool result, 8 follow-on. The
	// fresh head is a sibling variant; default active = last sibling.
	if len(view.SiblingHeads["1"]) != 3 || view.ActiveHeads["1"] != "6" {
		t.Fatalf("fresh head not a sibling variant: heads=%v active=%q", view.SiblingHeads["1"], view.ActiveHeads["1"])
	}
}

// mountScriptedRun mounts the real agent loop and a scripted fake
// provider into the model's extension context, so a started run
// executes the loop and persists through its store sink.
func mountScriptedRun(m *model, scripts ...[]core.ProviderStreamEvent) {
	if err := agent_loop.NewPlugin().Mount(m.extensions); err != nil {
		panic(err)
	}
	m.extensions.Provider = ai.NewFakeProviderScripts("test", scripts...)
}

// TestVariantGuards rejects the commands without a store, without a
// session, in ephemeral mode, and while a run is in flight.
func TestVariantGuards(t *testing.T) {
	if got := (model{}).variantGuards(); got != "no store available" {
		t.Fatalf("store guard: %q", got)
	}
	m := model{store: &mockStore{}}
	if got := m.variantGuards(); got != "no active session" {
		t.Fatalf("session guard: %q", got)
	}
	m.sessionID = "s"
	m.ephemeral = true
	if got := m.variantGuards(); got != "no sessions in ephemeral mode" {
		t.Fatalf("ephemeral guard: %q", got)
	}
	m.ephemeral = false
	m.busy = true
	out, _ := m.handleCommand("/variant")
	if got := out.(model); !got.busy || got.err != "a run is in flight" {
		t.Fatalf("busy guard: %q", got.err)
	}
}

// TestLinearSessionNoVariants verifies pointerless (old) sessions stay
// as-is: /variant reports no branch points and leaves the transcript
// alone.
func TestLinearSessionNoVariants(t *testing.T) {
	m := branchedModel(t)
	sp := m.store
	ctx := context.Background()
	if err := sp.CreateSession(ctx, "linear", "", ""); err != nil {
		t.Fatal(err)
	}
	for _, msg := range []core.Message{
		core.NewUser("q1"),
		core.NewAssistant("a1"),
		core.NewUser("q2"),
		core.NewAssistant("a2"),
	} {
		if err := sp.AppendMessage(ctx, "linear", msg); err != nil {
			t.Fatal(err)
		}
	}
	view, err := sp.GetActiveView(ctx, "linear")
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Branches) != 0 {
		t.Fatalf("linear session derived branches: %v", view.Branches)
	}
	m.sessionID = "linear"
	m.autoNamed = true
	m.reloadView(view)
	out, _ := m.handleCommand("/variant")
	if got := stripTranscript(out); !strings.Contains(got, "[no variants") {
		t.Fatalf("linear session not reported variant-free: %q", got)
	}
}

// stripTranscript flattens a returned model's transcript to plain text.
func stripTranscript(out tea.Model) string {
	return ansiStrip(out.(model).transcript)
}

// teaModel re-wraps a model as tea.Model for stripTranscript.
func teaModel(m model) tea.Model { return m }
