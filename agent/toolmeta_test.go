package agent

import (
	"github.com/EndoTheDev/omega/core"
	"reflect"
	"testing"
)

// fakeProvider is a ToolProvider stub for mount tests.
type fakeProvider struct {
	tools map[string]core.Tool
}

func (f fakeProvider) Tools() map[string]core.Tool { return f.tools }

// fakeMountPlugin mounts one provider plus its alias entries.
type fakeMountPlugin struct {
	name     string
	provides []string
	mount    func(ctx *Context) error
}

func (f fakeMountPlugin) Name() string             { return f.name }
func (f fakeMountPlugin) Provides() []string       { return f.provides }
func (f fakeMountPlugin) Requires() []string       { return nil }
func (f fakeMountPlugin) Mount(ctx *Context) error { return f.mount(ctx) }

// mountAllHelper wires MountAll over fake plugins.
func mountAllHelper(t *testing.T, ps []Plugin, ctx *Context) {
	t.Helper()
	if err := MountAll(ps, ctx); err != nil {
		t.Fatalf("MountAll: %v", err)
	}
}

// TestActionNames pins the action name list, in declaration order.
func TestActionNames(t *testing.T) {
	tool := core.Tool{Actions: []core.ToolAction{{Name: "add"}, {Name: "replace"}, {Name: "remove"}}}
	got := tool.ActionNames()
	if !reflect.DeepEqual(got, []string{"add", "replace", "remove"}) {
		t.Fatalf("ActionNames = %v", got)
	}
	// A tool without Actions reports nil, not an empty slice.
	var empty core.Tool
	got = empty.ActionNames()
	if got != nil {
		t.Fatalf("ActionNames without Actions = %v, want nil", got)
	}
}

// TestSchemaParametersDerivesActionEnum pins the drift fix: the action
// enum in the JSON schema comes from the Actions metadata, so a schema
// that hand-maintains its enum cannot drift out of sync.
func TestSchemaParametersDerivesActionEnum(t *testing.T) {
	tool := core.Tool{
		Actions: []core.ToolAction{{Name: "add"}, {Name: "replace"}, {Name: "remove"}},
		Parameters: map[string]any{
			"type":     "object",
			"required": []string{"action", "target"},
			"properties": map[string]any{
				"action": map[string]any{
					"type":        "string",
					"enum":        []string{"add", "replace", "remove", "FORGOTTEN_DRIFTED_ACTION"},
					"description": "What to do.",
				},
				"target": map[string]any{"type": "string", "enum": []string{"memory", "user"}},
			},
		},
	}
	got := tool.SchemaParameters()
	action := got["properties"].(map[string]any)["action"].(map[string]any)
	enum, _ := action["enum"].([]string)
	if !reflect.DeepEqual(enum, []string{"add", "replace", "remove"}) {
		t.Fatalf("action enum %v does not derive from Actions metadata", enum)
	}
	if action["description"] != "What to do." {
		t.Fatalf("action description lost: %v", action["description"])
	}
	// Sibling properties are untouched.
	target := got["properties"].(map[string]any)["target"].(map[string]any)
	if !reflect.DeepEqual(target["enum"], []string{"memory", "user"}) {
		t.Fatalf("sibling property mutated: %v", target)
	}
	// The original schema is not mutated.
	orig := tool.Parameters["properties"].(map[string]any)["action"].(map[string]any)
	origEnum, _ := orig["enum"].([]string)
	if len(origEnum) != 4 {
		t.Fatalf("SchemaParameters mutated the source schema: %v", origEnum)
	}
}

// TestSchemaParametersInjectsActionProperty covers a domain tool that
// declares Actions but forgot the property in its schema: the enum is
// still derived, the property appears.
func TestSchemaParametersInjectsActionProperty(t *testing.T) {
	tool := core.Tool{
		Actions:    []core.ToolAction{{Name: "task"}, {Name: "status"}},
		Parameters: map[string]any{"type": "object", "properties": map[string]any{}},
	}
	got := tool.SchemaParameters()
	action, ok := got["properties"].(map[string]any)["action"].(map[string]any)
	if !ok {
		t.Fatal("action property not injected")
	}
	if action["type"] != "string" {
		t.Fatalf("action type = %v", action["type"])
	}
	if !reflect.DeepEqual(action["enum"], []string{"task", "status"}) {
		t.Fatalf("injected enum = %v", action["enum"])
	}
}

// TestSchemaParametersPassthrough covers plain tools: the schema map is
// returned untouched when no Actions are declared.
func TestSchemaParametersPassthrough(t *testing.T) {
	params := map[string]any{"type": "object", "properties": map[string]any{"command": map[string]any{"type": "string"}}}
	tool := core.Tool{Parameters: params}
	if tool.SchemaParameters() == nil {
		t.Fatal("nil schema returned")
	}
	got := tool.SchemaParameters()
	if _, ok := got["properties"].(map[string]any)["action"]; ok {
		t.Fatal("action property injected on a tool without Actions")
	}
}

// TestMountAllAliasesHiddenFromInventory pins the alias contract:
// compat-shim entries stay registered (the model can still call them)
// but the extension metadata - prompt, /extensions, /tools - shows only
// the folded tool.
func TestMountAllAliasesHiddenFromInventory(t *testing.T) {
	ctx := &Context{Configs: map[string]any{}}
	p := fakeMountPlugin{
		name:     "tools",
		provides: []string{"tools"},
		mount: func(ctx *Context) error {
			ctx.ToolProviders = append(ctx.ToolProviders, fakeProvider{tools: map[string]core.Tool{
				"files":      {Description: "domain tool", Actions: []core.ToolAction{{Name: "write"}}},
				"files.read": {Description: "legacy read", AliasOf: "files"},
			}})
			return nil
		},
	}
	mountAllHelper(t, []Plugin{p}, ctx)
	if len(ctx.Infos) != 1 {
		t.Fatalf("infos = %d", len(ctx.Infos))
	}
	info := ctx.Infos[0]
	if info.Tools != 1 {
		t.Fatalf("Tools count = %d, want 1 (alias must not count)", info.Tools)
	}
	if len(info.ToolList) != 1 || info.ToolList[0].Name != "files" {
		t.Fatalf("ToolList = %+v, alias leaked", info.ToolList)
	}
	if len(info.ToolList[0].Actions) != 1 {
		t.Fatalf("Actions metadata not carried into ToolList: %+v", info.ToolList[0])
	}
}
