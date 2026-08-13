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

// AnthropicProvider implements Provider for the Anthropic Messages API.
type AnthropicProvider struct {
	modelName     string
	baseURL       string
	apiKey        string
	thinkingLevel string
}

// NewAnthropicProvider creates an AnthropicProvider. If baseURL is empty
// it defaults to https://api.anthropic.com/v1. If apiKey is empty it
// reads ANTHROPIC_API_KEY.
func NewAnthropicProvider(modelName, baseURL, apiKey string) *AnthropicProvider {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com/v1"
	}
	if apiKey == "" {
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
	}
	return &AnthropicProvider{
		modelName: modelName,
		baseURL:   strings.TrimRight(baseURL, "/"),
		apiKey:    apiKey,
	}
}

// ModelName returns the model name used by this provider.
func (p *AnthropicProvider) ModelName() string {
	return p.modelName
}

// SetThinkingLevel sets the thinking level. Anthropic supports
// thinking blocks with a budget_tokens parameter.
func (p *AnthropicProvider) SetThinkingLevel(level string) {
	p.thinkingLevel = level
}

// ListModels fetches available models from the Anthropic API (/v1/models).
func (p *AnthropicProvider) ListModels() ([]string, error) {
	req, err := http.NewRequest("GET", p.baseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic: HTTP %d", resp.StatusCode)
	}
	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(result.Data))
	for _, m := range result.Data {
		names = append(names, m.ID)
	}
	sort.Strings(names)
	return names, nil
}

// messagesToAPI converts internal Message types to Anthropic format.
// The system prompt is lifted to the top-level "system" field, and
// consecutive tool results are folded into a single user message of
// tool_result content blocks (the shape Anthropic requires after an
// assistant turn with tool_use blocks).
func (p *AnthropicProvider) messagesToAPI(messages []Message) (system string, result []map[string]any) {
	for i := 0; i < len(messages); i++ {
		switch m := messages[i].(type) {
		case System:
			system += m.Content + "\n"
		case User:
			result = append(result, map[string]any{"role": "user", "content": m.Content})
		case Assistant:
			if len(m.ToolCalls) == 0 {
				result = append(result, map[string]any{"role": "assistant", "content": m.Content})
				continue
			}
			blocks := make([]map[string]any, 0, len(m.ToolCalls)+1)
			if m.Content != "" {
				blocks = append(blocks, map[string]any{"type": "text", "text": m.Content})
			}
			for _, toolCall := range m.ToolCalls {
				blocks = append(blocks, map[string]any{
					"type":  "tool_use",
					"id":    toolCall.ID,
					"name":  toolCall.Name,
					"input": toolCall.Arguments,
				})
			}
			result = append(result, map[string]any{"role": "assistant", "content": blocks})
		case ToolResult:
			// Fold this and any following consecutive ToolResults into
			// one user message of tool_result blocks.
			var blocks []map[string]any
			for i < len(messages) {
				tm, ok := messages[i].(ToolResult)
				if !ok {
					break
				}
				blocks = append(blocks, map[string]any{
					"type":        "tool_result",
					"tool_use_id": tm.ToolCallID,
					"content":     tm.Content,
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

// Stream sends a messages request to the Anthropic API and emits stream
// events. See Provider.Stream for the channel contract.
func (p *AnthropicProvider) Stream(ctx context.Context, messages []Message, tools []ToolSchema) <-chan StreamEvent {
	events := make(chan StreamEvent)
	go p.stream(ctx, events, messages, tools)
	return events
}

func (p *AnthropicProvider) stream(ctx context.Context, events chan<- StreamEvent, messages []Message, tools []ToolSchema) {
	defer close(events)

	if p.apiKey == "" {
		emitError(events, fmt.Errorf("anthropic: ANTHROPIC_API_KEY not set"))
		return
	}

	system, apiMessages := p.messagesToAPI(messages)
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
				"name":         tool.Name,
				"description":  tool.Description,
				"input_schema": tool.Parameters,
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
		p.baseURL+"/messages", bytes.NewReader(payloadBytes))
	if err != nil {
		emitError(events, err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := retryHTTP(ctx, req)
	if err != nil {
		emitError(events, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		emitError(events, fmt.Errorf("anthropic: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body))))
		return
	}

	reader := bufio.NewReader(resp.Body)
	// Tool_use blocks arrive as content_block_start + input_json_delta
	// fragments, keyed by block index.
	type pendingTool struct {
		id        string
		name      string
		inputJSON string
	}
	pending := make(map[int]*pendingTool)
	finishReason := "stop"

	for {
		// Anthropic sends an `event:` line before each `data:` line.
		// Read lines until we reach the data payload for this event.
		payloadLine := ""
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					goto done
				}
				emitError(events, err)
				return
			}
			line = strings.TrimRight(line, "\r\n")
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			payloadLine = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if payloadLine == "" || payloadLine == "[DONE]" {
				continue
			}
			break
		}

		var chunk struct {
			Type  string `json:"type"`
			Index *int   `json:"index"`
			Delta *struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				InputJSON   string `json:"input_json_delta"`
				StopReason  string `json:"stop_reason"`
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
			emitError(events, fmt.Errorf("anthropic: parse error: %w", err))
			return
		}
		if chunk.Error != nil {
			emitError(events, fmt.Errorf("anthropic: %s", chunk.Error.Message))
			return
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
					events <- ResponseChunk{Type: "response_chunk", Content: chunk.Delta.Text}
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

done:
	// Flush accumulated tool calls in block-index order.
	if len(pending) > 0 {
		indices := make([]int, 0, len(pending))
		for i := range pending {
			indices = append(indices, i)
		}
		sort.Ints(indices)
		for _, i := range indices {
			pc := pending[i]
			var args map[string]any
			if err := json.Unmarshal([]byte(pc.inputJSON), &args); err != nil {
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
