// The inventory snapshot's live half: a capsule whose tools are runtime data (the mcp
// bridge) is re-stamped from its provider on every inventory re-read, and the dismount still
// removes exactly the entry the mount journal wrote.
package agent

import (
	"reflect"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// runtimeToolPlugin serves a tool set the test swaps mid-mount, the shape the mcp capsule has.
type runtimeToolPlugin struct {
	name  string
	tools map[string]core.Tool
}

func (p *runtimeToolPlugin) Name() string       { return p.name }
func (p *runtimeToolPlugin) Provides() []string { return []string{"tools"} }
func (p *runtimeToolPlugin) Requires() []string { return nil }
func (p *runtimeToolPlugin) MountWithAPI(_ *Context, api *ExtensionAPI) error {
	return api.RegisterRuntimeTools(p)
}
func (p *runtimeToolPlugin) Tools() map[string]core.Tool { return p.tools }

func infoToolNames(infos []core.ExtensionInfo, module string) ([]string, bool) {
	for _, info := range infos {
		if info.Name != module {
			continue
		}
		names := make([]string, 0, len(info.ToolList))
		for _, t := range info.ToolList {
			names = append(names, t.Name)
		}
		return names, true
	}
	return nil, false
}

func TestRefreshExtensionInfosRestampsTheRuntimeEntry(t *testing.T) {
	plugin := &runtimeToolPlugin{name: "bridge", tools: map[string]core.Tool{
		"alpha.echo": {Description: "Echoes input\nSecond line."},
		"beta.list": {Description: "List things", Actions: []core.ToolAction{{Name: "list", Description: "list them"}}},
		"alias":     {AliasOf: "alpha.echo", Description: "an alias is never an inventory name"},
	}}
	ctx := &Context{}
	if _, err := MountAllAPI([]Plugin{plugin}, ctx, nil, nil); err != nil {
		t.Fatalf("mount: %v", err)
	}
	names, ok := infoToolNames(ctx.Infos, "bridge")
	if !ok || !reflect.DeepEqual(names, []string{"alpha.echo", "beta.list"}) {
		t.Fatalf("mount inventory: %v (present %v), want [alpha.echo beta.list]", names, ok)
	}

	ag := NewFromContext(ctx, AgentOptions{})
	plugin.tools = map[string]core.Tool{"alpha.echo": {Description: "Echoes input"}}
	ag.RefreshExtensionInfos(ctx)

	names, _ = infoToolNames(ctx.Infos, "bridge")
	if !reflect.DeepEqual(names, []string{"alpha.echo"}) {
		t.Fatalf("the mounted inventory kept the stale tool: got %v, want [alpha.echo]", names)
	}
	names, _ = infoToolNames(ag.ExtensionInfos(), "bridge")
	if !reflect.DeepEqual(names, []string{"alpha.echo"}) {
		t.Fatalf("the agent snapshot kept the stale tool: got %v, want [alpha.echo]", names)
	}
	for _, info := range ctx.Infos {
		if info.Name != "bridge" {
			continue
		}
		if info.Tools != 1 || info.ToolList[0].Description != "Echoes input" {
			t.Fatalf("re-stamped entry: %+v", info)
		}
	}

	// The section's own empty rule: a provider that serves nothing leaves the entry with no
	// tools, never a dropped module and never a block.
	plugin.tools = nil
	ag.RefreshExtensionInfos(ctx)
	for _, info := range ctx.Infos {
		if info.Name == "bridge" && (info.Tools != 0 || len(info.ToolList) != 0) {
			t.Fatalf("shrunk-to-zero entry: %+v", info)
		}
	}
}

// TestDismountRemovesTheRestampedEntry pins the journal undo against the re-stamp: the entry's
// tool list changed under the mount, so removal cannot key on the value the mount stamped.
func TestDismountRemovesTheRestampedEntry(t *testing.T) {
	plugin := &runtimeToolPlugin{name: "bridge", tools: map[string]core.Tool{"alpha.echo": {Description: "Echoes"}}}
	ctx := &Context{}
	reg, err := MountAllAPI([]Plugin{plugin}, ctx, nil, nil)
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	ag := NewFromContext(ctx, AgentOptions{})
	plugin.tools = map[string]core.Tool{"alpha.echo": {Description: "Echoes"}, "beta.list": {Description: "List"}}
	ag.RefreshExtensionInfos(ctx)

	if err := reg.Dismount(ctx, "bridge"); err != nil {
		t.Fatalf("dismount: %v", err)
	}
	if _, present := infoToolNames(ctx.Infos, "bridge"); present {
		t.Fatal("dismount left the re-stamped inventory entry behind")
	}
}

// TestToolInfosShape pins the one stamping shape: sorted names, whole description, actions
// carried, aliases dropped.
func TestToolInfosShape(t *testing.T) {
	list := ToolInfos(map[string]core.Tool{
		"z.read": {Description: "read\nwith a second line", Actions: []core.ToolAction{{Name: "read", Description: "read it"}}},
		"a.run":  {Description: "run"},
		"alias":  {AliasOf: "a.run"},
	})
	if len(list) != 2 {
		t.Fatalf("alias leaked into the inventory: %+v", list)
	}
	if list[0].Name != "a.run" || list[1].Name != "z.read" {
		t.Fatalf("names unsorted: %+v", list)
	}
	if list[1].Description != "read\nwith a second line" || len(list[1].Actions) != 1 {
		t.Fatalf("description or actions lost: %+v", list[1])
	}
	if got := ToolInfos(nil); len(got) != 0 {
		t.Fatalf("an empty tool set stamped %+v", got)
	}
}
