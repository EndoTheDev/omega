package loop

// The approval seam's provenance handoff: a gate that
// judges CALL PROVENANCE needs the human's own words, and the loop is the
// one place that has them. Pinned here: the loop hands the conversation to
// a provenance-aware gate, and a plain ApprovalProvider keeps working
// through the one-argument consult (the documented floor).

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// plainGate is an ApprovalProvider that predates the provenance upgrade.
type plainGate struct{ calls int32 }

func (p *plainGate) Gate(context.Context, string, map[string]any) (string, error) {
	atomic.AddInt32(&p.calls, 1)
	return "", nil
}

// provenanceGate records the conversation the loop hands it.
type provenanceGate struct {
	mu    sync.Mutex
	convo []core.Message
	plain int
}

func (g *provenanceGate) Gate(context.Context, string, map[string]any) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.plain++
	return "", nil
}

func (g *provenanceGate) GateWithConversation(_ context.Context, _ string, _ map[string]any, conversation []core.Message) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.convo = append([]core.Message(nil), conversation...)
	return "", nil
}

func (g *provenanceGate) snapshot() []core.Message {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]core.Message(nil), g.convo...)
}

// toolCallProvider scripts one tool call then a final answer.
func toolCallProvider() agent.Provider {
	return agent.NewFakeProviderScripts("test",
		[]core.ProviderStreamEvent{
			core.ToolCallEvent{Type: "tool_call", ToolCall: core.ToolCall{ID: "c1", Name: "echo", Arguments: map[string]any{"msg": "hi"}}},
			core.StreamEnd{Type: "stream_end", FinishReason: "tool_call"},
		},
		[]core.ProviderStreamEvent{
			core.ResponseChunk{Type: "response_chunk", Content: "done"},
			core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		},
	)
}

func echoTool() map[string]core.Tool {
	return map[string]core.Tool{
		"echo": {
			Description: "Echo back",
			Parameters:  map[string]any{"type": "object", "properties": map[string]any{"msg": map[string]any{"type": "string"}}},
			Run: func(_ context.Context, args map[string]any) (string, error) {
				msg, _ := args["msg"].(string)
				return msg, nil
			},
		},
	}
}

// TestGateCallHandsTheConversation: the gate receives the run's messages -
// the human's turn AND the model's own turn, so the gate can apply its own
// exclusion law (assistant turns never source a host).
func TestGateCallHandsTheConversation(t *testing.T) {
	gate := &provenanceGate{}
	runLoop(t, context.Background(), agent.LoopOptions{
		Provider:  toolCallProvider(),
		Messages:  []core.Message{core.NewUser("go")},
		Tools:     echoTool(),
		Approvals: gate,
	})

	convo := gate.snapshot()
	if len(convo) < 2 {
		t.Fatalf("conversation handed to the gate = %d messages, want the run's history", len(convo))
	}
	sawUser, sawModelCall := false, false
	for _, m := range convo {
		switch msg := m.(type) {
		case core.User:
			// The day's first user message carries the date stamp: the gate reads the
			// human's words, not a fixed prefix.
			if strings.HasSuffix(msg.Content, "go") {
				sawUser = true
			}
		case core.Assistant:
			for _, call := range msg.ToolCalls {
				if call.ID == "c1" {
					sawModelCall = true
				}
			}
		}
	}
	if !sawUser {
		t.Fatalf("the human's turn is missing from the snapshot: %+v", convo)
	}
	if !sawModelCall {
		t.Fatalf("the model's own turn is missing from the snapshot: %+v", convo)
	}
	if gate.plain != 0 {
		t.Fatalf("the provenance consult fell back to the plain gate: %d calls", gate.plain)
	}
}

// TestGateCallFallsBackToThePlainConsult: a gate that does not implement
// GateProvenance keeps the one-argument consult.
func TestGateCallFallsBackToThePlainConsult(t *testing.T) {
	gate := &plainGate{}
	runLoop(t, context.Background(), agent.LoopOptions{
		Provider:  toolCallProvider(),
		Messages:  []core.Message{core.NewUser("go")},
		Tools:     echoTool(),
		Approvals: gate,
	})
	if got := atomic.LoadInt32(&gate.calls); got != 1 {
		t.Fatalf("plain gate consulted %d times, want 1", got)
	}
}
