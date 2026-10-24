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
	"github.com/EndoTheDev/omega/core"
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
func (p *Provider) Stream(ctx context.Context, messages []core.Message, tools []ai.ToolSchema) <-chan core.ProviderStreamEvent {
	events := make(chan core.ProviderStreamEvent, 64)
	go func() {
		defer close(events)
		// Only the OpenAI wire encodes tool-call arguments as a JSON
		// string. Ollama's native path parses messages itself and
		// requires the argument object (a string fails with "Value
		// looks like object..."); Anthropic's input blocks want one
		// too.
		msgJSON := messagesToJSON(messages, p.typ == "openai")
		if p.typ == "ollama" {
			imagesToOllamaWire(msgJSON)
		}
		toolJSON := toolSchemasToJSON(tools)
		var end core.StreamEnd
		switch p.typ {
		case "ollama":
			end = p.streamOllama(ctx, events, msgJSON, toolJSON)
		case "openai":
			end = p.streamOpenAI(ctx, events, msgJSON, toolJSON)
		case "anthropic":
			end = p.streamAnthropic(ctx, events, msgJSON, toolJSON)
		default:
			end = core.StreamEnd{Type: "stream_end", FinishReason: "error", Error: fmt.Sprintf("unknown provider type: %s", p.typ)}
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

// WithModel returns a copy of the provider serving the given model.
// The receiver is left unchanged, so per-run providers are derived
// without mutating the mounted base provider.
func (p *Provider) WithModel(model string) ai.Provider {
	cp := *p
	cp.modelName = model
	return &cp
}

// WithThinkingLevel returns a copy of the provider streaming at the
// given thinking level (the shared ai.ThinkingLevels ladder; "none"
// sends no thinking parameter). Copy discipline matches WithModel:
// the receiver is left unchanged, so per-run providers derived from
// the mounted base never mutate it.
func (p *Provider) WithThinkingLevel(level string) ai.Provider {
	cp := *p
	cp.thinkingLevel = level
	return &cp
}

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

// messagesToJSON serializes core.Message values to maps with a role
// field added based on the concrete message type. The timestamp
// field is dropped (not needed by the provider API). stringArgs
// selects the tool_calls argument encoding: the OpenAI wire requires
// a JSON string, the ollama native path and Anthropic expect the
// argument object.
func messagesToJSON(messages []core.Message, stringArgs bool) []map[string]any {
	msgJSON := make([]map[string]any, len(messages))
	for i, msg := range messages {
		data, _ := json.Marshal(msg)
		var m map[string]any
		json.Unmarshal(data, &m)
		switch msg.(type) {
		case core.System:
			m["role"] = "system"
		case core.User:
			m["role"] = "user"
		case core.Assistant:
			m["role"] = "assistant"
		case core.ToolResult:
			m["role"] = "tool"
		}
		// The OpenAI wire format requires each tool_calls entry to be
		// {"id","type":"function","function":{"name","arguments":"<json>"}}
		// while core.ToolCall marshals flat with map arguments. The
		// OpenAI wire needs arguments as a JSON string; ollama's
		// native path and Anthropic require the object itself.
		if calls, ok := m["tool_calls"].([]any); ok {
			wire := make([]map[string]any, 0, len(calls))
			for _, c := range calls {
				cm, ok := c.(map[string]any)
				if !ok {
					continue
				}
				args := cm["arguments"]
				if stringArgs {
					if argsMap, ok := args.(map[string]any); ok {
						if b, err := json.Marshal(argsMap); err == nil {
							args = string(b)
						}
					}
				}
				fn, _ := cm["function"].(map[string]any)
				if fn == nil {
					fn = map[string]any{}
				}
				fn["name"], _ = cm["name"].(string)
				if args != nil {
					fn["arguments"] = args
				}
				wire = append(wire, map[string]any{
					"id":       cm["id"],
					"type":     "function",
					"function": fn,
				})
			}
			m["tool_calls"] = wire
		}
		delete(m, "timestamp")
		// The tree pointer (branching, t_17365247) is store-level truth,
		// never provider wire - a parent_id in a chat payload 400s on
		// strict wire validators and is noise on lenient ones.
		delete(m, "parent_id")
		msgJSON[i] = m
	}
	return msgJSON
}

// imagesToOllamaWire rewrites user-message images from the generic
// object shape core.User marshals ({media_type, base64}) to the raw
// base64-string array ollama's /api/chat expects. In place over the
// message maps: an object list sent to ollama 400s with "cannot
// unmarshal object into Alias.images.0" - verified live 2026-10-20.
func imagesToOllamaWire(messages []map[string]any) {
	for _, m := range messages {
		if m["role"] != "user" {
			continue
		}
		imgs, ok := m["images"].([]any)
		if !ok || len(imgs) == 0 {
			continue
		}
		raw := make([]string, 0, len(imgs))
		for _, im := range imgs {
			ic, ok := im.(map[string]any)
			if !ok {
				continue
			}
			if b, ok := ic["base64"].(string); ok && b != "" {
				raw = append(raw, b)
			}
		}
		if len(raw) == len(imgs) {
			m["images"] = raw
		}
	}
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

func sendEvent(ctx context.Context, events chan<- core.ProviderStreamEvent, event core.ProviderStreamEvent) {
	select {
	case <-ctx.Done():
	case events <- event:
	}
}

// flushToolCalls sends accumulated tool-call events in index order.
// The accessor functions extract the id, name, and raw JSON string
// from each pending entry (field names differ between providers).
func flushToolCalls[T any](ctx context.Context, events chan<- core.ProviderStreamEvent, pending map[int]*T, idFn func(*T) string, nameFn func(*T) string, jsonFn func(*T) string) {
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
		sendEvent(ctx, events, core.ToolCallEvent{
			Type:     "tool_call",
			ToolCall: core.ToolCall{ID: idFn(entry), Name: nameFn(entry), Arguments: args},
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
	case "openai", "anthropic":
		// OpenAI-compatible hosts that are actually ollama lanes (keypool
		// -> ollama-cloud) also serve /api/show at the ROOT, not under /v1;
		// strip a trailing /v1 before probing. Non-ollama hosts 404 and we
		// fall back to the config default.
		root := strings.TrimSuffix(p.baseURL, "/v1")
		body, _ := json.Marshal(map[string]any{"model": p.modelName})
		req, err := http.NewRequest("POST", root+"/api/show", bytes.NewReader(body))
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
