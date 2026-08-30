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

func (p *Provider) streamOllama(ctx context.Context, events chan<- ai.StreamEvent, messages, tools []map[string]any) ai.StreamEnd {
	payload := map[string]any{
		"model":    p.modelName,
		"messages": messages,
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
					"name":        tool["name"],
					"description": tool["description"],
					"parameters":  tool["parameters"],
				},
			})
		}
		payload["tools"] = apiTools
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return ai.StreamEnd{Type: "stream_end", FinishReason: "error", Error: err.Error()}
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/api/chat", bytes.NewReader(payloadBytes))
	if err != nil {
		return ai.StreamEnd{Type: "stream_end", FinishReason: "error", Error: err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := ai.RetryHTTP(req)
	if err != nil {
		return ai.StreamEnd{Type: "stream_end", FinishReason: "error", Error: err.Error()}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return ai.StreamEnd{Type: "stream_end", FinishReason: "error", Error: fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(body))}
	}

	phase := ""
	reader := bufio.NewReader(resp.Body)
	for {
		line, readErr := reader.ReadString('\n')
		line = strings.TrimRight(line, "\n\r")

		if line != "" {
			var chunk map[string]any
			if jsonErr := json.Unmarshal([]byte(line), &chunk); jsonErr != nil {
				return ai.StreamEnd{Type: "stream_end", FinishReason: "error", Error: fmt.Sprintf("parse error: %v", jsonErr)}
			}

			done, _ := chunk["done"].(bool)
			if done {
				var pe, ev *int
				if v, ok := chunk["prompt_eval_count"].(float64); ok {
					n := int(v)
					pe = &n
				}
				if v, ok := chunk["eval_count"].(float64); ok {
					n := int(v)
					ev = &n
				}
				finishReason := "stop"
				if phase == "tool_call" {
					finishReason = "tool_call"
				}
				return ai.StreamEnd{Type: "stream_end", FinishReason: finishReason, PromptEvalCount: pe, EvalCount: ev}
			}

			message, _ := chunk["message"].(map[string]any)
			thinking, _ := message["thinking"].(string)
			content, _ := message["content"].(string)

			if thinking != "" {
				phase = "thinking"
				sendEvent(ctx, events, ai.ThinkingChunk{Type: "thinking_chunk", Content: thinking})
			}

			if content != "" {
				phase = "response"
				sendEvent(ctx, events, ai.ResponseChunk{Type: "response_chunk", Content: content})
			}

			toolCallsRaw, _ := message["tool_calls"].([]any)
			if len(toolCallsRaw) > 0 {
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
					sendEvent(ctx, events, ai.ToolCallEvent{
						Type:     "tool_call",
						ToolCall: ai.ToolCall{ID: callID, Name: name, Arguments: args},
					})
				}
				phase = "tool_call"
			}
		}

		if readErr != nil {
			if readErr == io.EOF {
				return ai.StreamEnd{Type: "stream_end", FinishReason: "stop"}
			}
			if ctx.Err() != nil {
				return ai.StreamEnd{Type: "stream_end", FinishReason: "error", Error: ctx.Err().Error()}
			}
			return ai.StreamEnd{Type: "stream_end", FinishReason: "error", Error: readErr.Error()}
		}
	}
}

// --- OpenAI ---

// pendingCall accumulates OpenAI tool-call fragments.
type pendingCall struct {
	id        string
	name      string
	arguments string
}


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

