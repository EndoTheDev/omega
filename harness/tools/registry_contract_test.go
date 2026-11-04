package tools

import (
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// The registry's contract (the shell-ring shrink wave 1): the tools
// capsule contributes NO tool provider - the built-in tool capsules
// (harness/files, harness/terminal's shell plugin) register through the
// same additive rail. The registry owns the seam claims, the /tools
// command, the ToolsList panel and the prompt section.

// TestPluginContract pins the plugin's identity + seams.
func TestPluginContract(t *testing.T) {
	p := NewPlugin()
	if p.Name() != "tools" {
		t.Fatalf("Name = %q, want %q", p.Name(), "tools")
	}
	if len(p.Requires()) != 0 {
		t.Fatalf("Requires = %v, want empty", p.Requires())
	}
	provides := p.Provides()
	if len(provides) != 1 || provides[0] != "prompt_sections" {
		t.Fatalf("Provides = %v, want [prompt_sections]", provides)
	}
}

// TestRegistryMountRegistersNoTools: the mount adds a prompt sectioner
// and a /tools command, and appends NO tool provider - the provider
// rail is the tool capsules' own.
func TestRegistryMountRegistersNoTools(t *testing.T) {
	p := NewPlugin()
	ctx := &core.Context{}
	if _, err := agent.MountAllAPI([]agent.Plugin{p}, ctx, nil, nil); err != nil {
		t.Fatalf("api mount: %v", err)
	}
	if len(ctx.ToolProviders) != 0 {
		t.Fatalf("ToolProviders len = %d, want 0 (the registry registers no tools)", len(ctx.ToolProviders))
	}
	if len(ctx.PromptSectioners) != 1 {
		t.Fatalf("PromptSectioners len = %d, want 1", len(ctx.PromptSectioners))
	}
}

var _ = core.Context{}