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
	"time"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/engine/ai"
)

func (p *Provider) streamOllama(ctx context.Context, events chan<- core.ProviderStreamEvent, messages, tools []map[string]any) core.StreamEnd {
	end, retriable := p.streamOllamaAttempt(ctx, events, messages, tools)
	// done_reason "load" rides a 200 OK stream and produces nothing, so
	// RetryHTTP never sees it: the failure happens after the HTTP retry
	// layer has already succeeded. Retry it here with the same budget
	// and backoff RetryHTTP applies to 429/5xx.
	for attempt := 0; retriable; attempt++ {
		if attempt >= ai.MaxRetries() {
			return core.StreamEnd{
				Type:         "stream_end",
				FinishReason: "error",
				Error: fmt.Sprintf("ollama: upstream canceled generation for a cold model load %d time(s) (done_reason \"load\"); no content was produced - run ends as an error",
					attempt+1),
			}
		}
		select {
		case <-ctx.Done():
			return core.StreamEnd{Type: "stream_end", FinishReason: "error", Error: ctx.Err().Error()}
		case <-time.After(ai.Backoff(attempt)):
		}
		end, retriable = p.streamOllamaAttempt(ctx, events, messages, tools)
	}
	return end
}

// streamOllamaAttempt runs one request/stream cycle. The second return
// is true when the stream ended with done_reason "load" and produced no
// events (no thinking, content, or tool calls were streamed), so a
// retry cannot duplicate anything the consumer already received.
func (p *Provider) streamOllamaAttempt(ctx context.Context, events chan<- core.ProviderStreamEvent, messages, tools []map[string]any) (core.StreamEnd, bool) {
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
		return core.StreamEnd{Type: "stream_end", FinishReason: "error", Error: err.Error()}, false
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/api/chat", bytes.NewReader(payloadBytes))
	if err != nil {
		return core.StreamEnd{Type: "stream_end", FinishReason: "error", Error: err.Error()}, false
	}
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := ai.RetryHTTP(req)
	if err != nil {
		return core.StreamEnd{Type: "stream_end", FinishReason: "error", Error: err.Error()}, false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return core.StreamEnd{Type: "stream_end", FinishReason: "error", Error: fmt.Sprintf("ollama: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))}, false
	}

	phase := ""
	// Ollama's tool_calls carry no id on some hosts/versions; the
	// openai path always yields one. Synthesize stable ids (call_N)
	// when the wire omits them so ToolResult correlation works.
	toolCallSeq := 0
	reader := bufio.NewReader(resp.Body)
	for {
		line, readErr := reader.ReadString('\n')
		line = strings.TrimRight(line, "\n\r")

		if line != "" {
			var chunk map[string]any
			if jsonErr := json.Unmarshal([]byte(line), &chunk); jsonErr != nil {
				return core.StreamEnd{Type: "stream_end", FinishReason: "error", Error: fmt.Sprintf("parse error: %v", jsonErr)}, false
			}

			// In-stream error payloads surface as an error StreamEnd
			// (parity with the openai path). The error field is a
			// bare string on ollama and an object on some proxies.
			switch eval := chunk["error"].(type) {
			case string:
				if eval != "" {
					return core.StreamEnd{Type: "stream_end", FinishReason: "error", Error: eval}, false
				}
			case map[string]any:
				if msg, ok := eval["message"].(string); ok && msg != "" {
					return core.StreamEnd{Type: "stream_end", FinishReason: "error", Error: msg}, false
				}
			}

			done, _ := chunk["done"].(bool)
			if done {
				var pe, ev, ca *int
				if v, ok := chunk["prompt_eval_count"].(float64); ok {
					n := int(v)
					pe = &n
				}
				if v, ok := chunk["eval_count"].(float64); ok {
					n := int(v)
					ev = &n
				}
				if v, ok := chunk["prompt_eval_cached_count"].(float64); ok {
					n := int(v)
					ca = &n
				}
				finishReason := "stop"
				if phase == "tool_call" {
					finishReason = "tool_call"
				} else if dr, ok := chunk["done_reason"].(string); ok && dr != "" {
					finishReason = dr
				}
				if finishReason == "load" && phase == "" {
					// Cloud cold-start verdict: the generation was
					// canceled while the model loaded and nothing was
					// streamed. Retryable only here - a load verdict
					// after content would duplicate that content.
					return core.StreamEnd{Type: "stream_end", FinishReason: finishReason}, true
				}
				return core.StreamEnd{Type: "stream_end", FinishReason: finishReason, PromptEvalCount: pe, EvalCount: ev, CachedCount: ca}, false
			}

			message, _ := chunk["message"].(map[string]any)
			thinking, _ := message["thinking"].(string)
			content, _ := message["content"].(string)

			if thinking != "" {
				phase = "thinking"
				sendEvent(ctx, events, core.ThinkingChunk{Type: "thinking_chunk", Content: thinking})
			}

			if content != "" {
				phase = "response"
				sendEvent(ctx, events, core.ResponseChunk{Type: "response_chunk", Content: content})
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
					if callID == "" {
						toolCallSeq++
						callID = fmt.Sprintf("call_%d", toolCallSeq)
					}
					sendEvent(ctx, events, core.ToolCallEvent{
						Type:     "tool_call",
						ToolCall: core.ToolCall{ID: callID, Name: name, Arguments: args},
					})
				}
				phase = "tool_call"
			}
		}

		if readErr != nil {
			if readErr == io.EOF {
				return core.StreamEnd{Type: "stream_end", FinishReason: "stop"}, false
			}
			if ctx.Err() != nil {
				return core.StreamEnd{Type: "stream_end", FinishReason: "error", Error: ctx.Err().Error()}, false
			}
			return core.StreamEnd{Type: "stream_end", FinishReason: "error", Error: readErr.Error()}, false
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
