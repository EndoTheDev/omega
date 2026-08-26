// Package provider implements the provider seam for omega.
// It contains the Ollama, OpenAI, and Anthropic provider
// implementations. The Provider struct implements ai.Provider directly.
package provider

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

	"github.com/EndoTheDev/omega/ai"
)

// Provider implements ai.Provider for Ollama, OpenAI, and Anthropic.
type Provider struct {
	typ           string // "ollama", "openai", "anthropic"
	modelName     string
	baseURL       string
	apiKey        string
	thinkingLevel string
}

// NewProvider creates a Provider from the given configuration fields.
// typ defaults to "ollama" when empty. baseURL defaults to the
// provider's standard endpoint when empty. apiKey is read from the
// appropriate environment variable when empty.
func NewProvider(typ, modelName, baseURL, apiKey string) *Provider {
	p := &Provider{
		typ:       typ,
		modelName: modelName,
		baseURL:   baseURL,
		apiKey:    apiKey,
	}
	if p.typ == "" {
		p.typ = "ollama"
	}
	p.initDefaults()
	return p
}

// initDefaults resolves the API key from env vars and fills in the
// default base URL for each provider type.
func (p *Provider) initDefaults() {
	switch p.typ {
	case "ollama":
		if p.apiKey == "" {
			p.apiKey = os.Getenv("OLLAMA_API_KEY")
		}
		if p.baseURL == "" {
			p.baseURL = os.Getenv("OLLAMA_HOST")
		}
		if p.baseURL == "" {
			p.baseURL = "http://localhost:11434"
		}
	case "openai":
		if p.apiKey == "" {
			p.apiKey = os.Getenv("OPENAI_API_KEY")
		}
		if p.baseURL == "" {
			p.baseURL = "https://api.openai.com/v1"
		}
	case "anthropic":
		if p.apiKey == "" {
			p.apiKey = os.Getenv("ANTHROPIC_API_KEY")
		}
		if p.baseURL == "" {
			p.baseURL = "https://api.anthropic.com/v1"
		}
	}
	p.baseURL = strings.TrimRight(p.baseURL, "/")
}

// Stream sends a completion request to the provider and streams
// events on the returned channel. The channel is closed when the
// stream ends. Errors are encoded as StreamEnd (FinishReason="error").
func (p *Provider) Stream(ctx context.Context, messages []ai.Message, tools []ai.ToolSchema) <-chan ai.StreamEvent {
	events := make(chan ai.StreamEvent, 64)
	go func() {
		defer close(events)
		msgJSON := messagesToJSON(messages)
		toolJSON := toolSchemasToJSON(tools)
		var end ai.StreamEnd
		switch p.typ {
		case "ollama":
			end = p.streamOllama(ctx, events, msgJSON, toolJSON)
		case "openai":
			end = p.streamOpenAI(ctx, events, msgJSON, toolJSON)
		case "anthropic":
			end = p.streamAnthropic(ctx, events, msgJSON, toolJSON)
		default:
			end = ai.StreamEnd{Type: "stream_end", FinishReason: "error", Error: fmt.Sprintf("unknown provider type: %s", p.typ)}
		}
		select {
		case <-ctx.Done():
		case events <- end:
		}
	}()
	return events
}

// ModelName returns the current model name.
func (p *Provider) ModelName() string { return p.modelName }

// SetThinkingLevel sets the thinking level for subsequent streams.
func (p *Provider) SetThinkingLevel(level string) { p.thinkingLevel = level }

// SetModel updates the model name.
func (p *Provider) SetModel(model string) { p.modelName = model }

// ListModels fetches available models from the provider API.
func (p *Provider) ListModels() ([]string, error) {
	return p.listModels()
}

// ModelInfo queries the provider for the current model's context window.
// Ollama: POST /api/show. OpenAI/Anthropic: return 0 (not exposed).
func (p *Provider) ModelInfo() (ai.ModelInfo, error) {
	ctx, err := p.modelInfo()
	if err != nil {
		return ai.ModelInfo{}, err
	}
	return ai.ModelInfo{ContextWindow: ctx}, nil
}

// --- message/tool serialization ---

// messagesToJSON serializes ai.Message values to maps with a role
// field added based on the concrete message type. The timestamp
// field is dropped (not needed by the provider API).
func messagesToJSON(messages []ai.Message) []map[string]any {
	msgJSON := make([]map[string]any, len(messages))
	for i, msg := range messages {
		data, _ := json.Marshal(msg)
		var m map[string]any
		json.Unmarshal(data, &m)
		switch msg.(type) {
		case ai.System:
			m["role"] = "system"
		case ai.User:
			m["role"] = "user"
		case ai.Assistant:
			m["role"] = "assistant"
		case ai.ToolResult:
			m["role"] = "tool"
		}
		delete(m, "timestamp")
		msgJSON[i] = m
	}
	return msgJSON
}

