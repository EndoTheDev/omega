package tools

import (
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// The registry's contract (the shell-ring shrink wave 1, extended by
// card t_db98ebfe): the tools capsule ships the grep + glob search
// tools itself and contributes the same additive tools rail every
// other tool capsule (harness/files, harness/terminal's shell plugin)
// registers through. The registry also owns the seam claims, the
// /tools command, the ToolsList panel and the prompt section.

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
	want := []string{"prompt_sections", "tools"}
	if len(provides) != len(want) {
		t.Fatalf("Provides = %v, want %v", provides, want)
	}
	for i, w := range want {
		if provides[i] != w {
			t.Fatalf("Provides = %v, want %v", provides, want)
		}
	}
}

// TestRegistryMountRegistersSearchTools: the mount registers the
// grep + glob provider (one ToolProvider, two tools) plus a prompt
// sectioner.
func TestRegistryMountRegistersSearchTools(t *testing.T) {
	p := NewPlugin()
	ctx := &core.Context{}
	if _, err := agent.MountAllAPI([]agent.Plugin{p}, ctx, nil, nil); err != nil {
		t.Fatalf("api mount: %v", err)
	}
	if len(ctx.ToolProviders) != 1 {
		t.Fatalf("ToolProviders len = %d, want 1 (the search provider)", len(ctx.ToolProviders))
	}
	tools := ctx.ToolProviders[0].Tools()
	if len(tools) != 2 {
		t.Fatalf("search provider ships %d tools, want 2 (grep + glob)", len(tools))
	}
	for _, name := range []string{"grep", "glob"} {
		if tools[name].Run == nil {
			t.Fatalf("search tool %q missing from the mounted registry", name)
		}
	}
	if len(ctx.PromptSectioners) != 1 {
		t.Fatalf("PromptSectioners len = %d, want 1", len(ctx.PromptSectioners))
	}
}

// TestSearchToolsShape pins the two single-purpose tools: names,
// descriptions and that grep and glob are one-verb tools (no
// sub-action split, the settled cut of card t_db98ebfe).
func TestSearchToolsShape(t *testing.T) {
	p := NewSearchProvider(nil)
	tools := p.Tools()
	for _, name := range []string{"grep", "glob"} {
		tl, ok := tools[name]
		if !ok {
			t.Fatalf("tool %q missing from the provider map", name)
		}
		if tl.Description == "" {
			t.Fatalf("tool %q has no description", name)
		}
		if tl.Run == nil {
			t.Fatalf("tool %q has no run func", name)
		}
		if len(tl.Actions) != 0 {
			t.Fatalf("tool %q carries sub-actions; the settled cut is one verb per tool", name)
		}
		params, ok := tl.Parameters["properties"].(map[string]any)
		if !ok {
			t.Fatalf("tool %q has no properties schema", name)
		}
		if _, ok := params["pattern"]; !ok {
			t.Fatalf("tool %q is missing its pattern parameter", name)
		}
	}
}

var _ = core.Context{}
