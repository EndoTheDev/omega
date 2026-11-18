package provider

// Live probe for the prompt-cache economics
// ladder step 1). Skipped unless KEYPOOL_CLIENT_KEY is set - no secrets
// in the tree.
//
//	KEYPOOL_CLIENT_KEY=<key> KEYPOOL_HOST=http://<host>:11435 \
// go test./harness/provider/ -run TestLiveToolBlockPromptCache -v
//
// What it measures: the tools block is the wire's largest stable
// prefix, and a prefix cache reuses only BYTE-IDENTICAL prefixes. This
// probe sends a production-sized (~25KB) tools block three times - an
// identical repeat, then an extended conversation - and reports
// prompt_eval_cached_count against prompt_eval_count for each call.
//
// The verdict is read by a human, not asserted: a gateway without cache
// telemetry answers cached=0 on every call, which is a lane fact, not a
// product failure. The deterministic laws live in tools_block_test.go
// (rendered block byte-stable) and agent/loop (sorted + hoisted).
//
// Measured on glm-5.3 through the keypool gateway, same
// 26,159-byte / 25-tool block, three calls per shape:
//
//	pre-fix shape (toolSchemasToJSON fed from a map range):
// turn 1 cold prompt_eval=6753 cached=0
// turn 2 repeat prompt_eval=6753 cached=0
//	  turn 3 extended prompt_eval=6766 cached=0
//	current shape (sorted slice rendered verbatim):
// turn 1 cold prompt_eval=6753 cached=0
// turn 2 repeat prompt_eval=6753 cached=6720
//	  turn 3 extended prompt_eval=6766 cached=6720
//
// i.e. 0 of 6,753 prompt tokens reused before, 6,720 of 6,753 after:
// the cacheable prefix moved from the system prompt alone to the whole
// system+system-tools block, and the extension call reused it too.

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/llm"
)

// productionSizedTools builds a tools block sized like omega's real registry.
func productionSizedTools() []llm.ToolSchema {
	domains := []string{
		"shell", "files", "memory", "skills", "cron", "kanban", "delegate",
		"verdict", "search", "browser", "insights", "journal", "canvas",
		"continuous", "notifications", "approval", "mcp", "store", "trust",
		"compactor", "prompt", "theme", "settings", "sessions", "metrics",
	}
	tools := make([]llm.ToolSchema, 0, len(domains))
	for _, domain := range domains {
		tools = append(tools, llm.ToolSchema{
			Name: domain + ".run",
			Description: fmt.Sprintf("%s operations. Runs one %s action against the live workspace: "+
				"reads and writes state, reports a structured result, and never panics into the caller - "+
				"every failure returns an error string the model can act on. Call it when the task "+
				"needs the %s domain; do not call it to answer a question the conversation already covers.",
				domain, domain, domain),
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action": map[string]any{
						"type":        "string",
						"description": "which " + domain + " action to run",
						"enum":        []any{"list", "read", "write", "delete", "status"},
					},
					"target":  map[string]any{"type": "string", "description": "the " + domain + " target (path, id, or name)"},
					"content": map[string]any{"type": "string", "description": "payload for write actions"},
					"options": map[string]any{
						"type":        "object",
						"description": "per-call modifiers; unknown keys are ignored",
						"properties": map[string]any{
							"recursive": map[string]any{"type": "boolean"},
							"limit":     map[string]any{"type": "integer"},
							"format":    map[string]any{"type": "string", "enum": []any{"text", "json"}},
						},
					},
					"call_id": map[string]any{"type": "string", "description": "opaque id echoed back in the result"},
				},
				"required": []any{"action", "target"},
			},
		})
	}
	return tools
}

// TestLiveToolBlockPromptCache reports the cache economics of a stable tools block.
func TestLiveToolBlockPromptCache(t *testing.T) {
	if os.Getenv("KEYPOOL_CLIENT_KEY") == "" {
		t.Skip("KEYPOOL_CLIENT_KEY not set; live tool-block cache probe skipped")
	}
	host := strings.TrimSuffix(os.Getenv("KEYPOOL_HOST"), "/v1")
	if host == "" {
		host = strings.TrimSuffix(keypoolDefaultHost, "/v1")
	}
	p := &Provider{typ: "ollama", baseURL: host, modelName: keypoolModel, apiKey: os.Getenv("KEYPOOL_CLIENT_KEY")}

	tools := productionSizedTools()
	if raw, err := jsonLen(tools); err == nil {
		t.Logf("tools block: %d tools, %d bytes rendered", len(tools), raw)
	}

	base := []core.Message{
		core.NewSystem("You are a terse assistant. Answer in one word unless told otherwise."),
		core.NewUser("Reply with the single word: ready."),
	}

	run := func(label string, messages []core.Message) core.StreamEnd {
		end := drainEvents(p.Stream(testContext(), messages, tools))
		if end.Error != "" {
			t.Fatalf("%s: stream error: %s", label, end.Error)
		}
		t.Logf("%-18s prompt_eval=%v cached=%v eval=%v", label,
			deref(end.PromptEvalCount), deref(end.CachedCount), deref(end.EvalCount))
		return end
	}

	first := run("turn 1 (cold)", base)
	second := run("turn 2 (repeat)", base)
	extended := append(append([]core.Message{}, base...),
		core.Assistant{Content: "ready"},
		core.NewUser("Reply with the single word: again."),
	)
	third := run("turn 3 (extended)", extended)

	if second.CachedCount != nil && second.PromptEvalCount != nil && *second.PromptEvalCount > 0 {
		t.Logf("VERDICT repeat reuse: %d of %d prompt tokens cached (%.0f%%)",
			*second.CachedCount, *second.PromptEvalCount,
			100*float64(*second.CachedCount)/float64(*second.PromptEvalCount))
	}
	if third.CachedCount != nil && first.PromptEvalCount != nil && *first.PromptEvalCount > 0 {
		t.Logf("VERDICT extension reuse: %d cached against a %d-token prefix (%.0f%%)",
			*third.CachedCount, *first.PromptEvalCount,
			100*float64(*third.CachedCount)/float64(*first.PromptEvalCount))
	}
}

// jsonLen renders the tools block the same way the ollama wire does and
// returns its byte length.
func jsonLen(tools []llm.ToolSchema) (int, error) {
	apiTools := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		apiTools = append(apiTools, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        tool.Name,
				"description": tool.Description,
				"parameters":  tool.Parameters,
			},
		})
	}
	raw, err := json.Marshal(apiTools)
	if err != nil {
		return 0, err
	}
	return len(raw), nil
}
