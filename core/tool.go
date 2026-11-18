package core

import (
	"context"
	"maps"
)

// ToolSchema describes a tool the model may call.
type ToolSchema struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// ToolAction is one named sub-action of a domain tool.
type ToolAction struct {
	Name        string
	Description string // one-liner, labeled under the tool in the prompt
}

// Tool is a callable the model may invoke.
type Tool struct {
	Description string
	Parameters  map[string]any
	Run         func(ctx context.Context, args map[string]any) (string, error)

	Actions []ToolAction

	AliasOf string

	// Origin stamps WHERE the tool was registered from, for surface routing. Modules
	// outside the mcp bridge never set it.
	Origin string

	// Yield declares an IMMEDIATE-yield tool: a call on it ENDS the turn without any
	// inline attempt - the loop dispatches the call on its own goroutine, the turn ends,
	// and the result re-enters later as a synthetic core.YieldResult input message. The
	// model is never told - the registry decides silently; no prompt, schema, or
	// description change of any kind.
	Yield bool
}

// ToolOriginMCP is the Origin value an MCP-bridge-registered tool carries. The only
// comparison point - consumers filter on this exact value, never on dotted-name shape.
const ToolOriginMCP = "mcp"

// ActionNames returns the action names in declaration order.
func (t Tool) ActionNames() []string {
	if len(t.Actions) == 0 {
		return nil
	}
	names := make([]string, len(t.Actions))
	for i, a := range t.Actions {
		names[i] = a.Name
	}
	return names
}

// SchemaParameters returns the tool's parameter schema with the "action" property's.
func (t Tool) SchemaParameters() map[string]any {
	if t.Actions == nil {
		return t.Parameters
	}
	out := maps.Clone(t.Parameters)
	if out == nil {
		out = map[string]any{}
	}
	propsRaw, _ := out["properties"]
	props, _ := propsRaw.(map[string]any)
	if props == nil {
		props = map[string]any{}
	}
	newProps := maps.Clone(props)
	actionRaw := props["action"]
	action, _ := actionRaw.(map[string]any)
	newAction := maps.Clone(action)
	if newAction == nil {
		newAction = map[string]any{}
	}
	newAction["type"] = "string"
	newAction["enum"] = t.ActionNames()
	newProps["action"] = newAction
	out["properties"] = newProps
	return out
}

// ModelInfo holds metadata about the current model, queried from the provider when.
// Zero values mean the provider does not expose that field; callers fall back to config
// defaults.
type ModelInfo struct {
	ContextWindow int // max context in tokens, 0 if unknown
}

// ModelEntry is one listed model with the provider's capability flags. Capabilities are
// nil when the provider's list endpoint does not carry them (the openai/anthropic id
// lists) - callers that need a slot filter fall back to a curated map for those.
type ModelEntry struct {
	Name         string   `json:"name"`
	Capabilities []string `json:"capabilities,omitempty"`
}
