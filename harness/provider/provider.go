// Package provider implements the provider seam for omega.
package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/EndoTheDev/omega/agent"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/llm"
)

// Provider implements agent.Provider for Ollama, OpenAI, and Anthropic.
type Provider struct {
	// mu guards the mutable state below. Readers snapshot the state (snapshot) and never
	// touch the lock again, so one write never blocks a run.
	mu            sync.RWMutex
	typ           string // "ollama", "openai", "anthropic", "mock"
	modelName     string
	baseURL       string
	apiKey        string
	thinkingLevel string
	instances map[string]*Provider
}

// DefaultInstance is the instance id of the primary provider.
const DefaultInstance = "default"

// Instance returns the named provider instance and whether it exists. "default" is the
// receiver: the primary is an instance like any other, and a copy resolves it to itself
// so a run's pinned model never drifts to another copy's. Implements
// agent.ProviderInstances: the shell fails a run pinned to an unknown name loudly,
// never silently falling back to the primary.
func (p *Provider) Instance(name string) (agent.Provider, bool) {
	if name == DefaultInstance {
		return p, true
	}
	inst, ok := p.snapshot().instances[name]
	if !ok {
		return nil, false
	}
	return inst, true
}

// InstanceNames lists the configured instance ids: the primary first, then the named
// rows sorted.
func (p *Provider) InstanceNames() []string {
	instances := p.snapshot().instances
	names := make([]string, 0, len(instances)+1)
	names = append(names, DefaultInstance)
	for n := range instances {
		if n == DefaultInstance {
			continue
		}
		names = append(names, n)
	}
	sort.Strings(names[1:])
	return names
}

// snapshot copies the mutable state into a lock-free Provider value: the one way a
// caller reads fields while a live config swap is possible. The copy carries the
// instances map but never a mutex use - a fresh zero mutex is created with the copy.
func (p *Provider) snapshot() *Provider {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return &Provider{
		typ:           p.typ,
		modelName:     p.modelName,
		baseURL:       p.baseURL,
		apiKey:        p.apiKey,
		thinkingLevel: p.thinkingLevel,
		instances:     p.instances,
	}
}

// SetConfig swaps the provider's configuration live. The MODEL the base serves is
// deliberately NOT taken from cfg: /model mutates it in place, runs pin their own
// copies, and a settings save must not silently undo a picker choice (the model_name
// knob keeps its restart semantics).
func (p *Provider) SetConfig(cfg Config) {
	next := NewProvider(cfg.Type, "", cfg.Host, cfg.APIKey)
	snapshot := p.snapshot()
	instances := make(map[string]*Provider, len(cfg.Instances))
	for name, inst := range cfg.Instances {
		if name == DefaultInstance {
			continue // the primary IS the base provider
		}
		instances[name] = NewProvider(inst.Type, "", inst.Host, inst.APIKey)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.typ = next.typ
	p.baseURL = next.baseURL
	p.apiKey = next.apiKey
	p.instances = instances
	p.modelName = snapshot.modelName
	p.thinkingLevel = snapshot.thinkingLevel
}

// NewProvider creates a Provider from the given configuration fields.
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

// initDefaults resolves the API key from env vars and fills in the default base URL.
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

// Stream sends a completion request to the provider and streams events.
func (p *Provider) Stream(ctx context.Context, messages []core.Message, tools []llm.ToolSchema) <-chan core.ProviderStreamEvent {
	s := p.snapshot()
	events := make(chan core.ProviderStreamEvent, 64)
	go func() {
		defer close(events)
		msgJSON := messagesToJSON(messages, s.typ == "openai")
		if s.typ == "ollama" {
			imagesToOllamaWire(msgJSON)
		}
		toolJSON := toolSchemasToJSON(tools)
		var end core.StreamEnd
		switch s.typ {
		case "ollama":
			end = s.streamOllama(ctx, events, msgJSON, toolJSON)
		case "openai":
			end = s.streamOpenAI(ctx, events, msgJSON, toolJSON)
		case "anthropic":
			end = s.streamAnthropic(ctx, events, msgJSON, toolJSON)
		case "mock":
			end = s.streamMock(ctx, events, msgJSON, toolJSON)
		default:
			end = core.StreamEnd{Type: "stream_end", FinishReason: "error", Error: fmt.Sprintf("unknown provider type: %s", s.typ)}
		}
		select {
		case <-ctx.Done():
		case events <- end:
		}
	}()
	return events
}

// ModelName returns the current model name.
func (p *Provider) ModelName() string { return p.snapshot().modelName }

// SetThinkingLevel sets the thinking level for subsequent streams.
func (p *Provider) SetThinkingLevel(level string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.thinkingLevel = level
}

// SetModel points the base provider at another model in place. Runs and slots derive
// their own WithModel copies, so the switch never reaches a run already streaming.
func (p *Provider) SetModel(model string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.modelName = model
}

// WithModel returns a copy of the provider serving the given model.
func (p *Provider) WithModel(model string) agent.Provider {
	cp := p.snapshot()
	cp.modelName = model
	return cp
}

// WithThinkingLevel returns a copy of the provider streaming at the given thinking.
// Copy discipline matches WithModel: the receiver is left unchanged, so per-run
// providers derived from the mounted base never mutate it.
func (p *Provider) WithThinkingLevel(level string) agent.Provider {
	cp := p.snapshot()
	cp.thinkingLevel = level
	return cp
}

// ListModelEntries fetches available models from the provider API with the capability.
// The ollama family's /api/tags lists "capabilities" per model (the keypool mirror
// passes it through); the openai/anthropic lists carry ids only, so Capabilities stays
// nil for those.
func (p *Provider) ListModelEntries() ([]core.ModelEntry, error) {
	return p.snapshot().listModelEntries()
}

// ListModels fetches available model names from the provider API - the
// plain projection of listModelEntries, the agent.Provider contract.
func (p *Provider) ListModels() ([]string, error) {
	entries, err := p.snapshot().listModelEntries()
	if err != nil {
		return nil, err
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name
	}
	return names, nil
}

// ModelInfo queries the provider for the current model's context window.
func (p *Provider) ModelInfo() (llm.ModelInfo, error) {
	ctx, err := p.snapshot().modelInfo()
	if err != nil {
		return llm.ModelInfo{}, err
	}
	return llm.ModelInfo{ContextWindow: ctx}, nil
}

// message/tool serialization

// messagesToJSON serializes core.Message values to maps with a role field added based.
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
		case core.YieldResult:
			m["role"] = "tool"
		}
		// The OpenAI wire format requires each tool_calls entry to be.
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
		// The tree pointer is store-level truth, never provider wire.
		delete(m, "parent_id")
		// Non-image attachment references are local disk paths. The agent side surfaces the
		// reference in its own context - the provider wire never carries it.
		delete(m, "attachments")
		msgJSON[i] = m
	}
	return msgJSON
}

