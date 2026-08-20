package agent

import (
	"encoding/json"
	"testing"

	"github.com/EndoTheDev/omega/ai"
)

func TestNotificationToEvent(t *testing.T) {
	tests := []struct {
		name   string
		params map[string]any
		want   ai.StreamEvent
	}{
		{
			name:   "response_chunk",
			params: map[string]any{"type": "response_chunk", "content": "hello"},
			want:   ai.ResponseChunk{Type: "response_chunk", Content: "hello"},
		},
		{
			name:   "thinking_chunk",
			params: map[string]any{"type": "thinking_chunk", "content": "hmm"},
			want:   ai.ThinkingChunk{Type: "thinking_chunk", Content: "hmm"},
		},
		{
			name: "tool_call",
			params: map[string]any{
				"type": "tool_call",
				"tool_call": map[string]any{
					"id":        "call_1",
					"name":      "shell.run",
					"arguments": map[string]any{"command": "ls"},
				},
			},
			want: ai.ToolCallEvent{
				Type: "tool_call",
				ToolCall: ai.ToolCall{
					ID:        "call_1",
					Name:      "shell.run",
					Arguments: map[string]any{"command": "ls"},
				},
			},
		},
		{
			name:   "nil_params",
			params: nil,
			want:   ai.StreamEnd{Type: "stream_end", FinishReason: "error", Error: "nil notification params"},
		},
		{
			name:   "nil_tool_call",
			params: map[string]any{"type": "tool_call"},
			want:   ai.StreamEnd{Type: "stream_end", FinishReason: "error", Error: "nil tool_call in notification"},
		},
		{
			name:   "unknown_type",
			params: map[string]any{"type": "bogus"},
			want:   ai.StreamEnd{Type: "stream_end", FinishReason: "error", Error: "unknown notification type: bogus"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := notificationToEvent(tc.params)
			switch vg := got.(type) {
			case ai.ResponseChunk:
				want := tc.want.(ai.ResponseChunk)
				if vg != want {
					t.Errorf("got %+v, want %+v", vg, want)
				}
			case ai.ThinkingChunk:
				want := tc.want.(ai.ThinkingChunk)
				if vg != want {
					t.Errorf("got %+v, want %+v", vg, want)
				}
			case ai.ToolCallEvent:
				want := tc.want.(ai.ToolCallEvent)
				if vg.Type != want.Type || vg.ToolCall.ID != want.ToolCall.ID ||
					vg.ToolCall.Name != want.ToolCall.Name {
					t.Errorf("got %+v, want %+v", vg, want)
				}
			case ai.StreamEnd:
				want := tc.want.(ai.StreamEnd)
				if vg.FinishReason != want.FinishReason || vg.Error != want.Error {
					t.Errorf("got %+v, want %+v", vg, want)
				}
			default:
				t.Errorf("unexpected event type %T", got)
			}
		})
	}
}

func TestResultToStreamEnd(t *testing.T) {
	tests := []struct {
		name   string
		json   string
		want   ai.StreamEnd
	}{
		{
			name: "normal_stop",
			json: `{"finish_reason":"stop","prompt_eval_count":15,"eval_count":61}`,
			want: ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		},
		{
			name: "error",
			json: `{"finish_reason":"error","error":"HTTP 500"}`,
			want: ai.StreamEnd{Type: "stream_end", FinishReason: "error", Error: "HTTP 500"},
		},
		{
			name: "empty",
			json: `{}`,
			want: ai.StreamEnd{Type: "stream_end"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := resultToStreamEnd(json.RawMessage(tc.json))
			if got.Type != tc.want.Type || got.FinishReason != tc.want.FinishReason ||
				got.Error != tc.want.Error {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestResultToStreamEndTokenCounts(t *testing.T) {
	result := json.RawMessage(`{"finish_reason":"stop","prompt_eval_count":42,"eval_count":100}`)
	got := resultToStreamEnd(result)
	if got.PromptEvalCount == nil || *got.PromptEvalCount != 42 {
		t.Errorf("PromptEvalCount = %v, want 42", got.PromptEvalCount)
	}
	if got.EvalCount == nil || *got.EvalCount != 100 {
		t.Errorf("EvalCount = %v, want 100", got.EvalCount)
	}
}

func TestMessagesToJSONRoleInjection(t *testing.T) {
	messages := []ai.Message{
		ai.NewSystem("you are helpful"),
		ai.NewUser("hello"),
		ai.NewAssistant("hi there"),
		ai.NewToolResult("result", "call_1", false),
		ai.NewModelChange("llama3"),
		ai.NewThinkingLevelChange("high"),
	}

	got := messagesToJSON(messages)

	if len(got) != 6 {
		t.Fatalf("got %d messages, want 6", len(got))
	}

	wantRoles := []string{"system", "user", "assistant", "tool", "", ""}
	for i, want := range wantRoles {
		role, ok := got[i]["role"].(string)
		if !ok && want != "" {
			t.Errorf("msg %d: no role field, want %q", i, want)
		} else if want != "" && role != want {
			t.Errorf("msg %d: role = %q, want %q", i, role, want)
		}
	}

	// Verify timestamp is dropped from all messages.
	for i, m := range got {
		if _, hasTS := m["timestamp"]; hasTS {
			t.Errorf("msg %d: timestamp not dropped", i)
		}
	}

	// Verify content is preserved.
	if content, _ := got[0]["content"].(string); content != "you are helpful" {
		t.Errorf("msg 0 content = %q, want %q", content, "you are helpful")
	}
	if content, _ := got[1]["content"].(string); content != "hello" {
		t.Errorf("msg 1 content = %q, want %q", content, "hello")
	}
}