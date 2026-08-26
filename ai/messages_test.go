package ai

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