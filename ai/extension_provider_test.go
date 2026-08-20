package ai

import (
	"testing"
)

func TestExtensionProviderNilDispatcher(t *testing.T) {
	p := ExtensionProvider{Dispatcher: nil}

	// Stream should return a single error StreamEnd.
	ch := p.Stream(nil, nil, nil)
	if ch == nil {
		t.Fatal("Stream returned nil channel")
	}
	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	end, ok := events[0].(StreamEnd)
	if !ok {
		t.Fatalf("event type = %T, want StreamEnd", events[0])
	}
	if end.FinishReason != "error" {
		t.Errorf("FinishReason = %q, want \"error\"", end.FinishReason)
	}
	if end.Error == "" {
		t.Error("Error is empty, want non-empty error message")
	}

	// ModelName should return empty string.
	if name := p.ModelName(); name != "" {
		t.Errorf("ModelName = %q, want \"\"", name)
	}

	// SetThinkingLevel should be a no-op (no panic).
	p.SetThinkingLevel("high")

	// ListModels should return an error.
	models, err := p.ListModels()
	if err == nil {
		t.Error("ListModels err = nil, want error")
	}
	if models != nil {
		t.Errorf("ListModels = %v, want nil", models)
	}
}