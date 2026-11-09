package provider

// The mock provider pins (card t_150ce9fc): deterministic streams
// seeded by the request, the call-first-tool-once-then-answer behavior
// through the REAL agent loop, and the zero-network model paths - the
// model/list probes below run against a provider whose URL points at
// a closed port, so any HTTP attempt would error loudly.

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/agent/loop"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/llm"
)

func newMockProvider(model string) *Provider {
	return NewProvider("mock", model, "", "")
}

func collectStream(ctx context.Context, p *Provider, msgs []core.Message, tools []llm.ToolSchema) (chunks string, finish string, streamErr string) {
	for e := range p.Stream(ctx, msgs, tools) {
		switch e := e.(type) {
		case core.ResponseChunk:
			chunks += e.Content
		case core.StreamEnd:
			finish = e.FinishReason
			streamErr = e.Error
		}
	}
	return chunks, finish, streamErr
}

func TestMockStreamDeterministic(t *testing.T) {
	msgs := []core.Message{core.NewSystem("system line"), core.NewUser("hello mock")}
	ch1, fin1, err1 := collectStream(context.Background(), newMockProvider("mock-model"), msgs, nil)
	ch2, fin2, err2 := collectStream(context.Background(), newMockProvider("mock-model"), msgs, nil)
	if err1 != "" || err2 != "" {
		t.Fatalf("stream errors: %q / %q", err1, err2)
	}
	if fin1 != "stop" || fin2 != "stop" {
		t.Fatalf("finish = %q / %q, want stop / stop", fin1, fin2)
	}
	if ch1 == "" || ch1 != ch2 {
		t.Fatalf("reply = %q vs %q, want identical non-empty deterministic replies", ch1, ch2)
	}
	if !strings.HasPrefix(ch1, "[mock prompt ") {
		t.Fatalf("reply = %q, want the [mock prompt <seed>] shape", ch1)
	}
	// Different input, different reply.
	ch3, _, _ := collectStream(context.Background(), newMockProvider("mock-model"), []core.Message{core.NewUser("different question")}, nil)
	if ch3 == ch1 {
		t.Fatalf("differing inputs answered identically: %q", ch1)
	}
	// Same input through a fresh copy of an existing provider (the
	// WithModel/WithThinkingLevel shape) stays deterministic.
	base := newMockProvider("mock-model")
	ch4, fin4, _ := collectStream(context.Background(), base, msgs, nil)
	ch5, fin5, _ := collectStream(context.Background(), base.WithThinkingLevel("high").(*Provider), msgs, nil)
	if ch4 != ch2 || fin4 != fin2 || ch5 != ch2 || fin5 != fin2 {
		t.Fatalf("copy discipline broke determinism: %q/%q vs %q/%q", ch4, ch5, ch1, fin1)
	}
}

func TestMockFullAgentTurn(t *testing.T) {
	p := newMockProvider("mock-model")
	ran := 0
	echoText := ""
	tools := map[string]core.Tool{
		"echo": {
			Description: "echo a text argument",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"text": map[string]any{"type": "string"},
				},
				"required": []any{"text"},
			},
			Run: func(_ context.Context, args map[string]any) (string, error) {
				s, ok := args["text"].(string)
				if !ok {
					return "", fmt.Errorf("missing or non-string text")
				}
				ran++
				echoText = s
				return "echo:" + s, nil
			},
		},
	}
	a := agent.NewAgent(p, tools, 0)
	a.SetLoopProvider(loop.Loop{})
	var end core.AgentEnd
	var toolRes core.ToolResult
	for e := range a.RunWithProvider(context.Background(), p, []core.Message{core.NewUser("call echo please")}, tools) {
		switch ev := e.(type) {
		case core.AgentEnd:
			end = ev
		case core.ToolResultEvent:
			toolRes = ev.Message
		}
	}
	if end.FinishReason != "stop" || end.Error != "" {
		t.Fatalf("AgentEnd = %+v, want a clean 2-turn stop", end)
	}
	if end.Turns != 2 {
		t.Fatalf("turns = %d, want 2 (tool call turn + answer turn)", end.Turns)
	}
	if ran != 1 {
		t.Fatalf("tool ran %d times, want exactly 1", ran)
	}
	if !strings.HasPrefix(echoText, "mock-") {
		t.Fatalf("arg filled = %q, want the seeded mock- filler", echoText)
	}
	if toolRes.IsError || strings.TrimSpace(toolRes.Content) != "echo:"+echoText {
		t.Fatalf("tool result = %+v, want the echo output without error", toolRes)
	}
	// The final assistant message is the post-tool reply.
	if !strings.HasPrefix(end.Message.Content, "[mock tool ") {
		t.Fatalf("final message = %q, want the seeded post-tool reply", end.Message.Content)
	}
}

func TestMockModelPathsNeverDial(t *testing.T) {
	// A model name plus list request: ListModels/ModelInfo must
	// answer without touching the network. The baseURL below is
	// never read by the mock paths - if the mock ever dials, the
	// dead port fails the test.
	p := &Provider{typ: "mock", modelName: "mock-model", baseURL: "http://127.0.0.1:1"}
	models, err := p.ListModels()
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) != 1 || models[0] != "mock-model" {
		t.Fatalf("ListModels = %v, want [mock-model]", models)
	}
	info, err := p.ModelInfo()
	if err != nil {
		t.Fatalf("ModelInfo: %v", err)
	}
	_ = info // zero context window = the caller's fallback ladder

	// The /models command path through a mock provider answers too.
	_, cmdErr := (&Plugin{}).handleCommand(p, "/models", "")
	if cmdErr != nil {
		t.Fatalf("/models: %v", cmdErr)
	}
	if _, ok := p.Instance("nope"); ok {
		t.Fatalf("unknown instance answered found")
	}
}
