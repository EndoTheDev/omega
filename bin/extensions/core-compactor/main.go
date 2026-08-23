// core-compactor is an omega extension that implements the compactor seam.
// It receives the full message history and compaction config via JSON-RPC,
// estimates tokens, and when over budget: keeps the first N and last M
// messages, LLM-summarizes the middle, and returns the compacted list.
//
// Seam: compactor
// Methods: compaction/compact
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/EndoTheDev/omega/ai"
)

// --- omega extension protocol types ---

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int            `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// --- compaction config (mirrors agent.CompactionConfig) ---

type compactionConfig struct {
	Enabled       bool    `json:"enabled"`
	Threshold     float64 `json:"threshold"`
	ContextWindow int     `json:"context_window"`
	KeepFirst     int     `json:"keep_first"`
	KeepLast      int     `json:"keep_last"`
	ReserveTokens int     `json:"reserve_tokens"`
}

// --- provider state (from env vars, same as core-provider) ---

var (
	providerType string
	modelName    string
	baseURL      string
	apiKey       string
)

const (
	charsPerToken     = 4
	defaultContextWin = 8192
	defaultReserve    = 16384
)

func main() {
	providerType = os.Getenv("OMEGA_PROVIDER_TYPE")
	if providerType == "" {
		providerType = "ollama"
	}
	modelName = os.Getenv("OMEGA_PROVIDER_MODEL")
	baseURL = os.Getenv("OMEGA_PROVIDER_HOST")
	apiKey = os.Getenv("OLLAMA_API_KEY")

	stdin := bufio.NewReader(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)

	for {
		line, err := stdin.ReadString('\n')
		if err != nil {
			return
		}
		var req rpcRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			continue
		}

		switch req.Method {
		case "initialize":
			result, _ := json.Marshal(map[string]any{
				"name":         "core-compactor",
				"seams":        []string{"compactor"},
				"subscriptions": []string{},
			})
			if req.ID != nil {
				encoder.Encode(rpcResponse{JSONRPC: "2.0", ID: *req.ID, Result: result})
			}

		case "compaction/compact":
			handleCompact(encoder, req)
		}
	}
}

func handleCompact(encoder *json.Encoder, req rpcRequest) {
	var params struct {
		Messages []map[string]any `json:"messages"`
		Config   compactionConfig  `json:"config"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		encoder.Encode(rpcResponse{JSONRPC: "2.0", ID: *req.ID, Error: &rpcError{Code: -32602, Message: err.Error()}})
		return
	}

	if !params.Config.Enabled {
		encodeMessages(encoder, req.ID, params.Messages)
		return
	}

	// Estimate tokens. ponytail: 4 chars/token, same as agent package.
	totalChars := 0
	for _, m := range params.Messages {
		if c, ok := m["content"].(string); ok {
			totalChars += len(c)
		}
	}
	estTokens := totalChars / charsPerToken

	// Check budget.
	window := params.Config.ContextWindow
	if window <= 0 {
		window = defaultContextWin
	}
	reserve := params.Config.ReserveTokens
	if reserve <= 0 {
		reserve = defaultReserve
	}
	effective := window - reserve
	if effective < window/2 {
		effective = window / 2
	}
	budget := int(float64(effective) * params.Config.Threshold)

	keepFirst := params.Config.KeepFirst
	keepLast := params.Config.KeepLast

	if estTokens <= budget || keepFirst+keepLast >= len(params.Messages) {
		// Under budget or nothing to compact: return messages unchanged.
		encodeMessages(encoder, req.ID, params.Messages)
		return
	}

	// Summarize the middle.
	middle := params.Messages[keepFirst : len(params.Messages)-keepLast]
	summary, err := summarize(middle)
	if err != nil {
		encoder.Encode(rpcResponse{JSONRPC: "2.0", ID: *req.ID, Error: &rpcError{Code: -32603, Message: err.Error()}})
		return
	}

	// Assemble: [first...] + [system: "[compacted: summary]"] + [last...]
	result := make([]map[string]any, 0, keepFirst+keepLast+1)
	result = append(result, params.Messages[:keepFirst]...)
	result = append(result, map[string]any{
		"role":    "system",
		"content": "[compacted: " + summary + "]",
	})
	result = append(result, params.Messages[len(params.Messages)-keepLast:]...)

	encodeMessages(encoder, req.ID, result)
}

// encodeMessages encodes messages as (role, payload) pairs for
// decodeMessages in the agent package. Each message map is split:
// "role" becomes the role field, the rest is marshalled as payload.
// This preserves all message fields (tool_calls, tool_call_id,
// images, thinking, etc.) through the round-trip.
func encodeMessages(encoder *json.Encoder, reqID *int, msgs []map[string]any) {
	pairs := make([]struct {
		Role    string `json:"role"`
		Payload string `json:"payload"`
	}, len(msgs))
	for i, m := range msgs {
		role, _ := m["role"].(string)
		// Marshal the message without role as payload.
		copyM := make(map[string]any, len(m)-1)
		for k, v := range m {
			if k != "role" {
				copyM[k] = v
			}
		}
		payload, _ := json.Marshal(copyM)
		pairs[i].Role = role
		pairs[i].Payload = string(payload)
	}
	encoded, _ := json.Marshal(pairs)
	encoder.Encode(rpcResponse{JSONRPC: "2.0", ID: *reqID, Result: encoded})
}

// summarize calls the LLM to summarize a slice of messages.
func summarize(middle []map[string]any) (string, error) {
	if providerType != "ollama" {
		return "", fmt.Errorf("core-compactor: unsupported provider type %q for summarization", providerType)
	}

	var b strings.Builder
	b.WriteString("Summarize the following conversation concisely, preserving key facts, decisions, and context. Output only the summary.\n\n")
	for _, m := range middle {
		role, _ := m["role"].(string)
		content, _ := m["content"].(string)
		b.WriteString("- [")
		b.WriteString(role)
		b.WriteString("] ")
		b.WriteString(content)
		b.WriteString("\n")
	}

	payload := map[string]any{
		"model":  modelName,
		"stream": false,
		"messages": []map[string]any{
			{"role": "user", "content": b.String()},
		},
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", baseURL+"/api/chat", bytes.NewReader(payloadBytes))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := ai.RetryHTTP(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("summarize: HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("summarize: %w", err)
	}
	summary := strings.TrimSpace(result.Message.Content)
	if summary == "" {
		return "", fmt.Errorf("summarize: empty summary")
	}
	return summary, nil
}
