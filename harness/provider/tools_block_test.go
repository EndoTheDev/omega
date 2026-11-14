package provider

// The rendered tools block, measured on the wire.
//
// agent/loop owns the ORDER of the schema list (toolSchemas sorts by
// name) and the CADENCE (one derivation per run, not per turn). This
// file owns the third link in that chain: the provider must render the
// list it is handed byte-for-byte, with no re-shuffle of its own.
// toolSchemasToJSON is the shared conversion for all three wires; the
// ollama path then re-shapes each entry into the function envelope.
// json.Marshal sorts map keys, so the only way the block can drift is a
// reorder of the slice - which is what this pins.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/llm"
)

// renderedToolsFixture is a fixed registry whose insertion order is NOT
// name order, so an accidental sort (or an accidental re-range of a
// map) changes the rendered bytes and reds the test.
func renderedToolsFixture() []llm.ToolSchema {
	names := []string{
		"shell.run",
		"files.read",
		"kanban",
		"skills",
		"cron",
		"files.write",
		"memory",
		"delegate.task",
		"verdict.request",
	}
	tools := make([]llm.ToolSchema, 0, len(names))
	for i, name := range names {
		tools = append(tools, llm.ToolSchema{
			Name:        name,
			Description: fmt.Sprintf("tool %d: %s", i, name),
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":    map[string]any{"type": "string", "description": "target path"},
					"content": map[string]any{"type": "string"},
				},
				"required": []any{"path"},
			},
		})
	}
	return tools
}

// renderedTools returns the raw tools[] bytes of a captured ollama
// request body, exactly as the server received them.
func renderedTools(t *testing.T, body []byte) []byte {
	t.Helper()
	var sent struct {
		Tools json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatalf("request body is not JSON: %v", err)
	}
	if len(sent.Tools) == 0 {
		t.Fatalf("request body carries no tools block: %s", body)
	}
	return sent.Tools
}

// TestOllamaRenderedToolsBlockByteStable pins the wire half of the
// prompt-cache contract: two consecutive turns of one run - the same
// mounted tools, the second turn carrying one more assistant/user pair -
// render a byte-identical tools[] block.
//
// A prefix cache reuses only a byte-identical PREFIX and re-prefills
// from the first differing token, so a block that reshuffles between
// turns moves the divergence to the START of the tools array and drops
// the cacheable prefix back to the system prompt alone. The 2026-11-12
// measurement of the pre-fix build was the inverse of this test: same
// 9,865 bytes, different order.
func TestOllamaRenderedToolsBlockByteStable(t *testing.T) {
	bodies := make(chan []byte, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies <- body
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprintln(w, `{"message":{"content":"ok"}}`)
		fmt.Fprintln(w, `{"done":true,"prompt_eval_count":4000,"prompt_eval_cached_count":3900,"eval_count":2}`)
	}))
	defer srv.Close()

	p := &Provider{typ: "ollama", baseURL: srv.URL, modelName: "glm-5.3"}
	tools := renderedToolsFixture()

	turn1 := []core.Message{core.NewSystem("system prompt"), core.NewUser("first turn")}
	first := drainEvents(p.Stream(testContext(), turn1, tools))
	body1 := <-bodies
	if first.Error != "" {
		t.Fatalf("turn 1 stream error: %s", first.Error)
	}

	turn2 := append(append([]core.Message{}, turn1...),
		core.Assistant{Content: "first answer"},
		core.NewUser("second turn"),
	)
	second := drainEvents(p.Stream(testContext(), turn2, tools))
	body2 := <-bodies
	if second.Error != "" {
		t.Fatalf("turn 2 stream error: %s", second.Error)
	}

	// The bodies as a whole MUST differ - the conversation moved on. A
	// byte-identical body would mean the second turn never sent its new
	// messages, and the tools comparison below would be vacuous.
	if bytes.Equal(body1, body2) {
		t.Fatalf("turn 2 request body is identical to turn 1; the new turn did not reach the wire")
	}

	block1, block2 := renderedTools(t, body1), renderedTools(t, body2)
	if !bytes.Equal(block1, block2) {
		t.Fatalf("rendered tools[] differs between consecutive turns of one run\n turn 1: %s\n turn 2: %s", block1, block2)
	}
	t.Logf("tools block: %d bytes, byte-identical across both turns, %d tools", len(block2), len(tools))

	// The provider preserves the order it is handed - it neither sorts
	// nor re-ranges. Order authority is agent/loop's toolSchemas; a
	// silent sort here would hide a regression there.
	var rendered []struct {
		Type     string `json:"type"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if err := json.Unmarshal(block2, &rendered); err != nil {
		t.Fatalf("tools block is not JSON: %v", err)
	}
	if len(rendered) != len(tools) {
		t.Fatalf("rendered %d tools, want %d", len(rendered), len(tools))
	}
	for i, want := range tools {
		if rendered[i].Function.Name != want.Name {
			t.Fatalf("tool %d rendered as %q, want %q (provider must not reorder)", i, rendered[i].Function.Name, want.Name)
		}
		if rendered[i].Type != "function" {
			t.Fatalf("tool %d type = %q, want function", i, rendered[i].Type)
		}
	}

	// The usage counters the empirical gate reads ride the same request:
	// prompt_eval_cached_count must survive as CachedCount, else the
	// cache can be broken and never seen.
	if second.CachedCount == nil || *second.CachedCount != 3900 {
		t.Fatalf("CachedCount = %v, want 3900 (prompt_eval_cached_count)", second.CachedCount)
	}
	if second.PromptEvalCount == nil || *second.PromptEvalCount != 4000 {
		t.Fatalf("PromptEvalCount = %v, want 4000", second.PromptEvalCount)
	}
	if !bytes.Contains(block2, []byte(`"shell.run"`)) {
		t.Fatalf("rendered block lost its first tool name: %s", block2)
	}
}