// imagesToOllamaWire rewrites user-message images from the generic object shape. In
// place over the message maps: an object list sent to ollama 400s with "cannot
// unmarshal object into Alias.images.0" - verified live.
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

// toolSchemasToJSON converts ToolSchema values to the generic map format expected.
func toolSchemasToJSON(tools []llm.ToolSchema) []map[string]any {
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

// Ollama

func sendEvent(ctx context.Context, events chan<- core.ProviderStreamEvent, event core.ProviderStreamEvent) {
	select {
	case <-ctx.Done():
	case events <- event:
	}
}

// flushToolCalls sends accumulated tool-call events in index order.
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

// buildListModelsRequest constructs the HTTP request for listing models based.
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

// listModelEntries fetches the model list and parses it into entries with capability.
func (p *Provider) listModelEntries() ([]core.ModelEntry, error) {
	// The mock serves its model list directly - no request, no network, exactly.
	if p.typ == "mock" {
		return mockModelEntries(p.modelName), nil
	}
	req, err := p.buildListModelsRequest()
	if err != nil {
		return nil, err
	}

	resp, err := llm.HTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var result struct {
		Models []struct {
			Name         string   `json:"name"`
			ID           string   `json:"id"`
			Capabilities []string `json:"capabilities"`
		} `json:"models"`
		Data []struct {
			Name string `json:"name"`
			ID   string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	var entries []core.ModelEntry
	for _, m := range result.Models {
		if m.Name != "" {
			entries = append(entries, core.ModelEntry{Name: m.Name, Capabilities: m.Capabilities})
		}
	}
	for _, m := range result.Data {
		if m.ID != "" {
			entries = append(entries, core.ModelEntry{Name: m.ID})
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, nil
}

// modelInfo queries the provider for the current model's context window.
func (p *Provider) modelInfo() (int, error) {
	switch p.typ {
	case "mock":
		// The mock exposes no context window; callers fall back to the config /.
		return 0, nil
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
		resp, err := llm.HTTPClient().Do(req)
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
		// model_info keys are "<arch>.context_length" e.g. "gemma4.context_length": 131072.
		for k, v := range result.ModelInfo {
			if strings.HasSuffix(k, ".context_length") {
				if n, ok := v.(float64); ok {
					return int(n), nil
				}
			}
		}
		return 0, nil
	case "openai", "anthropic":
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
		resp, err := llm.HTTPClient().Do(req)
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

// thinking level mappers
