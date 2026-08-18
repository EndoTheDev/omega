package ai

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// TestOllamaStream verifies the full streaming pipeline against a
// live Ollama instance. It requires OLLAMA_HOST and OLLAMA_MODEL to
// be set in the environment; otherwise it skips.
func TestOllamaStream(t *testing.T) {
	host := os.Getenv("OLLAMA_HOST")
	model := os.Getenv("OLLAMA_MODEL")
	if host == "" || model == "" {
		t.Skip("OLLAMA_HOST and OLLAMA_MODEL not set; skipping live stream test")
	}

	provider := NewOllamaProvider(model, host, "")
	if provider.ModelName() != model {
		t.Fatalf("expected model %q, got %q", model, provider.ModelName())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	events := provider.Stream(ctx, []Message{
		NewSystem("You are a terse assistant. Reply in one short sentence."),
		NewUser("Say hello."),
	}, nil)

	var sawResponse, sawEnd bool
	var response strings.Builder
	for event := range events {
		switch e := event.(type) {
		case ResponseChunk:
			sawResponse = true
			response.WriteString(e.Content)
		case ThinkingChunk:
			// acceptable; some models emit thinking
		case ToolCallEvent:
			t.Fatalf("unexpected tool call in plain chat: %s", e.ToolCall.Name)
		case StreamEnd:
			sawEnd = true
			if e.FinishReason == "error" {
				t.Fatalf("stream error: %s", e.Error)
			}
		}
	}

	if !sawResponse {
		t.Fatal("expected at least one ResponseChunk")
	}
	if !sawEnd {
		t.Fatal("expected StreamEnd")
	}
	if strings.TrimSpace(response.String()) == "" {
		t.Fatal("expected non-empty response content")
	}
	t.Logf("response: %q", response.String())
}
