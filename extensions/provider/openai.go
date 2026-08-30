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

func (p *Provider) streamOpenAI(ctx context.Context, events chan<- ai.StreamEvent, messages, tools []map[string]any) ai.StreamEnd {
	if p.apiKey == "" {
		return ai.StreamEnd{Type: "stream_end", FinishReason: "error", Error: "openai: OPENAI_API_KEY not set"}
	}

	payload := map[string]any{
		"model":    p.modelName,
		"messages": messages,
		"stream":   true,
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
		return ai.StreamEnd{Type: "stream_end", FinishReason: "error", Error: err.Error()}
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/chat/completions", bytes.NewReader(payloadBytes))
	if err != nil {
		return ai.StreamEnd{Type: "stream_end", FinishReason: "error", Error: err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := ai.RetryHTTP(req)
	if err != nil {
		return ai.StreamEnd{Type: "stream_end", FinishReason: "error", Error: err.Error()}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return ai.StreamEnd{Type: "stream_end", FinishReason: "error", Error: fmt.Sprintf("openai: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))}
	}

	reader := bufio.NewReader(resp.Body)
	pending := make(map[int]*pendingCall)
	finishReason := "stop"

	for {
		payloadLine, ok, readErr := ai.SSEData(reader)
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			if ctx.Err() != nil {
				return ai.StreamEnd{Type: "stream_end", FinishReason: "error", Error: ctx.Err().Error()}
			}
			return ai.StreamEnd{Type: "stream_end", FinishReason: "error", Error: readErr.Error()}
		}
		if !ok {
			continue
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   *string `json:"content"`
					ToolCalls []struct {
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
			return ai.StreamEnd{Type: "stream_end", FinishReason: "error", Error: fmt.Sprintf("openai: parse error: %v", err)}
		}
		if chunk.Error != nil {
			return ai.StreamEnd{Type: "stream_end", FinishReason: "error", Error: fmt.Sprintf("openai: %s", chunk.Error.Message)}
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]

		if choice.Delta.Content != nil && *choice.Delta.Content != "" {
			sendEvent(ctx, events, ai.ResponseChunk{Type: "response_chunk", Content: *choice.Delta.Content})
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
	flushToolCalls(ctx, events, pending,
		func(pc *pendingCall) string { return pc.id },
		func(pc *pendingCall) string { return pc.name },
		func(pc *pendingCall) string { return pc.arguments },
	)

	return ai.StreamEnd{Type: "stream_end", FinishReason: finishReason}
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

