package llm

import (
	"bufio"
	"net/http"
	"strings"
	"time"

	"github.com/EndoTheDev/omega/core"
)

// httpClient is the shared HTTP client used by all providers.
var httpClient = &http.Client{
	Timeout: 300 * time.Second,
	Transport: &http.Transport{
		Proxy: http.ProxyFromEnvironment,
	},
}

// SetHTTPTimeout updates the shared HTTP client's timeout.
func SetHTTPTimeout(seconds int) {
	if seconds > 0 {
		httpClient.Timeout = time.Duration(seconds) * time.Second
	}
}

// HTTPClient returns the shared HTTP client.
func HTTPClient() *http.Client {
	return httpClient
}

// RetryHTTP runs req with exponential backoff on transient failures.
func RetryHTTP(req *http.Request) (*http.Response, error) {
	return retryHTTP(req.Context(), req)
}

// SSEData returns the payload of each `data:` line in an SSE stream, skipping.
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

// Compatibility aliases: the tool + model wire shapes live in core (arc 1b2). They are
// Go aliases of the core wire types, not a second contract. The Provider face itself
// left core in part 3 (wave 2, the seam constitution): consumers declare it
// (agent.Provider); llm keeps no provider contract at all. DO NOT add new contract
// types here - provider contracts belong to their consumers.
type (
	// ToolSchema describes a tool the model may call.
	ToolSchema = core.ToolSchema
	// ModelInfo holds metadata about the current model.
	ModelInfo = core.ModelInfo
)

// ThinkingLevels is the ordered list of thinking levels the user can cycle through.
var ThinkingLevels = []string{"none", "off", "on", "minimal", "low", "medium", "high", "extra high", "max", "ultra"}

// ThinkingEnabled returns true if the level enables thinking (anything except "none".
func ThinkingEnabled(level string) bool {
	return level != "" && level != "none" && level != "off"
}
