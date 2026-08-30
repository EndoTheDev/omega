// Package provider implements the provider seam for omega.
// It contains the Ollama, OpenAI, and Anthropic provider
// implementations. The Provider struct implements ai.Provider directly.
package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

// buildListModelsRequest constructs the HTTP request for listing
// models based on the provider type. It returns the configured
// request or an error for unknown provider types.
func (p *Provider) buildListModelsRequest() (*http.Request, error) {
	switch p.typ {
	case "ollama":
		req, err := http.NewRequest("GET", p.baseURL+"/api/tags", nil)
		if err != nil {
			return nil, err
		}
		if p.apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+p.apiKey)
		}
		return req, nil
	case "openai":
		req, err := http.NewRequest("GET", p.baseURL+"/models", nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
		return req, nil
	case "anthropic":
		req, err := http.NewRequest("GET", p.baseURL+"/models", nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("x-api-key", p.apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
		return req, nil
	default:
		return nil, fmt.Errorf("unknown provider type: %s", p.typ)
	}
}

// listModels fetches available models from the provider API.
func (p *Provider) listModels() ([]string, error) {
	req, err := p.buildListModelsRequest()
	if err != nil {
		return nil, err
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

