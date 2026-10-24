package ai

import (
	"context"
	"github.com/EndoTheDev/omega/core"
	"testing"
)

func TestNewFakeProviderScripts(t *testing.T) {
	script1 := []core.ProviderStreamEvent{
		core.ResponseChunk{Type: "response", Content: "first"},
		core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	}
	script2 := []core.ProviderStreamEvent{
		core.ResponseChunk{Type: "response", Content: "second"},
		core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	}
	p := NewFakeProviderScripts("fake", script1, script2)

	// Call 1 — should emit script1.
	drain := func(ch <-chan core.ProviderStreamEvent) []core.ProviderStreamEvent {
		var got []core.ProviderStreamEvent
		for e := range ch {
			got = append(got, e)
		}
		return got
	}

	got1 := drain(p.Stream(context.Background(), nil, nil))
	if len(got1) != len(script1) {
		t.Fatalf("call 1: got %d events, want %d", len(got1), len(script1))
	}
	if got1[0].(core.ResponseChunk).Content != "first" {
		t.Errorf("call 1 first event = %v, want content %q", got1[0], "first")
	}

	// Call 2 — should emit script2.
	got2 := drain(p.Stream(context.Background(), nil, nil))
	if len(got2) != len(script2) {
		t.Fatalf("call 2: got %d events, want %d", len(got2), len(script2))
	}
	if got2[0].(core.ResponseChunk).Content != "second" {
		t.Errorf("call 2 first event = %v, want content %q", got2[0], "second")
	}

	// Call 3 — last script repeats.
	got3 := drain(p.Stream(context.Background(), nil, nil))
	if len(got3) != len(script2) {
		t.Fatalf("call 3: got %d events, want %d (last script repeat)", len(got3), len(script2))
	}
	if got3[0].(core.ResponseChunk).Content != "second" {
		t.Errorf("call 3 first event = %v, want content %q (last script repeat)", got3[0], "second")
	}
}

func TestFakeProviderCalls(t *testing.T) {
	p := NewFakeProvider("fake",
		core.ResponseChunk{Type: "response", Content: "hi"},
		core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	)
	if p.Calls() != 0 {
		t.Fatalf("Calls() = %d before any Stream, want 0", p.Calls())
	}
	ch := p.Stream(context.Background(), nil, nil)
	for range ch {
	}
	if p.Calls() != 1 {
		t.Errorf("Calls() = %d after 1 Stream, want 1", p.Calls())
	}
	ch = p.Stream(context.Background(), nil, nil)
	for range ch {
	}
	if p.Calls() != 2 {
		t.Errorf("Calls() = %d after 2 Streams, want 2", p.Calls())
	}
}

func TestFakeProviderModelName(t *testing.T) {
	p := NewFakeProvider("test-model")
	if name := p.ModelName(); name != "test-model" {
		t.Errorf("ModelName() = %q, want %q", name, "test-model")
	}
}

func TestFakeProviderWithModel(t *testing.T) {
	p := NewFakeProvider("old-model")
	cp := p.WithModel("new-model")
	if name := p.ModelName(); name != "old-model" {
		t.Errorf("after WithModel, original ModelName() = %q, want %q", name, "old-model")
	}
	if name := cp.ModelName(); name != "new-model" {
		t.Errorf("copy ModelName() = %q, want %q", name, "new-model")
	}
}

func TestFakeProviderSetThinkingLevel(t *testing.T) {
	p := NewFakeProvider("fake")
	// No-op — just verify it doesn't panic.
	p.SetThinkingLevel("high")
}

func TestFakeProviderListModels(t *testing.T) {
	p := NewFakeProvider("fake")
	models, err := p.ListModels()
	if err != nil {
		t.Fatalf("ListModels() error: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("ListModels() returned %d models, want 2", len(models))
	}
	if models[0] != "fake" {
		t.Errorf("models[0] = %q, want %q", models[0], "fake")
	}
	if models[1] != "other-model" {
		t.Errorf("models[1] = %q, want %q", models[1], "other-model")
	}
}

func TestFakeProviderModelInfo(t *testing.T) {
	p := NewFakeProvider("fake")
	info, err := p.ModelInfo()
	if err != nil {
		t.Fatalf("ModelInfo() error: %v", err)
	}
	if info.ContextWindow != 8192 {
		t.Errorf("ContextWindow = %d, want 8192", info.ContextWindow)
	}
}
