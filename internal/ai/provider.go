package ai

import (
	"bufio"
	"context"
	"fmt"
	"strings"
)

// NewProvider creates a Provider of the given type ("ollama", "openai",
// or "anthropic"). apiKey may be empty; OpenAI and Anthropic fall back
// to their OPENAI_API_KEY / ANTHROPIC_API_KEY / ANTHROPIC_API_KEY env vars, and Ollama
// ignores it. host may be empty to use the provider default base URL.
func NewProvider(providerType, model, host, apiKey string) (Provider, error) {
	switch providerType {
	case "", "ollama":
		return NewOllamaProvider(model, host), nil
	case "openai":
		return NewOpenAIProvider(model, host, apiKey), nil
	case "anthropic":
		return NewAnthropicProvider(model, host, apiKey), nil
	default:
		return nil, fmt.Errorf("unknown provider type %q (want ollama, openai, or anthropic)", providerType)
	}
}

// sseData returns the payload of each `data:` line in an SSE stream,
// skipping comments, event/blank lines, and the trailing `[DONE]`
// sentinel. It is shared by the OpenAI and Anthropic providers.
func sseData(reader *bufio.Reader) (string, bool, error) {
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", false, err
		}
		line = strings.TrimRight(line, "\r\n")
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		return payload, true, nil
	}
}

// ToolSchema describes a tool the model may call. It is passed to
// the provider and serialized into the API request.
type ToolSchema struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// Provider is the interface for LLM provider implementations.
// Stream returns a channel of stream events. Errors are encoded as
// StreamEnd(FinishReason="error", Error=...), not returned as Go
// errors. The channel is closed when the stream ends.
type Provider interface {
	Stream(ctx context.Context, messages []Message, tools []ToolSchema) <-chan StreamEvent
	ModelName() string
}
