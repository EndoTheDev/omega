package agent_loop

import (
	"reflect"
	"testing"

	"github.com/EndoTheDev/omega/agent"
)

// TestToolSchemasActionEnumDerivesFromMetadata pins the drift gate at
// the provider boundary: toolSchemas renders the action enum from the
// Tool's Actions metadata alone. A schema that hand-maintains a stale
// enum (here: "purge", not declared in Actions) is corrected, not
// trusted.
func TestToolSchemasActionEnumDerivesFromMetadata(t *testing.T) {
	tools := map[string]agent.Tool{
		"memory": {
			Description: "domain tool",
			Actions:     []agent.ToolAction{{Name: "add"}, {Name: "replace"}, {Name: "remove"}},
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action": map[string]any{
						"type": "string",
						"enum": []string{"add", "replace", "remove", "purge"},
					},
				},
			},
		},
		"shell.run": {
			Description: "plain tool",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{"command": map[string]any{"type": "string"}},
			},
		},
	}
	schemas := toolSchemas(tools)
	if len(schemas) != 2 {
		t.Fatalf("schemas = %d", len(schemas))
	}
	for _, s := range schemas {
		if s.Name == "shell.run" {
			continue
		}
		props := s.Parameters["properties"].(map[string]any)
		action := props["action"].(map[string]any)
		if !reflect.DeepEqual(action["enum"], []string{"add", "replace", "remove"}) {
			t.Fatalf("memory action enum %v not derived from Actions metadata", action["enum"])
		}
	}
}