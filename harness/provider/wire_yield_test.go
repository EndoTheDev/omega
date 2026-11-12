package provider

// The wire pin for the async re-entry contract (card t_954a67a6): the
// wake path's front half (ledger, wake, launch, input persist) was
// healthy and every wake test exercised the CORE message list against a
// fake provider - none serialized the re-entered result onto the
// provider wire. There, core.YieldResult fell through messagesToJSON's
// role switch with NO role key, so the wake turn's context ended at the
// model's own last message: a lenient server accepted the role-less row
// and answered nothing (~1s), the conversation read as dead.
//
// This test is the seam those tests missed, in the shape a wake run
// actually produces: the assistant that issued the yield-class call,
// an inline tool result, the caveat assistant, then the settled result
// re-entering as the run's last message.

import (
	"encoding/json"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// wakeWireMessages is the message list a wake run's provider call sees
// for the live receipt (assistant id 1130 -> 1134): a call, an inline
// result answering another call, the caveat turn, then the settled
// result.
func wakeWireMessages() []core.Message {
	call := core.ToolCall{
		ID:        "call_5b16qor0",
		Name:      "files.glob",
		Arguments: map[string]any{"pattern": "*"},
	}
	return []core.Message{
		core.Assistant{ToolCalls: []core.ToolCall{call}},
		core.NewToolResult("inline result", "call_inline", false),
		core.NewAssistant("the directory listing call is still running"),
		core.YieldResult{
			ToolCallID: "call_5b16qor0",
			ToolName:   "files.glob",
			Content:    "a.txt\nb.txt",
			Timestamp:  core.NowISO(),
		},
	}
}

// TestYieldResultSerializesAsToolRow pins the one thing the wake tests
// never asserted: the settled result reaches the model. Every wire row
// carries a role, and the yield row rides as a "tool" row keeping its
// tool_call_id so it pairs with the answered call in the earlier
// assistant's tool_calls - the same shape a normal ToolResult produces.
func TestYieldResultSerializesAsToolRow(t *testing.T) {
	for _, stringArgs := range []bool{false, true} {
		name := "objectArgs"
		if stringArgs {
			name = "stringArgs"
		}
		t.Run(name, func(t *testing.T) {
			wire := messagesToJSON(wakeWireMessages(), stringArgs)
			if len(wire) != 4 {
				t.Fatalf("got %d wire rows, want 4", len(wire))
			}
			wantRoles := []string{"assistant", "tool", "assistant", "tool"}
			for i, want := range wantRoles {
				role, _ := wire[i]["role"].(string)
				if role != want {
					body, _ := json.Marshal(wire[i])
					t.Errorf("row %d role = %q, want %q (%s)", i, role, want, body)
				}
			}
			// The re-entered result must keep the pairing id: a strict
			// wire rejects a tool row whose id no assistant called.
			gotID, _ := wire[3]["tool_call_id"].(string)
			if gotID != "call_5b16qor0" {
				t.Errorf("yield row tool_call_id = %q, want call_5b16qor0", gotID)
			}
			if got, _ := wire[3]["content"].(string); got != "a.txt\nb.txt" {
				t.Errorf("yield row content = %q, want the listing", got)
			}
			// No row may reach the wire role-less: a role-less row is
			// exactly how the result went invisible.
			for i, m := range wire {
				if role, _ := m["role"].(string); role == "" {
					body, _ := json.Marshal(m)
					t.Errorf("row %d serialized without a role: %s", i, body)
				}
			}
			// The Anthropic path folds every "tool" row into a
			// tool_result block keyed by that id - the strictest
			// consumer of the pairing.
			_, conv := anthropicConvertMessages(wire)
			last := conv[len(conv)-1]
			if role, _ := last["role"].(string); role != "user" {
				t.Fatalf("anthropic last message role = %q, want user (the folded tool block)", role)
			}
			blocks, _ := last["content"].([]map[string]any)
			if len(blocks) != 1 {
				t.Fatalf("anthropic folded %d blocks, want 1", len(blocks))
			}
			if id, _ := blocks[0]["tool_use_id"].(string); id != "call_5b16qor0" {
				t.Errorf("anthropic tool_use_id = %q, want call_5b16qor0", id)
			}
		})
	}
}
