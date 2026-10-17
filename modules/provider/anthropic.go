package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/EndoTheDev/omega/ai"
)

func (p *Provider) streamAnthropic(ctx context.Context, events chan<- ai.StreamEvent, messages, tools []map[string]any) ai.StreamEnd {
	if p.apiKey == "" {
		return ai.StreamEnd{Type: "stream_end", FinishReason: "error", Error: "anthropic: ANTHROPIC_API_KEY not set"}
	}

	// Anthropic requires system prompt as top-level field, and tool
	// results folded into user messages.
	system, apiMessages := anthropicConvertMessages(messages)

	payload := map[string]any{
		"model":      p.modelName,
		"messages":   apiMessages,
		"max_tokens": 4096,
		"stream":     true,
	}
	if budget := anthropicBudgetTokens(p.thinkingLevel); budget > 0 {
		payload["thinking"] = map[string]any{
			"type":          "enabled",
			"budget_tokens": budget,
		}
	}
	if system != "" {
		payload["system"] = system
	}
	if len(tools) > 0 {
		apiTools := make([]map[string]any, 0, len(tools))
		for _, tool := range tools {
			apiTools = append(apiTools, map[string]any{
				"name":         tool["name"],
				"description":  tool["description"],
				"input_schema": tool["parameters"],
			})
		}
		payload["tools"] = apiTools
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return ai.StreamEnd{Type: "stream_end", FinishReason: "error", Error: err.Error()}
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/messages", bytes.NewReader(payloadBytes))
	if err != nil {
		return ai.StreamEnd{Type: "stream_end", FinishReason: "error", Error: err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := ai.RetryHTTP(req)
	if err != nil {
		return ai.StreamEnd{Type: "stream_end", FinishReason: "error", Error: err.Error()}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return ai.StreamEnd{Type: "stream_end", FinishReason: "error", Error: fmt.Sprintf("anthropic: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))}
	}

	reader := bufio.NewReader(resp.Body)
	pending := make(map[int]*pendingTool)
	finishReason := "stop"

	for {
		payloadLine, ok, err := ai.SSEData(reader)
		if err != nil {
			if err == io.EOF {
				break
			}
			if ctx.Err() != nil {
				return ai.StreamEnd{Type: "stream_end", FinishReason: "error", Error: ctx.Err().Error()}
			}
			return ai.StreamEnd{Type: "stream_end", FinishReason: "error", Error: err.Error()}
		}
		if !ok {
			break
		}

		var chunk struct {
			Type  string `json:"type"`
			Index *int   `json:"index"`
			Delta *struct {
				Type         string `json:"type"`
				Text         string `json:"text"`
				InputJSON    string `json:"partial_json"`
				StopReason   string `json:"stop_reason"`
				StopSequence string `json:"stop_sequence"`
			} `json:"delta"`
			ContentBlock *struct {
				Type string `json:"type"`
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"content_block"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(payloadLine), &chunk); err != nil {
			return ai.StreamEnd{Type: "stream_end", FinishReason: "error", Error: fmt.Sprintf("anthropic: parse error: %v", err)}
		}
		if chunk.Error != nil {
			return ai.StreamEnd{Type: "stream_end", FinishReason: "error", Error: fmt.Sprintf("anthropic: %s", chunk.Error.Message)}
		}

		switch chunk.Type {
		case "content_block_start":
			if chunk.ContentBlock != nil && chunk.ContentBlock.Type == "tool_use" {
				idx := 0
				if chunk.Index != nil {
					idx = *chunk.Index
				}
				pending[idx] = &pendingTool{
					id:   chunk.ContentBlock.ID,
					name: chunk.ContentBlock.Name,
				}
			}
		case "content_block_delta":
			if chunk.Delta == nil {
				continue
			}
			switch chunk.Delta.Type {
			case "text_delta":
				if chunk.Delta.Text != "" {
					sendEvent(ctx, events, ai.ResponseChunk{Type: "response_chunk", Content: chunk.Delta.Text})
				}
			case "input_json_delta":
				idx := 0
				if chunk.Index != nil {
					idx = *chunk.Index
				}
				if pc, ok := pending[idx]; ok {
					pc.inputJSON += chunk.Delta.InputJSON
				}
			}
		case "message_delta":
			if chunk.Delta != nil && chunk.Delta.StopReason != "" {
				finishReason = chunk.Delta.StopReason
			}
		}
	}

	// Flush accumulated tool calls in block-index order.
	flushToolCalls(ctx, events, pending,
		func(pt *pendingTool) string { return pt.id },
		func(pt *pendingTool) string { return pt.name },
		func(pt *pendingTool) string { return pt.inputJSON },
	)

	return ai.StreamEnd{Type: "stream_end", FinishReason: finishReason}
}

// --- Anthropic message conversion ---

// anthropicConvertMessages converts generic message maps to Anthropic
// format: system prompt lifted to top-level, tool results folded into
// user messages.
func anthropicConvertMessages(messages []map[string]any) (system string, result []map[string]any) {
	for i := 0; i < len(messages); i++ {
		role, _ := messages[i]["role"].(string)
		switch role {
		case "system":
			content, _ := messages[i]["content"].(string)
			system += content + "\n"
		case "user":
			// Check for images (content as array of blocks).
			if content, ok := messages[i]["content"].([]any); ok {
				result = append(result, map[string]any{"role": "user", "content": content})
			} else {
				content, _ := messages[i]["content"].(string)
				result = append(result, map[string]any{"role": "user", "content": content})
			}
		case "assistant":
			toolCalls, _ := messages[i]["tool_calls"].([]any)
			if len(toolCalls) == 0 {
				content, _ := messages[i]["content"].(string)
				result = append(result, map[string]any{"role": "assistant", "content": content})
				continue
			}
			blocks := make([]map[string]any, 0, len(toolCalls)+1)
			if content, _ := messages[i]["content"].(string); content != "" {
				blocks = append(blocks, map[string]any{"type": "text", "text": content})
			}
			for _, raw := range toolCalls {
				tc, _ := raw.(map[string]any)
				fn, _ := tc["function"].(map[string]any)
				name, _ := fn["name"].(string)
				args, _ := fn["arguments"]
				callID, _ := tc["id"].(string)
				blocks = append(blocks, map[string]any{
					"type":  "tool_use",
					"id":    callID,
					"name":  name,
					"input": args,
				})
			}
			result = append(result, map[string]any{"role": "assistant", "content": blocks})
		case "tool":
			// Fold consecutive tool results into one user message.
			var blocks []map[string]any
			for i < len(messages) {
				r, ok := messages[i]["role"].(string)
				if !ok || r != "tool" {
					break
				}
				content, _ := messages[i]["content"].(string)
				toolCallID, _ := messages[i]["tool_call_id"].(string)
				blocks = append(blocks, map[string]any{
					"type":        "tool_result",
					"tool_use_id": toolCallID,
					"content":     content,
				})
				i++
			}
			i--
			result = append(result, map[string]any{"role": "user", "content": blocks})
		}
	}
	system = strings.TrimSuffix(system, "\n")
	return system, result
}

// --- shared helpers ---

// sendEvent sends an event on the channel, respecting context
// cancellation. Non-blocking: if the context is cancelled, the event
// is dropped.

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
