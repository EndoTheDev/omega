package verdict

// dotted_e2e_test.go: the split's acceptance witness (t_d8fbad57,
// phase 2). A scripted provider drives verdict.request through the
// REAL agent loop - the same wire a model rides - while a concurrent
// answerer resolves the card through the hub, the path the /verdicts
// POST shares. No server, no REST: the loop's tool dispatch on the
// dotted name is the surface under test.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/agent/loop"
	"github.com/EndoTheDev/omega/core"
)

func TestDottedRequestEndToEndThroughTheLoop(t *testing.T) {
	h := NewHub(nil, nil)
	p := &verdictToolProvider{hub: h}

	// The answerer: the loop executes tool calls in goroutines, so
	// verdict.request blocks the turn until this resolves the card
	// the way a user click would.
	answerErr := make(chan error, 1)
	go func() {
		deadline := time.Now().Add(testWaitTimeout)
		for time.Now().Before(deadline) {
			if cards := h.Pending(""); len(cards) > 0 {
				_, err := h.Answer("", cards[0].ID, core.QuestionAnswer{OptionIndex: 1})
				answerErr <- err
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		answerErr <- nil
	}()

	prov := agent.NewFakeProviderScripts("test",
		// Turn 1: the model issues the dotted verdict call.
		[]core.ProviderStreamEvent{
			core.ToolCallEvent{Type: "tool_call", ToolCall: core.ToolCall{
				ID: "c1", Name: "verdict.request", Arguments: map[string]any{
					"title":   "ship the split?",
					"options": []any{"yes", "no"},
				},
			}},
			core.StreamEnd{Type: "stream_end", FinishReason: "tool_call"},
		},
		// Turn 2: respond and stop.
		[]core.ProviderStreamEvent{
			core.ResponseChunk{Type: "response_chunk", Content: "done"},
			core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		},
	)
	a := agent.NewAgent(prov, nil, 0)
	a.SetLoopProvider(loop.Loop{})

	events := a.RunWithProvider(context.Background(), prov,
		[]core.Message{core.NewUser("ask me whether to ship")}, p.Tools())

	results := 0
	var resultContent string
	for e := range events {
		tre, ok := e.(core.ToolResultEvent)
		if !ok {
			continue
		}
		results++
		if tre.Message.IsError {
			t.Fatalf("tool result %s errored inside the loop: %s", tre.Message.ToolCallID, tre.Message.Content)
		}
		resultContent = tre.Message.Content
	}
	if err := <-answerErr; err != nil {
		t.Fatalf("answer through the hub: %v", err)
	}
	if results != 1 {
		t.Fatalf("the loop executed %d tool results, want 1", results)
	}
	// the wire's JSON verdict is the tool result the model reads
	// (option indexes are 1-based: index 1 = yes)
	if !strings.Contains(resultContent, `"verdict":"answer"`) || !strings.Contains(resultContent, `"option":"yes"`) || !strings.Contains(resultContent, `"option_index":1`) {
		t.Fatalf("verdict.request result = %s, want the answered pick (option yes)", resultContent)
	}
}
