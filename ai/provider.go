package ai

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// httpClient is the shared HTTP client used by all providers. It
// respects HTTP_PROXY and HTTPS_PROXY environment variables via
// http.ProxyFromEnvironment. The timeout defaults to 300s and is
// set via SetHTTPTimeout from config loading.
var httpClient = &http.Client{
	Timeout: 300 * time.Second,
	Transport: &http.Transport{
		Proxy: http.ProxyFromEnvironment,
	},
}

// SetHTTPTimeout updates the shared HTTP client's timeout. Called
// during config loading from gateway.Config.HTTPTimeout.
func SetHTTPTimeout(seconds int) {
	if seconds > 0 {
		httpClient.Timeout = time.Duration(seconds) * time.Second
	}
}

// NewProvider creates a Provider of the given type ("ollama", "openai",
// or "anthropic"). apiKey may be empty; OpenAI and Anthropic fall back
// to their OPENAI_API_KEY / ANTHROPIC_API_KEY env vars, and Ollama uses
// it for Ollama Cloud direct connections (empty for local). host may be empty to use the provider default base URL.
func NewProvider(providerType, model, host, apiKey string) (Provider, error) {
	switch providerType {
	case "", "ollama":
		return NewOllamaProvider(model, host, apiKey), nil
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
	SetThinkingLevel(level string)
	ListModels() ([]string, error)
}

// ThinkingLevels is the ordered list of thinking levels the user can
// cycle through with /thinking (no argument). "none" is the default:
// no thinking parameter is sent to the provider. "off" explicitly
// disables thinking. The rest enable thinking at increasing intensity.
var ThinkingLevels = []string{"none", "off", "on", "minimal", "low", "medium", "high", "extra high", "max", "ultra"}

// ThinkingEnabled returns true if the level enables thinking (anything
// except "none" and "off").
func ThinkingEnabled(level string) bool {
	return level != "" && level != "none" && level != "off"
}

// openaiReasoningEffort maps a thinking level to OpenAI's
// reasoning_effort parameter. OpenAI supports only low/medium/high.
func openaiReasoningEffort(level string) string {
	switch level {
	case "minimal", "low":
		return "low"
	case "medium":
		return "medium"
	case "high", "extra high", "max", "ultra":
		return "high"
	default:
		return ""
	}
}

// anthropicBudgetTokens maps a thinking level to Anthropic's
// budget_tokens parameter. Higher levels get more tokens.
func anthropicBudgetTokens(level string) int {
	switch level {
	case "minimal":
		return 1024
	case "low":
		return 2048
	case "medium":
		return 4096
	case "high":
		return 8192
	case "extra high":
		return 16384
	case "max":
		return 24576
	case "ultra":
		return 32768
	default:
		return 0
	}
}

// ollamaThinkValue maps a thinking level to Ollama's think parameter.
// Ollama accepts true/false or levels (low, medium, high, max).
func ollamaThinkValue(level string) any {
	switch level {
	case "off":
		return false
	case "on":
		return true
	case "minimal", "low":
		return "low"
	case "medium":
		return "medium"
	case "high":
		return "high"
	case "extra high", "max", "ultra":
		return "max"
	default:
		return nil
	}
}
