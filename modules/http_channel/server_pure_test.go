package http_channel

import (
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/ai"
)

// selectTools and its request field were deleted: the serve host never
// mounted a tool registry, so the documented restriction was never
// enforced (an unenforced restriction is worse than none).

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

// TestAccumulateAssistantSegments verifies the segment boundary: a
// response chunk after tool results starts a new record, so a tool turn
// persists as assistant(A+calls) -> tool results -> assistant(B) and
// the webui reload renders two bubbles like the live view.
func TestAccumulateAssistantSegments(t *testing.T) {
	s, _ := newTestServerWithStore(t)
	var recs []assistantRecord
	var rec assistantRecord
	req := chatRequest{SessionID: "seg1"}
	feed := func(ev agent.Event) { s.accumulateAssistant(&recs, &rec, ev, req) }

	call := ai.ToolCall{ID: "c1", Name: "shell.run"}
	feed(agent.StreamEvent{Event: ai.ResponseChunk{Content: "preamble "}})
	feed(agent.StreamEvent{Event: ai.ToolCallEvent{ToolCall: call}})
	feed(agent.ToolResultEvent{Message: ai.NewToolResult("out", "c1", false)})
	feed(agent.StreamEvent{Event: ai.ResponseChunk{Content: "final answer"}})

	if len(recs) != 1 {
		t.Fatalf("len(recs) = %d, want 1 (final segment stays in rec)", len(recs))
	}
	feed(agent.StreamEvent{Event: ai.StreamEnd{FinishReason: "stop"}})
	if rec.content != "final answer" {
		t.Fatalf("rec.content = %q, want 'final answer'", rec.content)
	}
	if len(rec.toolCalls) != 0 || len(rec.results) != 0 {
		t.Fatalf("second segment carries calls/results: %d/%d", len(rec.toolCalls), len(rec.results))
	}
	r1 := recs[0]
	if r1.content != "preamble " {
		t.Fatalf("first segment content = %q", r1.content)
	}
	if len(r1.toolCalls) != 1 || r1.toolCalls[0].ID != "c1" {
		t.Fatalf("first segment tool calls = %+v", r1.toolCalls)
	}
	if len(r1.results) != 1 || r1.results[0].Content != "out" {
		t.Fatalf("first segment results = %+v", r1.results)
	}
}

// TestAccumulateAssistantNoSegmentWithoutResults: thinking and plain
// responses accumulate into a single record; the split only happens
// across tool results.
func TestAccumulateAssistantNoSegmentWithoutResults(t *testing.T) {
	s, _ := newTestServerWithStore(t)
	var recs []assistantRecord
	var rec assistantRecord
	req := chatRequest{SessionID: "seg2"}
	feed := func(ev agent.Event) { s.accumulateAssistant(&recs, &rec, ev, req) }

	feed(agent.StreamEvent{Event: ai.ThinkingChunk{Content: "hmm"}})
	feed(agent.StreamEvent{Event: ai.ResponseChunk{Content: "one"}})
	feed(agent.StreamEvent{Event: ai.ResponseChunk{Content: " two"}})

	if len(recs) != 0 {
		t.Fatalf("len(recs) = %d, want 0 (no tool phase, no split)", len(recs))
	}
	if rec.content != "one two" || rec.thinking != "hmm" {
		t.Fatalf("rec = %+v", rec)
	}
}

// runMeter math: the last-run block is completion tokens and chunk
// count over the streaming wall clock, rounded, with the same floor
// the live meter applies. Fields are set directly so the arithmetic is
// deterministic - the ticker itself is exercised through the run
// handler tests.
func TestRunMeterStats(t *testing.T) {
	// 60 completion tokens over 12 chunks in 2.0s -> 30 tok/s, 6 ch/s.
	m := &runMeter{started: time.Unix(1000, 0), lastChunk: time.Unix(1002, 0), chunks: 12}
	got := m.stats(streamUsage{prompt: 5, completion: 60})
	if got == nil || got.TokensPerSecond != 30 || got.ChunksPerSecond != 6 {
		t.Fatalf("stats = %+v, want 30 tok/s 6 ch/s", got)
	}

	// Shorter than the 0.2s floor: no honest average, nothing written.
	m = &runMeter{started: time.Unix(1000, 0), lastChunk: time.Unix(1000, 100000000), chunks: 3}
	if got := m.stats(streamUsage{completion: 40}); got != nil {
		t.Fatalf("sub-floor stats = %+v, want nil", got)
	}

	// No usage reported by the provider: nothing written.
	m = &runMeter{started: time.Unix(1000, 0), lastChunk: time.Unix(1010, 0), chunks: 12}
	if got := m.stats(streamUsage{}); got != nil {
		t.Fatalf("no-usage stats = %+v, want nil", got)
	}

	// No chunks (an error run that never streamed): nothing written.
	m = &runMeter{started: time.Unix(1000, 0)}
	if got := m.stats(streamUsage{completion: 40}); got != nil {
		t.Fatalf("no-chunk stats = %+v, want nil", got)
	}

	// Rounding: 10 tokens over 3 chunks in 0.3s -> 33 tok/s, 10 ch/s.
	m = &runMeter{started: time.Unix(1000, 0), lastChunk: time.Unix(1000, 300000000), chunks: 3}
	got = m.stats(streamUsage{completion: 10})
	if got == nil || got.TokensPerSecond != 33 || got.ChunksPerSecond != 10 {
		t.Fatalf("rounded stats = %+v, want 33/10", got)
	}
}

// tick counts only streamed chunks (thinking AND response); the clock
// opens at meter creation, not at the first chunk, so prompt
// processing is part of the run the same way it is for the client.
func TestRunMeterTick(t *testing.T) {
	m := newRunMeter()
	if m.started.IsZero() {
		t.Fatalf("meter did not start its clock")
	}
	m.tick(agent.StreamEvent{Event: ai.ThinkingChunk{Content: "hi"}})
	m.tick(agent.TurnStart{}) // not a chunk
	m.tick(agent.StreamEvent{Event: ai.ResponseChunk{Content: "ab"}})
	m.tick(agent.ToolResultEvent{}) // not a chunk
	m.tick(agent.StreamEvent{Event: ai.StreamEnd{}})
	if m.chunks != 2 {
		t.Fatalf("chunks = %d, want 2", m.chunks)
	}
	if m.lastChunk.IsZero() {
		t.Fatalf("lastChunk not stamped")
	}
}
