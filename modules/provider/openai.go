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

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/engine/ai"
)

func (p *Provider) streamOpenAI(ctx context.Context, events chan<- core.ProviderStreamEvent, messages, tools []map[string]any) core.StreamEnd {
	if p.apiKey == "" {
		return core.StreamEnd{Type: "stream_end", FinishReason: "error", Error: "openai: OPENAI_API_KEY not set"}
	}

	payload := map[string]any{
		"model":    p.modelName,
		"messages": messages,
		"stream":   true,
		// ask the provider to append a final chunk with token usage
		"stream_options": map[string]any{"include_usage": true},
	}
	if effort := openaiReasoningEffort(p.thinkingLevel); effort != "" {
		payload["reasoning_effort"] = effort
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
		return core.StreamEnd{Type: "stream_end", FinishReason: "error", Error: err.Error()}
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/chat/completions", bytes.NewReader(payloadBytes))
	if err != nil {
		return core.StreamEnd{Type: "stream_end", FinishReason: "error", Error: err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := ai.RetryHTTP(req)
	if err != nil {
		return core.StreamEnd{Type: "stream_end", FinishReason: "error", Error: err.Error()}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return core.StreamEnd{Type: "stream_end", FinishReason: "error", Error: fmt.Sprintf("openai: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))}
	}

	reader := bufio.NewReader(resp.Body)
	pending := make(map[int]*pendingCall)
	// Some openai-compatible hosts omit tool-call ids entirely; the
	// ollama native path synthesizes stable call_N ids so ToolResult
	// correlation works. Mirror that here.
	toolCallSeq := 0
	finishReason := "stop"
	promptTokens, completionTokens, cachedTokens := 0, 0, 0
	sawUsage := false

	for {
		payloadLine, ok, readErr := ai.SSEData(reader)
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			if ctx.Err() != nil {
				return core.StreamEnd{Type: "stream_end", FinishReason: "error", Error: ctx.Err().Error()}
			}
			return core.StreamEnd{Type: "stream_end", FinishReason: "error", Error: readErr.Error()}
		}
		if !ok {
			continue
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   *string `json:"content"`
					Reasoning *string `json:"reasoning"`
					// ReasoningContent is the deepseek-style field
					// name some openai-compatible hosts use
					// instead of `reasoning`.
					ReasoningContent *string `json:"reasoning_content"`
					ToolCalls        []struct {
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
			Usage *struct {
				PromptTokens     *int `json:"prompt_tokens"`
				CompletionTokens *int `json:"completion_tokens"`
				PromptDetails    *struct {
					CachedTokens *int `json:"cached_tokens"`
				} `json:"prompt_tokens_details"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(payloadLine), &chunk); err != nil {
			return core.StreamEnd{Type: "stream_end", FinishReason: "error", Error: fmt.Sprintf("openai: parse error: %v", err)}
		}
		if chunk.Error != nil {
			return core.StreamEnd{Type: "stream_end", FinishReason: "error", Error: fmt.Sprintf("openai: %s", chunk.Error.Message)}
		}
		if chunk.Usage != nil {
			sawUsage = true
			if chunk.Usage.PromptTokens != nil {
				promptTokens = *chunk.Usage.PromptTokens
			}
			if chunk.Usage.CompletionTokens != nil {
				completionTokens = *chunk.Usage.CompletionTokens
			}
			if chunk.Usage.PromptDetails != nil && chunk.Usage.PromptDetails.CachedTokens != nil {
				cachedTokens = *chunk.Usage.PromptDetails.CachedTokens
			}
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]

		reasoning := ""
		if choice.Delta.Reasoning != nil {
			reasoning = *choice.Delta.Reasoning
		} else if choice.Delta.ReasoningContent != nil {
			reasoning = *choice.Delta.ReasoningContent
		}
		if reasoning != "" {
			sendEvent(ctx, events, core.ThinkingChunk{Type: "thinking_chunk", Content: reasoning})
		}
		if choice.Delta.Content != nil && *choice.Delta.Content != "" {
			sendEvent(ctx, events, core.ResponseChunk{Type: "response_chunk", Content: *choice.Delta.Content})
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
				// Only synthesize when the wire never sends an id
				// for this index; later fragments may still carry it.
				toolCallSeq++
				pc.id = fmt.Sprintf("call_%d", toolCallSeq)
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
	flushToolCalls(ctx, events, pending,
		func(pc *pendingCall) string { return pc.id },
		func(pc *pendingCall) string { return pc.name },
		func(pc *pendingCall) string { return pc.arguments },
	)

	pt, ct := promptTokens, completionTokens
	ca := cachedTokens
	// Parity with the ollama native path: when the host never sent a
	// usage chunk, report absent counters (nil) instead of fake zeros.
	if !sawUsage {
		return core.StreamEnd{Type: "stream_end", FinishReason: finishReason}
	}
	return core.StreamEnd{
		Type:            "stream_end",
		FinishReason:    finishReason,
		PromptEvalCount: &pt,
		EvalCount:       &ct,
		CachedCount:     &ca,
	}
}

// --- Anthropic ---

// pendingTool accumulates Anthropic tool-call fragments.
type pendingTool struct {
	id        string
	name      string
	inputJSON string
}

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
