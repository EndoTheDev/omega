package ai

import (
	"bufio"
	"net/http"
	"strings"
	"time"

	"github.com/EndoTheDev/omega/core"
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
// during config loading from the host config's HTTPTimeout field.
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

// Compatibility aliases: the provider contract (core.ToolSchema, core.Provider,
// ModelInfo) now lives in core (arc 1b2, t_d16468fd). Every provider
// lane and module import of ai.* keeps compiling; the aliases
// disappear when modules rewire in phase 2.
//
// DO NOT add new contract types here - new provider contracts belong
// in core.
type (
	// ToolSchema describes a tool the model may call.
	ToolSchema = core.ToolSchema
	// Provider is the interface for LLM provider implementations.
	Provider = core.Provider
	// ModelInfo holds metadata about the current model.
	ModelInfo = core.ModelInfo
)

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
