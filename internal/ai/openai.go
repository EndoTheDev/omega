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

// OpenAIProvider implements Provider for the OpenAI Chat Completions API.
type OpenAIProvider struct {
	modelName string
	baseURL   string
	apiKey    string
}

// NewOpenAIProvider creates an OpenAIProvider. If baseURL is empty it
// defaults to https://api.openai.com/v1. If apiKey is empty it reads
// OPENAI_API_KEY.
func NewOpenAIProvider(modelName, baseURL, apiKey string) *OpenAIProvider {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	return &OpenAIProvider{
		modelName: modelName,
		baseURL:   strings.TrimRight(baseURL, "/"),
		apiKey:    apiKey,
	}
}

// ModelName returns the model name used by this provider.
func (p *OpenAIProvider) ModelName() string {
	return p.modelName
}

// messagesToAPI converts internal Message types to OpenAI format.
// Assistant tool_calls and tool results use OpenAI's role/id shape.
func (p *OpenAIProvider) messagesToAPI(messages []Message) []map[string]any {
	result := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		switch m := message.(type) {
		case System:
			result = append(result, map[string]any{"role": "system", "content": m.Content})
		case User:
			result = append(result, map[string]any{"role": "user", "content": m.Content})
		case Assistant:
			apiMessage := map[string]any{"role": "assistant", "content": m.Content}
			if len(m.ToolCalls) > 0 {
				calls := make([]map[string]any, 0, len(m.ToolCalls))
				for i, toolCall := range m.ToolCalls {
					calls = append(calls, map[string]any{
						"id": toolCall.ID,
						"function": map[string]any{
							"name":      toolCall.Name,
							"arguments": mustJSON(toolCall.Arguments),
						},
						"type": "function",
						"index": i,
					})
				}
				apiMessage["tool_calls"] = calls
			}
			// ponytail: OpenAI has no thinking field; drop m.Thinking.
			result = append(result, apiMessage)
		case ToolResult:
			result = append(result, map[string]any{
				"role":         "tool",
				"tool_call_id": m.ToolCallID,
				"content":      m.Content,
			})
		}
	}
	return result
}

// mustJSON marshals v to a JSON string, returning "{}" on error. OpenAI
// tool arguments are JSON strings; the model also echoes them as strings.
func mustJSON(v map[string]any) string {
	b, err := json.Marshal(v)
	if err != nil || len(b) == 0 {
		return "{}"
	}
	return string(b)
}

// Stream sends a chat request to the OpenAI API and emits stream events.
// See Provider.Stream for the channel contract.
func (p *OpenAIProvider) Stream(ctx context.Context, messages []Message, tools []ToolSchema) <-chan StreamEvent {
	events := make(chan StreamEvent)
	go p.stream(ctx, events, messages, tools)
	return events
}

func (p *OpenAIProvider) stream(ctx context.Context, events chan<- StreamEvent, messages []Message, tools []ToolSchema) {
	defer close(events)

	if p.apiKey == "" {
		emitError(events, fmt.Errorf("openai: OPENAI_API_KEY not set"))
		return
	}

	payload := map[string]any{
		"model":    p.modelName,
		"messages": p.messagesToAPI(messages),
		"stream":   true,
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
		p.baseURL+"/chat/completions", bytes.NewReader(payloadBytes))
	if err != nil {
		emitError(events, err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := retryHTTP(ctx, req)
	if err != nil {
		emitError(events, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		emitError(events, fmt.Errorf("openai: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body))))
		return
	}

	reader := bufio.NewReader(resp.Body)
	// Tool call arguments arrive split across chunks, keyed by index.
	type pendingCall struct {
		id         string
		name       string
		arguments  string
	}
	pending := make(map[int]*pendingCall)
	finishReason := "stop"

	for {
		payloadLine, ok, readErr := sseData(reader)
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			emitError(events, readErr)
			return
		}
		if !ok {
			continue
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content    *string `json:"content"`
					ToolCalls  []struct {
						Index    *int   `json:"index"`
						ID       string `json:"id"`
						Function struct {
							Name      *string `json:"name"`
							Arguments *string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(payloadLine), &chunk); err != nil {
			emitError(events, fmt.Errorf("openai: parse error: %w", err))
			return
		}
		if chunk.Error != nil {
			emitError(events, fmt.Errorf("openai: %s", chunk.Error.Message))
			return
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]

		if choice.Delta.Content != nil && *choice.Delta.Content != "" {
			events <- ResponseChunk{Type: "response_chunk", Content: *choice.Delta.Content}
		}

		for _, tc := range choice.Delta.ToolCalls {
			idx := 0
			if tc.Index != nil {
				idx = *tc.Index
			}
			pc, exists := pending[idx]
			if !exists {
				pc = &pendingCall{}
				pending[idx] = pc
			}
			if tc.ID != "" {
				pc.id = tc.ID
			}
			if tc.Function.Name != nil {
				pc.name += *tc.Function.Name
			}
			if tc.Function.Arguments != nil {
				pc.arguments += *tc.Function.Arguments
			}
		}

		if choice.FinishReason != nil && *choice.FinishReason != "" {
			finishReason = *choice.FinishReason
		}
	}

	// Flush accumulated tool calls in index order.
	if len(pending) > 0 {
		indices := make([]int, 0, len(pending))
		for i := range pending {
			indices = append(indices, i)
		}
		sort.Ints(indices)
		for _, i := range indices {
			pc := pending[i]
			var args map[string]any
			if err := json.Unmarshal([]byte(pc.arguments), &args); err != nil {
				args = map[string]any{}
			}
			events <- ToolCallEvent{Type: "tool_call", ToolCall: ToolCall{
				ID:        pc.id,
				Name:      pc.name,
				Arguments: args,
			}}
		}
	}

	events <- StreamEnd{
		Type:         "stream_end",
		FinishReason: finishReason,
	}
}
