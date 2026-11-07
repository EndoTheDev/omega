package api

// TestSetDefaultWorkspaceRefusal lives in
// server_defaultworkspace_test.go (TestSetDefaultWorkspaceGuards) -
// the setter's subject file. This file only carries the catalog pin.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/llm"
)

// timedProvider is a minimal Provider whose /models listing can be
// delayed - the dead instance in the grouped-catalog pin.
type timedProvider struct {
	modelName string
	models    []string
	listDelay time.Duration
}

func (p *timedProvider) ModelName() string                       { return p.modelName }
func (p *timedProvider) SetThinkingLevel(string)                 {}
func (p *timedProvider) WithModel(m string) agent.Provider       { cp := *p; cp.modelName = m; return &cp }
func (p *timedProvider) WithThinkingLevel(string) agent.Provider { return p }
func (p *timedProvider) ListModels() ([]string, error) {
	if p.listDelay > 0 {
		time.Sleep(p.listDelay)
	}
	return p.models, nil
}
func (p *timedProvider) ModelInfo() (llm.ModelInfo, error) {
	return llm.ModelInfo{ContextWindow: 4096}, nil
}
func (p *timedProvider) Stream(context.Context, []core.Message, []llm.ToolSchema) <-chan core.ProviderStreamEvent {
	ch := make(chan core.ProviderStreamEvent, 2)
	ch <- core.ResponseChunk{Type: "response_chunk", Content: "x"}
	ch <- core.StreamEnd{Type: "stream_end", FinishReason: "stop"}
	close(ch)
	return ch
}

// TestModelsSectionsDeadInstance: one unreachable instance must not
// stall GET /models (part 1) - the answer comes back with the live
// sections' models and the dead section's Error named.
func TestModelsSectionsDeadInstance(t *testing.T) {
	old := modelSectionsTimeout
	modelSectionsTimeout = 100 * time.Millisecond
	t.Cleanup(func() { modelSectionsTimeout = old })

	base := &timedProvider{modelName: "base", models: []string{"base-model"}}
	slow := &timedProvider{modelName: "slow", models: []string{"never"}, listDelay: 30 * time.Second}
	wrapped := &instancesProvider{Provider: base, instances: map[string]agent.Provider{"slow": slow}}
	a := agent.NewAgent(wrapped, nil, 0)
	a.SetLoopProvider(testLoop{})
	s := NewServer(a, nil, nil, nil, nil, nil)

	start := time.Now()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/models", nil))
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("GET /models blocked %v behind the dead instance", took)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Providers []ModelSection `json:"providers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	byName := map[string]ModelSection{}
	for _, sec := range body.Providers {
		byName[sec.Name] = sec
	}
	baseSec, ok := byName["default"]
	if !ok {
		t.Fatalf("no default section in %v", byName)
	}
	if got := baseSec.Models; len(got) != 1 || got[0] != "base-model" {
		t.Fatalf("default section lost its models to the dead instance: %v (error %q)", got, baseSec.Error)
	}
	if baseSec.Error != "" {
		t.Fatalf("default section carries an error: %q", baseSec.Error)
	}
	slowSec, ok := byName["slow"]
	if !ok {
		t.Fatalf("no slow section in %v", byName)
	}
	if slowSec.Error == "" {
		t.Fatal("dead instance section carried no Error - the stall would read as an empty provider")
	}
	if len(slowSec.Models) != 0 {
		t.Fatalf("dead instance section carries models: %v", slowSec.Models)
	}
}
