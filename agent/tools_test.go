package agent

import (
	"testing"
)

// Built-in tool implementations have moved to the core-tools extension
// (bin/extensions/core-tools/). These tests verify the agent core's
// tool registry is empty — all tools come from extensions.

func TestNewRegistryEmpty(t *testing.T) {
	tools := NewRegistry()
	if len(tools) != 0 {
		t.Fatalf("NewRegistry returned %d tools, want 0 (built-ins moved to core-tools extension)", len(tools))
	}
}