// toolSchemasToJSON converts ToolSchema values to the generic map
// format expected by the streaming functions.
func toolSchemasToJSON(tools []ai.ToolSchema) []map[string]any {
	toolJSON := make([]map[string]any, len(tools))
	for i, t := range tools {
		toolJSON[i] = map[string]any{
			"name":        t.Name,
			"description": t.Description,
			"parameters":  t.Parameters,
		}
	}
	return toolJSON
}

// --- Ollama ---

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
				InputJSON    string `json:"input_json_delta"`
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
func sendEvent(ctx context.Context, events chan<- ai.StreamEvent, event ai.StreamEvent) {
	select {
	case <-ctx.Done():
	case events <- event:
	}
}

// flushToolCalls sends accumulated tool-call events in index order.
// The accessor functions extract the id, name, and raw JSON string
// from each pending entry (field names differ between providers).
func flushToolCalls[T any](ctx context.Context, events chan<- ai.StreamEvent, pending map[int]*T, idFn func(*T) string, nameFn func(*T) string, jsonFn func(*T) string) {
	if len(pending) == 0 {
		return
	}
	indices := make([]int, 0, len(pending))
	for i := range pending {
		indices = append(indices, i)
	}
	sort.Ints(indices)
	for _, i := range indices {
		entry := pending[i]
		var args map[string]any
		if err := json.Unmarshal([]byte(jsonFn(entry)), &args); err != nil {
			args = map[string]any{}
		}
		sendEvent(ctx, events, ai.ToolCallEvent{
			Type:     "tool_call",
			ToolCall: ai.ToolCall{ID: idFn(entry), Name: nameFn(entry), Arguments: args},
		})
	}
}

// listModels fetches available models from the provider API.
func (p *Provider) listModels() ([]string, error) {
	var req *http.Request
	var err error

	switch p.typ {
	case "ollama":
		req, err = http.NewRequest("GET", p.baseURL+"/api/tags", nil)
		if err != nil {
			return nil, err
		}
		if p.apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+p.apiKey)
		}
	case "openai":
		req, err = http.NewRequest("GET", p.baseURL+"/models", nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	case "anthropic":
		req, err = http.NewRequest("GET", p.baseURL+"/models", nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("x-api-key", p.apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	default:
		return nil, fmt.Errorf("unknown provider type: %s", p.typ)
	}

	resp, err := ai.HTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	// All three providers return a list of model objects with a
	// name/id field.
	var result struct {
		Models []struct {
			Name string `json:"name"`
			ID   string `json:"id"`
		} `json:"models"`
		Data []struct {
			Name string `json:"name"`
			ID   string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	var names []string
	for _, m := range result.Models {
		if m.Name != "" {
			names = append(names, m.Name)
		}
	}
	for _, m := range result.Data {
		if m.ID != "" {
			names = append(names, m.ID)
		}
	}
	sort.Strings(names)
	return names, nil
}

// modelInfo queries the provider for the current model's context
// window. Ollama: POST /api/show, extract *.context_length from
// model_info. OpenAI/Anthropic: return 0 (not exposed by their
// model listing APIs).
func (p *Provider) modelInfo() (int, error) {
	switch p.typ {
	case "ollama":
		body, _ := json.Marshal(map[string]any{"model": p.modelName})
		req, err := http.NewRequest("POST", p.baseURL+"/api/show", bytes.NewReader(body))
		if err != nil {
			return 0, err
		}
		req.Header.Set("Content-Type", "application/json")
		if p.apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+p.apiKey)
		}
		resp, err := ai.HTTPClient().Do(req)
		if err != nil {
			return 0, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return 0, fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		var result struct {
			ModelInfo map[string]any `json:"model_info"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return 0, err
		}
		// model_info keys are "<arch>.context_length" e.g.
		// "gemma4.context_length": 131072. Scan for any key ending
		// in ".context_length". JSON decodes numbers as float64.
		for k, v := range result.ModelInfo {
			if strings.HasSuffix(k, ".context_length") {
				if n, ok := v.(float64); ok {
					return int(n), nil
				}
			}
		}
		return 0, nil
	default:
		return 0, nil
	}
}

// --- thinking level mappers ---

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