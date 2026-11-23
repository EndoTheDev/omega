// The inventory snapshot's live half: a capsule whose tools are runtime data (the mcp
// bridge) is re-stamped from its provider on every inventory re-read, and the dismount still
// removes exactly the entry the mount journal wrote.
package agent

import (
	"reflect"
	"testing"
	"time"

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
	names, ok := infoToolNames(ctx.Inventory(), "bridge")
	if !ok || !reflect.DeepEqual(names, []string{"alpha.echo", "beta.list"}) {
		t.Fatalf("mount inventory: %v (present %v), want [alpha.echo beta.list]", names, ok)
	}

	ag := NewFromContext(ctx, AgentOptions{})
	plugin.tools = map[string]core.Tool{"alpha.echo": {Description: "Echoes input"}}
	ag.RefreshExtensionInfos(ctx)

	names, _ = infoToolNames(ctx.Inventory(), "bridge")
	if !reflect.DeepEqual(names, []string{"alpha.echo"}) {
		t.Fatalf("the mounted inventory kept the stale tool: got %v, want [alpha.echo]", names)
	}
	names, _ = infoToolNames(ag.ExtensionInfos(), "bridge")
	if !reflect.DeepEqual(names, []string{"alpha.echo"}) {
		t.Fatalf("the agent snapshot kept the stale tool: got %v, want [alpha.echo]", names)
	}
	for _, info := range ctx.Inventory() {
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
	for _, info := range ctx.Inventory() {
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
	if _, present := infoToolNames(ctx.Inventory(), "bridge"); present {
		t.Fatal("dismount left the re-stamped inventory entry behind")
	}
}

// staticAndRuntimePlugin registers one static tool beside a runtime set: the mount journals
// the union, so a re-stamp has to rebuild that same union.
type staticAndRuntimePlugin struct {
	name    string
	runtime map[string]core.Tool
}

func (p *staticAndRuntimePlugin) Name() string       { return p.name }
func (p *staticAndRuntimePlugin) Provides() []string { return []string{"tools"} }
func (p *staticAndRuntimePlugin) Requires() []string { return nil }
func (p *staticAndRuntimePlugin) MountWithAPI(_ *Context, api *ExtensionAPI) error {
	if err := api.RegisterBuiltinTool(p.name+".static", core.Tool{Description: "a static tool"}); err != nil {
		return err
	}
	return api.RegisterRuntimeTools(p)
}
func (p *staticAndRuntimePlugin) Tools() map[string]core.Tool { return p.runtime }

// TestRefreshKeepsAMixedModulesStaticTools pins the merged stamp: a capsule that registers
// static and runtime tools keeps both rows through a re-stamp - the runtime half alone never
// replaces the module's entry.
func TestRefreshKeepsAMixedModulesStaticTools(t *testing.T) {
	plugin := &staticAndRuntimePlugin{
		name:    "mixed",
		runtime: map[string]core.Tool{"mixed.alpha": {Description: "runtime alpha"}},
	}
	ctx := &Context{}
	if _, err := MountAllAPI([]Plugin{plugin}, ctx, nil, nil); err != nil {
		t.Fatalf("mount: %v", err)
	}
	names, ok := infoToolNames(ctx.Inventory(), "mixed")
	if !ok || !reflect.DeepEqual(names, []string{"mixed.alpha", "mixed.static"}) {
		t.Fatalf("mount inventory: %v (present %v), want both rows", names, ok)
	}

	ag := NewFromContext(ctx, AgentOptions{})
	plugin.runtime = map[string]core.Tool{"mixed.beta": {Description: "runtime beta"}}
	ag.RefreshExtensionInfos(ctx)

	want := []string{"mixed.beta", "mixed.static"}
	for _, infos := range [][]core.ExtensionInfo{ctx.Inventory(), ag.ExtensionInfos()} {
		names, ok := infoToolNames(infos, "mixed")
		if !ok || !reflect.DeepEqual(names, want) {
			t.Fatalf("re-stamped inventory: %v (present %v), want %v", names, ok, want)
		}
	}
}

// TestInventoryUpdateCannotClobberAConcurrentMount pins the one-lock law: the journal lane and
// the re-stamp lane write the same inventory, so an entry mounted while a re-stamp holds its
// read-modify-write open waits for it and survives - never lost in the stale window.
func TestInventoryUpdateCannotClobberAConcurrentMount(t *testing.T) {
	ctx := &Context{}
	ctx.AddInfo(core.ExtensionInfo{Name: "mounted"})

	inside := make(chan struct{})
	release := make(chan struct{})
	updated := make(chan struct{})
	go func() {
		defer close(updated)
		ctx.UpdateInfos(func(infos []core.ExtensionInfo) []core.ExtensionInfo {
			close(inside)
			<-release
			return infos // a no-op re-stamp: the concurrent mount must still land
		})
	}()
	<-inside

	added := make(chan struct{})
	go func() {
		ctx.AddInfo(core.ExtensionInfo{Name: "mounted-while-restamping"})
		close(added)
	}()
	select {
	case <-added:
		t.Fatal("a mount landed inside the re-stamp's read-modify-write window")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	<-updated
	<-added

	infos := ctx.Inventory()
	if _, ok := infoToolNames(infos, "mounted-while-restamping"); !ok {
		t.Fatalf("the re-stamp clobbered an entry mounted against it: %+v", infos)
	}
	if _, ok := infoToolNames(infos, "mounted"); !ok {
		t.Fatalf("the re-stamp dropped the entry it read: %+v", infos)
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
