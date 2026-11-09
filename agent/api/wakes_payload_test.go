package api

// Lane B of the phantom-results probe (card t_a54ef2ce): the prod
// agent's skill accused the pending re-entry path of delivering ONE
// arrival that mixed rows from more than one result. The probe found
// every stored yield_result carrying its own tool_call_id and content
// (no mixing), so this test pins the queue invariant that makes it
// impossible: the hub is FIFO per session, each slot holds exactly one
// result, and a payload never leaves the queue it entered.

import (
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// TestWakeHubPayloadIsolation: concurrent arrivals for two homes never
// cross, and each home's results come out byte-exact and in arrival
// order - the shape a shared-buffer or wrong-id-match bug would break.
func TestWakeHubPayloadIsolation(t *testing.T) {
	h := newWakeHub()

	// Interleaved arrivals: same tool, same session, different calls.
	h.enqueue("s1", core.YieldResult{ToolCallID: "c1", ToolName: "shell.run", Content: "grep: /a/one"})
	h.enqueue("s2", core.YieldResult{ToolCallID: "c9", ToolName: "shell.run", Content: "grep: /b/nine"})
	h.enqueue("s1", core.YieldResult{ToolCallID: "c2", ToolName: "files.grep", Content: "grep: /a/two"})

	want := map[string][]core.YieldResult{
		"s1": {
			{ToolCallID: "c1", ToolName: "shell.run", Content: "grep: /a/one"},
			{ToolCallID: "c2", ToolName: "files.grep", Content: "grep: /a/two"},
		},
		"s2": {
			{ToolCallID: "c9", ToolName: "shell.run", Content: "grep: /b/nine"},
		},
	}
	for sessionID, results := range want {
		for i, exp := range results {
			got, ok := h.pop(sessionID)
			if !ok {
				t.Fatalf("%s: pop %d returned nothing", sessionID, i)
			}
			if got.ToolCallID != exp.ToolCallID || got.ToolName != exp.ToolName || got.Content != exp.Content {
				t.Fatalf("%s pop %d = %+v, want %+v (payloads mixed or reordered)", sessionID, i, got, exp)
			}
		}
		if got, ok := h.pop(sessionID); ok {
			t.Fatalf("%s drained an extra result: %+v", sessionID, got)
		}
	}
}

// TestWakeHubPushFrontKeepsPayload: the lost-slot-race path re-queues a
// result at the FRONT and the live run's finish hook pops it again. The
// re-queued slot must carry the same payload it went in with - a re-queue
// that rebuilt or truncated it would hand the model a different result
// than the call produced.
func TestWakeHubPushFrontKeepsPayload(t *testing.T) {
	h := newWakeHub()
	first := core.YieldResult{ToolCallID: "c1", ToolName: "shell.run", Content: "kept whole"}
	h.enqueue("s1", first)
	got, ok := h.pop("s1")
	if !ok {
		t.Fatal("pop returned nothing")
	}
	h.pushFront("s1", got)
	h.enqueue("s1", core.YieldResult{ToolCallID: "c2", ToolName: "shell.run", Content: "later"})

	again, ok := h.pop("s1")
	if !ok {
		t.Fatal("pop after pushFront returned nothing")
	}
	if again.ToolCallID != "c1" || again.Content != "kept whole" {
		t.Fatalf("re-queued result = %+v, want the original payload first", again)
	}
	next, ok := h.pop("s1")
	if !ok || next.ToolCallID != "c2" || next.Content != "later" {
		t.Fatalf("follow-up result = %+v ok=%v, want c2 untouched", next, ok)
	}
}
