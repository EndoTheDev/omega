package provider

// The mock provider (`type: mock` in the provider section; card
// t_150ce9fc) answers every request with a deterministic, network-free
// response seeded by the request itself: same input, same output. It
// implements agent.Provider at the same seam as the real transports,
// so tests and journeys can run full agent flows without burning
// provider tokens.
//
// Determinism contract: the seed is a hash over the identity-bearing
// fields of the request (role, content, tool-call names and arguments)
// and the behavior rules never consult clocks, randomness, or env. One
// behavior rule drives the whole thing - the mock calls the FIRST
// announced tool exactly once per fresh user prompt, and after it
// consumes the tool result it answers with text. Every other turn is
// a seeded text echo streamed in fixed-size chunks.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/EndoTheDev/omega/core"
)

// mockModelEntries serves the model list without network: the mock
// answers with exactly the configured model name.
func mockModelEntries(modelName string) []core.ModelEntry {
	return []core.ModelEntry{{Name: modelName}}
}

// mockSeed derives the stable seed from the wire-ready messages. Only
// identity-bearing fields feed it - roles, content, tool-call names
// and arguments - so transport noise (timestamps, timings, call ids,
// tree pointers) cannot perturb a request that means the same thing.
func mockSeed(messages []map[string]any) string {
	var sb strings.Builder
	for _, m := range messages {
		fmt.Fprint(&sb, m["role"], "|")
		if c, ok := m["content"].(string); ok {
			fmt.Fprint(&sb, c, "|")
		}
		if calls, ok := m["tool_calls"].([]map[string]any); ok {
			for _, c := range calls {
				fn, _ := c["function"].(map[string]any)
				fmt.Fprint(&sb, fn["name"], "|")
				b, _ := json.Marshal(fn["arguments"])
				fmt.Fprint(&sb, string(b), "|")
			}
		}
		sb.WriteString("\n")
	}
	sum := sha256.Sum256([]byte(sb.String()))
	return hex.EncodeToString(sum[:])[:12]
}

// streamMock produces the deterministic stream for typ "mock".
// Behavior: last message a tool result (or no tools declared) -
// stream the seeded text reply. Otherwise - one tool call for the
// first announced tool, FinishReason "tool_call".
func (p *Provider) streamMock(ctx context.Context, events chan<- core.ProviderStreamEvent, messages, tools []map[string]any) core.StreamEnd {
	seed := mockSeed(messages)
	var lastRole string
	if n := len(messages); n > 0 {
		lastRole, _ = messages[n-1]["role"].(string)
	}
	if lastRole != "tool" && len(tools) > 0 {
		call := mockToolCall(tools[0], seed, messages)
		sendEvent(ctx, events, core.ToolCallEvent{Type: "tool_call", ToolCall: call})
		return core.StreamEnd{Type: "stream_end", FinishReason: "tool_call"}
	}
	user := ""
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i]["role"] == "user" {
			user, _ = messages[i]["content"].(string)
			break
		}
	}
	for _, chunk := range mockChunks(mockReply(seed, user, lastRole == "tool")) {
		sendEvent(ctx, events, core.ResponseChunk{Type: "response_chunk", Content: chunk})
	}
	return core.StreamEnd{Type: "stream_end", FinishReason: "stop"}
}

// mockToolCall builds the deterministic tool call for the first
// announced tool. The id is stamped from how many assistant tool-call
// turns the transcript already carries (so ids stay unique across
// the loop), and required properties are filled from the schema:
// strings take the seeded filler, numbers take 1, booleans take true,
// collections take empties - every tool callable by the mock runs.
func mockToolCall(tool map[string]any, seed string, messages []map[string]any) core.ToolCall {
	name, _ := tool["name"].(string)
	seen := 0
	for _, m := range messages {
		if m["role"] == "assistant" {
			if calls, ok := m["tool_calls"].([]map[string]any); ok && len(calls) > 0 {
				seen++
			}
		}
	}
	args := map[string]any{}
	params, _ := tool["parameters"].(map[string]any)
	props, _ := params["properties"].(map[string]any)
	required, _ := params["required"].([]any)
	for _, r := range required {
		rn, ok := r.(string)
		if !ok {
			continue
		}
		var typ any
		if prop, ok := props[rn].(map[string]any); ok {
			typ = prop["type"]
		}
		args[rn] = mockFill(typ, seed)
	}
	return core.ToolCall{ID: fmt.Sprintf("mock_call_%d", seen+1), Name: name, Arguments: args}
}

// mockFill derives the deterministic value for a required schema
// property of the given JSON type.
func mockFill(typ any, seed string) any {
	switch typ {
	case "string":
		return "mock-" + seed
	case "number", "integer":
		return 1
	case "boolean":
		return true
	case "object":
		return map[string]any{}
	case "array":
		return []any{}
	default:
		return nil
	}
}

// mockReply composes the seeded deterministic text: a one-line echo of
// the prompt (the tool result it just consumed, on a post-tool turn)
// prefixed with the seed tag. Truncated to keep the reply one line.
func mockReply(seed, prompt string, fromTool bool) string {
	body := prompt
	if fromTool {
		body = "tool result consumed"
	}
	body = strings.TrimSpace(body)
	if body == "" {
		body = "(empty)"
	}
	const max = 160
	if len(body) > max {
		body = body[:max]
	}
	var who string
	if fromTool {
		who = "tool"
	} else {
		who = "prompt"
	}
	return fmt.Sprintf("[mock %s %s] %s", who, seed, body)
}

// mockChunks streams the reply in fixed-size RUNE chunks so consumers
// that care see real streaming shape instead of one mega chunk.
func mockChunks(text string) []string {
	const size = 16
	runes := []rune(text)
	if len(runes) == 0 {
		return nil
	}
	out := make([]string, 0, len(runes)/size+1)
	for i := 0; i < len(runes); i += size {
		end := i + size
		if end > len(runes) {
			end = len(runes)
		}
		out = append(out, string(runes[i:end]))
	}
	return out
}
