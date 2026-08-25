package ai

import (
	"bufio"
	"context"
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

// HTTPClient returns the shared HTTP client. Extensions import this
// to make HTTP calls with the same timeout and proxy settings.
func HTTPClient() *http.Client {
	return httpClient
}

// RetryHTTP runs req with exponential backoff on transient failures.
// Extensions import this for provider HTTP calls.
func RetryHTTP(req *http.Request) (*http.Response, error) {
	return retryHTTP(req.Context(), req)
}

// SSEData returns the payload of each `data:` line in an SSE stream,
// skipping comments, event/blank lines, and the trailing `[DONE]`
// sentinel. Extensions import this for parsing SSE responses.
func SSEData(reader *bufio.Reader) (string, bool, error) {
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
	SetModel(model string)
	ListModels() ([]string, error)
	ModelInfo() (ModelInfo, error)
}

// ModelInfo holds metadata about the current model, queried from the
// provider when available. Zero values mean the provider does not
// expose that field; callers fall back to config defaults.
type ModelInfo struct {
	ContextWindow int // max context in tokens, 0 if unknown
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
