// core-provider is an omega extension that implements the provider seam.
// It contains the Ollama, OpenAI, and Anthropic provider implementations,
// moved from the ai/ package. The extension receives provider/stream
// requests with messages and tools as JSON, makes HTTP calls to the LLM
// API, streams chunks back as JSON-RPC notifications, and sends a final
// response with finish_reason and token counts.
//
// Seam: provider
// Methods: provider/stream, provider/list_models, provider/model_name,
// provider/set_thinking, provider/set_model
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/EndoTheDev/omega/ai"
)

// --- omega extension protocol types ---

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError        `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// --- provider state ---

var (
	providerType  string
	modelName     string
	baseURL       string
	apiKey        string
	thinkingLevel string
)

// --- JSON-RPC dispatch ---

func main() {
	// Read config from env vars.
	providerType = os.Getenv("OMEGA_PROVIDER_TYPE")
	if providerType == "" {
		providerType = "ollama"
	}
	modelName = os.Getenv("OMEGA_PROVIDER_MODEL")
	baseURL = os.Getenv("OMEGA_PROVIDER_HOST")

	stdin := bufio.NewReader(os.Stdin)

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
			handleInitialize(req)

		case "provider/stream":
			handleStream(req)

		case "provider/model_name":
			if req.ID != nil {
				sendResponse(*req.ID, map[string]any{"model": modelName})
			}

		case "provider/list_models":
			handleListModels(req)

		case "provider/set_thinking":
			var params struct {
				Level string `json:"level"`
			}
			if req.Params != nil {
				json.Unmarshal(req.Params, &params)
			}
			thinkingLevel = params.Level
			if req.ID != nil {
				sendResponse(*req.ID, map[string]any{"ok": true})
			}

		case "provider/set_model":
			var params struct {
				Model string `json:"model"`
			}
			if req.Params != nil {
				json.Unmarshal(req.Params, &params)
			}
			modelName = params.Model
			if req.ID != nil {
				sendResponse(*req.ID, map[string]any{"ok": true})
			}

		case "shutdown":
			return

		default:
			if req.ID != nil {
				sendResponse(*req.ID, map[string]any{})
			}
		}
	}
}

func handleInitialize(req rpcRequest) {
	// Read API key from the appropriate env var.
	switch providerType {
	case "ollama":
		apiKey = os.Getenv("OLLAMA_API_KEY")
		if baseURL == "" {
			baseURL = os.Getenv("OLLAMA_HOST")
		}
		if baseURL == "" {
			baseURL = "http://localhost:11434"
		}
	case "openai":
		apiKey = os.Getenv("OPENAI_API_KEY")
		if baseURL == "" {
			baseURL = "https://api.openai.com/v1"
		}
	case "anthropic":
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
		if baseURL == "" {
			baseURL = "https://api.anthropic.com/v1"
		}
	}
	baseURL = strings.TrimRight(baseURL, "/")

	if req.ID != nil {
		sendResponse(*req.ID, map[string]any{
			"name":         "core-provider",
			"tools":        []any{},
			"subscriptions": []string{},
			"seams":         []string{"provider"},
		})
	}
}

func handleListModels(req rpcRequest) {
	models, err := listModels()
	if err != nil {
		if req.ID != nil {
			writeJSON(rpcResponse{
				JSONRPC: "2.0", ID: *req.ID,
				Error: &rpcError{Code: -1, Message: err.Error()},
			})
		}
		return
	}
	if req.ID != nil {
		sendResponse(*req.ID, map[string]any{"models": models})
	}
}

// writeJSON marshals v and writes it as a single line to stdout,
// followed by a newline. Uses os.Stdout.Write directly (no buffering)
// so notifications are flushed immediately to the pipe.
func writeJSON(v any) {
	data, _ := json.Marshal(v)
	data = append(data, '\n')
	os.Stdout.Write(data)
}

// sendResponse sends a JSON-RPC response with the given ID and result.
func sendResponse(id int64, result any) {
	data, _ := json.Marshal(result)
	writeJSON(rpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  data,
	})
}

// sendNotification sends a JSON-RPC notification (no ID) with the
// given method and params.
func sendNotification(method string, params map[string]any) {
	writeJSON(map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	})
}

// --- stream params ---

type streamParams struct {
	Messages []map[string]any `json:"messages"`
	Tools    []map[string]any `json:"tools"`
}

// --- provider dispatch ---

func handleStream(req rpcRequest) {
	var params streamParams
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}

	var finishReason string
	var promptEval, eval *int
	var streamErr error

	switch providerType {
	case "ollama":
		finishReason, promptEval, eval, streamErr = streamOllama(params)
	case "openai":
		finishReason, streamErr = streamOpenAI(params)
	case "anthropic":
		finishReason, streamErr = streamAnthropic(params)
	default:
		streamErr = fmt.Errorf("unknown provider type: %s", providerType)
	}

	result := map[string]any{
		"finish_reason": finishReason,
	}
	if streamErr != nil {
		result["error"] = streamErr.Error()
		result["finish_reason"] = "error"
	}
	if promptEval != nil {
		result["prompt_eval_count"] = *promptEval
	}
	if eval != nil {
		result["eval_count"] = *eval
	}

	if req.ID != nil {
		sendResponse(*req.ID, result)
	}
}

// --- shared helpers ---

// listModels fetches available models from the provider API.
func listModels() ([]string, error) {
	var req *http.Request
	var err error

	switch providerType {
	case "ollama":
		req, err = http.NewRequest("GET", baseURL+"/api/tags", nil)
		if err != nil {
			return nil, err
		}
		if apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}
	case "openai":
		req, err = http.NewRequest("GET", baseURL+"/models", nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
	case "anthropic":
		req, err = http.NewRequest("GET", baseURL+"/models", nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("x-api-key", apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	default:
		return nil, fmt.Errorf("unknown provider type: %s", providerType)
	}

	resp, err := ai.HTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	// All three providers return a list of model objects with a name/id field.
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

// --- Ollama ---

func streamOllama(params streamParams) (string, *int, *int, error) {
	payload := map[string]any{
		"model":    modelName,
		"messages": params.Messages,
		"stream":   true,
	}
	if v := ollamaThinkValue(thinkingLevel); v != nil {
		payload["think"] = v
	}
	if len(params.Tools) > 0 {
		apiTools := make([]map[string]any, 0, len(params.Tools))
		for _, tool := range params.Tools {
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
		return "", nil, nil, err
	}

	req, err := http.NewRequest("POST", baseURL+"/api/chat", bytes.NewReader(payloadBytes))
	if err != nil {
		return "", nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := ai.RetryHTTP(req)
	if err != nil {
		return "", nil, nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", nil, nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	phase := ""
	reader := bufio.NewReader(resp.Body)
	for {
		line, readErr := reader.ReadString('\n')
		line = strings.TrimRight(line, "\n\r")

		if line != "" {
			var chunk map[string]any
			if jsonErr := json.Unmarshal([]byte(line), &chunk); jsonErr != nil {
				return "", nil, nil, fmt.Errorf("parse error: %w", jsonErr)
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
				return finishReason, pe, ev, nil
			}

			message, _ := chunk["message"].(map[string]any)
			thinking, _ := message["thinking"].(string)
			content, _ := message["content"].(string)

			if thinking != "" {
				phase = "thinking"
				sendNotification("stream_event", map[string]any{
					"type":    "thinking_chunk",
					"content": thinking,
				})
			}

			if content != "" {
				phase = "response"
				sendNotification("stream_event", map[string]any{
					"type":    "response_chunk",
					"content": content,
				})
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
					sendNotification("stream_event", map[string]any{
						"type": "tool_call",
						"tool_call": map[string]any{
							"id":        callID,
							"name":      name,
							"arguments": args,
						},
					})
				}
				phase = "tool_call"
			}
		}

		if readErr != nil {
			if readErr == io.EOF {
				return "stop", nil, nil, nil
			}
			return "", nil, nil, readErr
		}
	}
}

// --- OpenAI ---

// flushToolCalls sends accumulated tool-call notifications in index
// order. The accessor functions extract the id, name, and raw JSON
// string from each pending entry (field names differ between providers).
func flushToolCalls[T any](pending map[int]*T, idFn func(*T) string, nameFn func(*T) string, jsonFn func(*T) string) {
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
		sendNotification("stream_event", map[string]any{
			"type": "tool_call",
			"tool_call": map[string]any{
				"id":        idFn(entry),
				"name":      nameFn(entry),
				"arguments": args,
			},
		})
	}
}

// pendingCall accumulates OpenAI tool-call fragments.
type pendingCall struct {
	id        string
	name      string
	arguments string
}

func streamOpenAI(params streamParams) (string, error) {
	if apiKey == "" {
		return "", fmt.Errorf("openai: OPENAI_API_KEY not set")
	}

	payload := map[string]any{
		"model":    modelName,
		"messages": params.Messages,
		"stream":   true,
	}
	if effort := openaiReasoningEffort(thinkingLevel); effort != "" {
		payload["reasoning_effort"] = effort
	}
	if len(params.Tools) > 0 {
		apiTools := make([]map[string]any, 0, len(params.Tools))
		for _, tool := range params.Tools {
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
		return "", err
	}

	req, err := http.NewRequest("POST", baseURL+"/chat/completions", bytes.NewReader(payloadBytes))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := ai.RetryHTTP(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return "", fmt.Errorf("openai: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
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
			return "", readErr
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
			return "", fmt.Errorf("openai: parse error: %w", err)
		}
		if chunk.Error != nil {
			return "", fmt.Errorf("openai: %s", chunk.Error.Message)
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]

		if choice.Delta.Content != nil && *choice.Delta.Content != "" {
			sendNotification("stream_event", map[string]any{
				"type":    "response_chunk",
				"content": *choice.Delta.Content,
			})
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
	flushToolCalls(pending,
		func(pc *pendingCall) string { return pc.id },
		func(pc *pendingCall) string { return pc.name },
		func(pc *pendingCall) string { return pc.arguments },
	)

	return finishReason, nil
}

// --- Anthropic ---

func streamAnthropic(params streamParams) (string, error) {
	if apiKey == "" {
		return "", fmt.Errorf("anthropic: ANTHROPIC_API_KEY not set")
	}

	// Anthropic requires system prompt as top-level field, and tool
	// results folded into user messages.
	system, apiMessages := anthropicConvertMessages(params.Messages)

	payload := map[string]any{
		"model":      modelName,
		"messages":   apiMessages,
		"max_tokens": 4096,
		"stream":     true,
	}
	if budget := anthropicBudgetTokens(thinkingLevel); budget > 0 {
		payload["thinking"] = map[string]any{
			"type":          "enabled",
			"budget_tokens": budget,
		}
	}
	if system != "" {
		payload["system"] = system
	}
	if len(params.Tools) > 0 {
		apiTools := make([]map[string]any, 0, len(params.Tools))
		for _, tool := range params.Tools {
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
		return "", err
	}

	req, err := http.NewRequest("POST", baseURL+"/messages", bytes.NewReader(payloadBytes))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := ai.RetryHTTP(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return "", fmt.Errorf("anthropic: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	reader := bufio.NewReader(resp.Body)
	type pendingTool struct {
		id        string
		name      string
		inputJSON string
	}
	pending := make(map[int]*pendingTool)
	finishReason := "stop"

	for {
		payloadLine, ok, err := ai.SSEData(reader)
		if err != nil {
			if err == io.EOF {
				break
			}
			return "", err
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
			return "", fmt.Errorf("anthropic: parse error: %w", err)
		}
		if chunk.Error != nil {
			return "", fmt.Errorf("anthropic: %s", chunk.Error.Message)
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
					sendNotification("stream_event", map[string]any{
						"type":    "response_chunk",
						"content": chunk.Delta.Text,
					})
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
	flushToolCalls(pending,
		func(pt *pendingTool) string { return pt.id },
		func(pt *pendingTool) string { return pt.name },
		func(pt *pendingTool) string { return pt.inputJSON },
	)

	return finishReason, nil
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

