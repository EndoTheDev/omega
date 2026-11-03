package core

import (
	"context"
	"maps"
)

// ToolSchema describes a tool the model may call. It is passed to
// the provider and serialized into the API request.
type ToolSchema struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// ToolAction is one named sub-action of a domain tool. A tool that
// packs several verbs behind one name declares them here so the JSON
// schema enum, the system prompt, and the /tools inventory all derive
// from this one source of truth - enum drift becomes impossible.
type ToolAction struct {
	Name        string
	Description string // one-liner, labeled under the tool in the prompt
}

// Tool is a callable the model may invoke. The map key is the tool name.
type Tool struct {
	Description string
	Parameters  map[string]any
	Run         func(ctx context.Context, args map[string]any) (string, error)

	// Actions optionally lists the sub-actions of a domain tool. When
	// set, the "action" property's enum in the JSON schema derives from
	// these names (see SchemaParameters), and the prompt + /tools
	// render the split (domain line, then labeled sub-actions).
	Actions []ToolAction

	// AliasOf marks a backward-compatibility shim: an old tool name
	// that maps onto a folded domain tool. Aliases stay callable by
	// the model until the v0.12 mount migration retires them but are
	// hidden from the prompt and the /tools inventory, which show the
	// folded tool only.
	AliasOf string

	// Origin stamps WHERE the tool was registered from, for surface
	// routing (card t_c2da3379): "mcp" = an MCP bridge tool (belongs
	// to the webui MCP tab only), "" = native. Nothing keys on the
	// name - native tools like sessions.search/shell.run are dotted
	// too. Modules outside the mcp bridge never set it.
	Origin string
}

// ToolOriginMCP is the Origin value an MCP-bridge-registered tool
// carries. The only comparison point - consumers filter on this exact
// value, never on dotted-name shape.
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

// SchemaParameters returns the tool's parameter schema with the
// "action" property's enum derived from Actions. Tools without
// Actions are returned untouched. The derivation replaces only the
// enum (and injects the property if the tool forgot to declare it);
// any property description survives.
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

// ModelInfo holds metadata about the current model, queried from the
// provider when available. Zero values mean the provider does not
// expose that field; callers fall back to config defaults.
type ModelInfo struct {
	ContextWindow int // max context in tokens, 0 if unknown
}

// ModelEntry is one listed model with the provider's capability flags
// (card t_70c28128: the typed model pickers' data). Capabilities are
// nil when the provider's list endpoint does not carry them (the
// openai/anthropic id lists) - callers that need a slot filter fall
// back to a curated map for those.
type ModelEntry struct {
	Name         string   `json:"name"`
	Capabilities []string `json:"capabilities,omitempty"`
}