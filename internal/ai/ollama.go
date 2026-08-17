package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
)

// OllamaProvider implements Provider for the Ollama API.
type OllamaProvider struct {
	modelName     string
	baseURL       string
	apiKey        string
	thinkingLevel string
}

// NewOllamaProvider creates an OllamaProvider. If baseURL is empty,
// it defaults to OLLAMA_HOST or "http://localhost:11434". If
// modelName is empty, it defaults to OLLAMA_MODEL. apiKey is used
// for Ollama Cloud direct connections; empty for local.
func NewOllamaProvider(modelName, baseURL, apiKey string) *OllamaProvider {
	if baseURL == "" {
		baseURL = os.Getenv("OLLAMA_HOST")
	}
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	if modelName == "" {
		modelName = os.Getenv("OLLAMA_MODEL")
	}
	return &OllamaProvider{
		modelName: modelName,
		baseURL:   strings.TrimRight(baseURL, "/"),
		apiKey:    apiKey,
	}
}

// ModelName returns the model name used by this provider.
func (p *OllamaProvider) ModelName() string {
	return p.modelName
}

// SetThinkingLevel sets the thinking level. Ollama only supports on/off;
// any level except "none" and "off" enables thinking.
func (p *OllamaProvider) SetThinkingLevel(level string) {
	p.thinkingLevel = level
}

// ListModels fetches available models from the Ollama API (/api/tags).
func (p *OllamaProvider) ListModels() ([]string, error) {
	req, err := http.NewRequest("GET", p.baseURL+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama: HTTP %d", resp.StatusCode)
	}
	var result struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(result.Models))
	for _, m := range result.Models {
		names = append(names, m.Name)
	}
	sort.Strings(names)
	return names, nil
}

// messagesToAPI converts internal Message types to Ollama API format.
func (p *OllamaProvider) messagesToAPI(messages []Message) []map[string]any {
	result := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		switch m := message.(type) {
		case System:
			result = append(result, map[string]any{
				"role":    "system",
				"content": m.Content,
			})
		case User:
			msg := map[string]any{
				"role":    "user",
				"content": m.Content,
			}
			if len(m.Images) > 0 {
				images := make([]string, 0, len(m.Images))
				for _, img := range m.Images {
					images = append(images, img.Base64)
				}
				msg["images"] = images
			}
			result = append(result, msg)
		case Assistant:
			apiMessage := map[string]any{
				"role":    "assistant",
				"content": m.Content,
			}
			if m.Thinking != nil {
				apiMessage["thinking"] = *m.Thinking
			}
			if len(m.ToolCalls) > 0 {
				calls := make([]map[string]any, 0, len(m.ToolCalls))
				for _, toolCall := range m.ToolCalls {
					calls = append(calls, map[string]any{
						"id": toolCall.ID,
						"function": map[string]any{
							"name":      toolCall.Name,
							"arguments": toolCall.Arguments,
						},
					})
				}
				apiMessage["tool_calls"] = calls
			}
			result = append(result, apiMessage)
		case ToolResult:
			// ponytail: is_error is internal, not sent to the API.
			// tool_call_id sent for OpenAI/Anthropic compatibility;
			// Ollama ignores it.
			result = append(result, map[string]any{
				"role":         "tool",
				"content":      m.Content,
				"tool_call_id": m.ToolCallID,
			})
		}
	}
	return result
}

// Stream sends a chat request to the Ollama API and emits stream
// events on the returned channel. The channel is closed when the
// stream ends. Errors are encoded as StreamEnd(FinishReason="error",
// Error=...), not returned as Go errors.
func (p *OllamaProvider) Stream(ctx context.Context, messages []Message, tools []ToolSchema) <-chan StreamEvent {
	events := make(chan StreamEvent)
	go p.stream(ctx, events, messages, tools)
	return events
}

func (p *OllamaProvider) stream(ctx context.Context, events chan<- StreamEvent, messages []Message, tools []ToolSchema) {
	defer close(events)

	payload := map[string]any{
		"model":    p.modelName,
		"messages": p.messagesToAPI(messages),
		"stream":   true,
	}
	if v := ollamaThinkValue(p.thinkingLevel); v != nil {
		payload["think"] = v
	}
	if len(tools) > 0 {
		apiTools := make([]map[string]any, 0, len(tools))
		for _, tool := range tools {
			apiTools = append(apiTools, map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        tool.Name,
					"description": tool.Description,
					"parameters":  tool.Parameters,
				},
			})
		}
		payload["tools"] = apiTools
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		emitError(events, err)
		return
	}

	req, err := http.NewRequestWithContext(ctx, "POST",
		p.baseURL+"/api/chat", bytes.NewReader(payloadBytes))
	if err != nil {
		emitError(events, err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := retryHTTP(ctx, req)
	if err != nil {
		emitError(events, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		emitError(events, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body)))
		return
	}

	phase := "" // "thinking" | "response" | "tool_call"

	reader := bufio.NewReader(resp.Body)
	for {
		line, readErr := reader.ReadString('\n')
		line = strings.TrimRight(line, "\n\r")

		if line != "" {
			var chunk map[string]any
			if jsonErr := json.Unmarshal([]byte(line), &chunk); jsonErr != nil {
				emitError(events, fmt.Errorf("parse error: %w", jsonErr))
				return
			}

			done, _ := chunk["done"].(bool)
			if done {
				var promptEval, eval *int
				if v, ok := chunk["prompt_eval_count"].(float64); ok {
					n := int(v)
					promptEval = &n
				}
				if v, ok := chunk["eval_count"].(float64); ok {
					n := int(v)
					eval = &n
				}
				finishReason := "stop"
				if phase == "tool_call" {
					finishReason = "tool_call"
				}
				events <- StreamEnd{
					Type:            "stream_end",
					FinishReason:    finishReason,
					PromptEvalCount: promptEval,
					EvalCount:       eval,
				}
				return
			}

			message, _ := chunk["message"].(map[string]any)
			thinking, _ := message["thinking"].(string)
			content, _ := message["content"].(string)

			if thinking != "" {
				phase = "thinking"
				events <- ThinkingChunk{Type: "thinking_chunk", Content: thinking}
			}

			if content != "" {
				phase = "response"
				events <- ResponseChunk{Type: "response_chunk", Content: content}
			}

			// Tool calls
			toolCallsRaw, _ := message["tool_calls"].([]any)
			if len(toolCallsRaw) > 0 {
				calls := make([]ToolCall, 0, len(toolCallsRaw))
				for _, raw := range toolCallsRaw {
					tc, _ := raw.(map[string]any)
					fn, _ := tc["function"].(map[string]any)
					name, _ := fn["name"].(string)
					var args map[string]any
					switch a := fn["arguments"].(type) {
					case map[string]any:
						args = a
					case string:
						_ = json.Unmarshal([]byte(a), &args)
					}
					if args == nil {
						args = map[string]any{}
					}
					callID, _ := tc["id"].(string)
					calls = append(calls, ToolCall{
						ID:        callID,
						Name:      name,
						Arguments: args,
					})
				}
				for _, call := range calls {
					events <- ToolCallEvent{Type: "tool_call", ToolCall: call}
				}
				phase = "tool_call"
			}
		}

		if readErr != nil {
			if readErr == io.EOF {
				return
			}
			emitError(events, readErr)
			return
		}
	}
}

// emitError emits a StreamEnd with an error.
func emitError(events chan<- StreamEvent, err error) {
	events <- StreamEnd{
		Type:         "stream_end",
		FinishReason: "error",
		Error:        err.Error(),
	}
}
