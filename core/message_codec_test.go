package core

import (
	"testing"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	thinking := "let me think"
	toolCalls := []ToolCall{
		{ID: "tc1", Name: "shell.run", Arguments: map[string]any{"cmd": "ls"}},
	}
	images := []ImageContent{
		{MediaType: "image/png", Base64: "iVBOR"},
	}

	cases := []struct {
		name string
		msg  Message
		want string
	}{
		{"system", System{Content: "you are helpful", Timestamp: "2026-01-01T00:00:00Z"}, "system"},
		{"user", User{Content: "hello", Timestamp: "2026-01-01T00:00:00Z"}, "user"},
		{"user_with_images", NewUserWithImages("describe this", images), "user"},
		{"assistant", Assistant{Content: "hi there", Timestamp: "2026-01-01T00:00:00Z"}, "assistant"},
		{"assistant_thinking", Assistant{Thinking: &thinking, Content: "answer", Timestamp: "2026-01-01T00:00:00Z"}, "assistant"},
		{"assistant_toolcalls", Assistant{ToolCalls: toolCalls, Content: "done", Timestamp: "2026-01-01T00:00:00Z"}, "assistant"},
		{"tool_result", ToolResult{Content: "output", ToolCallID: "tc1", IsError: false, Timestamp: "2026-01-01T00:00:00Z"}, "tool"},
		{"tool_result_error", ToolResult{Content: "boom", ToolCallID: "tc2", IsError: true, Timestamp: "2026-01-01T00:00:00Z"}, "tool"},
		{"model_change", ModelChange{Model: "glm-5.2", Timestamp: "2026-01-01T00:00:00Z"}, "model_change"},
		{"thinking_level_change", ThinkingLevelChange{Level: "high", Timestamp: "2026-01-01T00:00:00Z"}, "thinking_level_change"},
		{"verdict_card", NewVerdictCard("c1", "Approve?", "ship it?", []VerdictOption{{Label: "yes", Description: "do it"}, {Label: "no"}}, true, false, 0), "verdict_card"},
		{"verdict_card_allowtext", NewVerdictCard("c2", "Name?", "", nil, false, true, 90), "verdict_card"},
		{"verdict_pick", NewVerdictRecord("c1", []int{1}, []string{"yes"}, "", false, false), "verdict"},
		{"verdict_multi_pick", NewVerdictRecord("c3", []int{1, 3}, []string{"a", "c"}, "", false, false), "verdict"},
		{"verdict_skip", NewVerdictRecord("c1", nil, nil, "", true, false), "verdict"},
		{"verdict_expired", NewVerdictRecord("c1", nil, nil, "", false, true), "verdict"},
		{"verdict_text", NewVerdictRecord("c2", nil, nil, "omega", false, false), "verdict"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			role, payload, err := EncodeMessage(tc.msg)
			if err != nil {
				t.Fatalf("EncodeMessage: unexpected error: %v", err)
			}
			if role != tc.want {
				t.Errorf("EncodeMessage role = %q, want %q", role, tc.want)
			}

			decoded, err := DecodeMessage(role, payload)
			if err != nil {
				t.Fatalf("DecodeMessage: unexpected error: %v", err)
			}

			// Re-encode the decoded message and compare payload bytes.
			// Round-trip stability: encode → decode → encode must produce
			// identical JSON, proving no field was lost or altered.
			role2, payload2, err := EncodeMessage(decoded)
			if err != nil {
				t.Fatalf("re-encode: unexpected error: %v", err)
			}
			if role2 != role {
				t.Errorf("re-encoded role = %q, want %q", role2, role)
			}
			if string(payload2) != string(payload) {
				t.Errorf("round-trip mismatch\nfirst:  %s\nsecond: %s", payload, payload2)
			}
		})
	}
}

func TestEncodeMessageUnknownType(t *testing.T) {
	// nil does not match any case in the type switch.
	_, _, err := EncodeMessage(nil)
	if err == nil {
		t.Fatal("EncodeMessage(nil) expected error, got nil")
	}
}

func TestDecodeMessageUnknownRole(t *testing.T) {
	_, err := DecodeMessage("bogus", []byte(`{}`))
	if err == nil {
		t.Fatal("DecodeMessage with unknown role expected error, got nil")
	}
}

func TestDecodeMessageBadJSON(t *testing.T) {
	_, err := DecodeMessage("user", []byte(`{bad json`))
	if err == nil {
		t.Fatal("DecodeMessage with invalid JSON expected error, got nil")
	}
}

// TestToolResultGateRoundTrip (card t_1b3e2a8b): the gate's decision
// record survives the persisted-payload round trip - what a reloaded
// session's transcript renders is what the gate stamped.
func TestToolResultGateRoundTrip(t *testing.T) {
	lines := []string{
		"smart gate: auto-approved",
		"smart gate: auto-denied",
		"smart gate: escalated -> approved",
		"smart gate: escalated -> denied - command skipped",
		"smart gate: escalated -> expired",
		"smart gate: escalated -> cancelled",
	}
	for _, line := range lines {
		role, payload, err := EncodeMessage(ToolResult{
			Content: "output", ToolCallID: "tc1", Gate: line, Timestamp: "2026-11-01T00:00:00Z",
		})
		if err != nil {
			t.Fatalf("EncodeMessage: unexpected error: %v", err)
		}
		if role != "tool" {
			t.Fatalf("role = %q, want tool", role)
		}
		decoded, err := DecodeMessage(role, payload)
		if err != nil {
			t.Fatalf("DecodeMessage: unexpected error: %v", err)
		}
		res, ok := decoded.(ToolResult)
		if !ok {
			t.Fatalf("decoded %T, want ToolResult", decoded)
		}
		if res.Gate != line {
			t.Fatalf("gate line lost in the round trip: got %q, want %q", res.Gate, line)
		}
	}
	// A record without a stamp decodes with the field absent - no
	// transcript line, older sessions render exactly as they always did.
	role, payload, err := EncodeMessage(ToolResult{Content: "out", ToolCallID: "tc2"})
	if err != nil {
		t.Fatalf("EncodeMessage: unexpected error: %v", err)
	}
	decoded, err := DecodeMessage(role, payload)
	if err != nil {
		t.Fatalf("DecodeMessage: unexpected error: %v", err)
	}
	if res := decoded.(ToolResult); res.Gate != "" {
		t.Fatalf("unstamped result carries a gate: %q", res.Gate)
	}
}
