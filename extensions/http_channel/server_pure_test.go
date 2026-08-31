package http_channel

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/ai"
)

// newTestServerWithTools returns a Server whose tool registry contains a
// fixed set of mock tools, used for selectTools coverage.
func newTestServerWithTools() *Server {
	s := newTestServer()
	s.tools = map[string]agent.Tool{
		"shell.run":   {Description: "run shell", Parameters: nil, Run: func(context.Context, map[string]any) (string, error) { return "", nil }},
		"files.read":  {Description: "read file", Parameters: nil, Run: func(context.Context, map[string]any) (string, error) { return "", nil }},
		"files.write": {Description: "write file", Parameters: nil, Run: func(context.Context, map[string]any) (string, error) { return "", nil }},
	}
	return s
}

func TestSelectToolsEmpty(t *testing.T) {
	s := newTestServerWithTools()
	got := s.selectTools(nil)
	if len(got) != len(s.tools) {
		t.Fatalf("expected %d tools, got %d", len(s.tools), len(got))
	}
	for name := range s.tools {
		if _, ok := got[name]; !ok {
			t.Errorf("expected tool %q in result", name)
		}
	}
}

func TestSelectToolsAllMatch(t *testing.T) {
	s := newTestServerWithTools()
	names := []string{"shell.run", "files.read", "files.write"}
	got := s.selectTools(names)
	if len(got) != len(names) {
		t.Fatalf("expected %d tools, got %d", len(names), len(got))
	}
	for _, name := range names {
		if _, ok := got[name]; !ok {
			t.Errorf("expected tool %q in result", name)
		}
	}
}

func TestSelectToolsPartialMatch(t *testing.T) {
	s := newTestServerWithTools()
	names := []string{"shell.run", "does.not.exist"}
	got := s.selectTools(names)
	if len(got) != 1 {
		t.Fatalf("expected 1 tool (missing skipped), got %d", len(got))
	}
	if _, ok := got["shell.run"]; !ok {
		t.Errorf("expected shell.run in result")
	}
	if _, ok := got["does.not.exist"]; ok {
		t.Errorf("non-existent tool should not be present")
	}
}

func TestSelectToolsNoneMatch(t *testing.T) {
	s := newTestServerWithTools()
	names := []string{"nope.a", "nope.b"}
	got := s.selectTools(names)
	if len(got) != 0 {
		t.Fatalf("expected empty map, got %d", len(got))
	}
}

func TestDecodeMessagesAllRoles(t *testing.T) {
	raw := []json.RawMessage{
		json.RawMessage(`{"role":"system","content":"you are a helper"}`),
		json.RawMessage(`{"role":"user","content":"hello"}`),
		json.RawMessage(`{"role":"assistant","content":"hi there"}`),
		json.RawMessage(`{"role":"tool","content":"result data"}`),
	}
	msgs, err := decodeMessages(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(msgs) != len(raw) {
		t.Fatalf("expected %d messages, got %d", len(raw), len(msgs))
	}
	if _, ok := msgs[0].(ai.System); !ok {
		t.Errorf("expected first message to be ai.System, got %T", msgs[0])
	}
	if _, ok := msgs[1].(ai.User); !ok {
		t.Errorf("expected second message to be ai.User, got %T", msgs[1])
	}
	if _, ok := msgs[2].(ai.Assistant); !ok {
		t.Errorf("expected third message to be ai.Assistant, got %T", msgs[2])
	}
	if _, ok := msgs[3].(ai.ToolResult); !ok {
		t.Errorf("expected fourth message to be ai.ToolResult, got %T", msgs[3])
	}
}

func TestDecodeMessagesUnknownRole(t *testing.T) {
	raw := []json.RawMessage{
		json.RawMessage(`{"role":"wizard","content":"abracadabra"}`),
	}
	_, err := decodeMessages(raw)
	if err == nil {
		t.Fatal("expected error for unknown role, got nil")
	}
	if !strings.Contains(err.Error(), "wizard") {
		t.Errorf("expected error to contain role name, got %q", err.Error())
	}
}

func TestDecodeMessagesInvalidJSON(t *testing.T) {
	raw := []json.RawMessage{
		json.RawMessage(`{"role":"user","content":"hello"`), // missing closing brace
	}
	_, err := decodeMessages(raw)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestDecodeMessagesEmpty(t *testing.T) {
	msgs, err := decodeMessages(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected empty slice, got %d messages", len(msgs))
	}
}

func TestNewSessionID(t *testing.T) {
	id1, _ := agent.NewSessionID()
	if len(id1) != 32 {
		t.Fatalf("expected 32-char ID, got %d: %q", len(id1), id1)
	}
	id2, _ := agent.NewSessionID()
	if len(id2) != 32 {
		t.Fatalf("expected 32-char ID, got %d: %q", len(id2), id2)
	}
	if id1 == id2 {
		t.Errorf("expected two different IDs, got identical %q", id1)
	}
}

func TestNewSessionIDFormat(t *testing.T) {
	id, _ := agent.NewSessionID()
	if len(id) != 32 {
		t.Fatalf("expected 32-char ID, got %d: %q", len(id), id)
	}
	if _, err := hex.DecodeString(id); err != nil {
		t.Errorf("ID %q is not valid hexadecimal: %v", id, err)
	}
}

// TestSSEStreamEventAllCases exercises every type-switch arm in
// sseStreamEvent, including the default fallback for an unrecognized
// ai.StreamEvent implementation.
func TestSSEStreamEventAllCases(t *testing.T) {
	cases := []struct {
		name    string
		event   ai.StreamEvent
		want    string
		wantErr bool
	}{
		{"ThinkingChunk", ai.ThinkingChunk{Type: "thinking", Content: "hmm"}, "thinking_chunk", false},
		{"ResponseChunk", ai.ResponseChunk{Type: "response", Content: "hi"}, "response_chunk", false},
		{"ToolCallEvent", ai.ToolCallEvent{Type: "tool_call", ToolCall: ai.ToolCall{ID: "t1", Name: "shell"}}, "tool_call", false},
		{"StreamEnd", ai.StreamEnd{Type: "stream_end", FinishReason: "stop"}, "stream_end", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, data, err := sseStreamEvent(tc.event)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("event type = %q, want %q", got, tc.want)
			}
			if len(data) == 0 {
				t.Fatalf("expected non-empty data, got empty")
			}
		})
	}
}

// TestEventTypeOfAllCases covers every type-switch arm in eventTypeOf.
func TestEventTypeOfAllCases(t *testing.T) {
	cases := []struct {
		name  string
		event agent.Event
		want  string
	}{
		{"AgentStart", agent.AgentStart{Type: "agent_start", ModelName: "m"}, "agent_start"},
		{"TurnStart", agent.TurnStart{Type: "turn_start", Turn: 1}, "turn_start"},
		{"TurnEnd", agent.TurnEnd{Type: "turn_end", Turn: 1, ToolCalls: 0}, "turn_end"},
		{"AgentEnd", agent.AgentEnd{Type: "agent_end", Turns: 1, FinishReason: "stop"}, "agent_end"},
		{"AssistantMessageEvent", agent.AssistantMessageEvent{Type: "assistant_message", Message: ai.NewAssistant("hi")}, "assistant_message"},
		{"ToolResultEvent", agent.ToolResultEvent{Type: "tool_result", Message: ai.NewToolResult("ok", "t1", false)}, "tool_result"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := eventTypeOf(tc.event)
			if got != tc.want {
				t.Fatalf("eventTypeOf(%T) = %q, want %q", tc.event, got, tc.want)
			}
		})
	}
}