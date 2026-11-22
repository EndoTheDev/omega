package core

// types_test.go: the registry entry's enable flag - the flag is a per-entry stamp,
// never stored on the mount inventory, so a live fold change re-stamps without a
// remount and the inventory the capsules hold never mutates under them.

import "testing"

func TestStampDisabledToolsMarksNamedEntries(t *testing.T) {
	infos := []ExtensionInfo{
		{Name: "files", ToolList: []ToolInfo{
			{Name: "files.read", Description: "read"},
			{Name: "files.write", Description: "write"},
		}},
		{Name: "empty"},
	}
	out := StampDisabledTools(infos, []string{"files.write"})
	if len(out) != len(infos) {
		t.Fatalf("extensions = %d, want %d", len(out), len(infos))
	}
	if out[0].ToolList[0].Disabled {
		t.Fatalf("files.read stamped disabled")
	}
	if !out[0].ToolList[1].Disabled {
		t.Fatalf("files.write not stamped disabled")
	}
}

func TestStampDisabledToolsNeverMutatesInventory(t *testing.T) {
	infos := []ExtensionInfo{{Name: "files", ToolList: []ToolInfo{{Name: "files.read"}}}}
	_ = StampDisabledTools(infos, []string{"files.read"})
	if infos[0].ToolList[0].Disabled {
		t.Fatalf("the mount inventory was mutated - a live toggle would leak into the next read")
	}
}

func TestStampDisabledToolsDefaultsEnabled(t *testing.T) {
	infos := []ExtensionInfo{{Name: "files", ToolList: []ToolInfo{{Name: "files.read"}}}}
	for _, out := range [][]ExtensionInfo{
		StampDisabledTools(infos, nil),
		StampDisabledTools(infos, []string{}),
		StampDisabledTools(infos, []string{"some.other.tool"}),
	} {
		if out[0].ToolList[0].Disabled {
			t.Fatalf("an unnamed entry came back disabled - the default must stay ON")
		}
	}
}

// TestToolEnabledJudgesFoldAndInventory pins the prompt-side reading of "on the wire": a
// tool is offered when the inventory carries it and the fold does not name it. An absent
// inventory is unknown, never empty, so a caller-less build keeps its prompt.
func TestToolEnabledJudgesFoldAndInventory(t *testing.T) {
	infos := []ExtensionInfo{{Name: "tools", ToolList: []ToolInfo{
		{Name: "files.read"},
		{Name: "shell.run"},
	}}}
	opts := PromptBuildOptions{Extensions: infos, DisabledTools: []string{"shell.run"}}

	if !opts.ToolEnabled("files.read") {
		t.Error("files.read is in the inventory and unfolded, want enabled")
	}
	if opts.ToolEnabled("shell.run") {
		t.Error("shell.run is folded, want disabled")
	}
	if opts.ToolEnabled("kanban.list") {
		t.Error("kanban.list is in no inventory, want disabled")
	}
	if !opts.ToolEnabled("") {
		t.Error("an empty name gates nothing, want enabled")
	}
	if !opts.ToolsEnabled("shell.run", "files.read") {
		t.Error("one of the family is enabled, want true")
	}
	if opts.ToolsEnabled("shell.run", "kanban.list") {
		t.Error("no member of the family is enabled, want false")
	}

	unknown := PromptBuildOptions{DisabledTools: []string{"shell.run"}}
	if !unknown.ToolEnabled("kanban.list") {
		t.Error("an unsupplied inventory is unknown, want the name judged by the fold alone")
	}
}

func TestHasToolsReadsTheStampedInventory(t *testing.T) {
	infos := []ExtensionInfo{{Name: "tools", ToolList: []ToolInfo{
		{Name: "files.read"},
		{Name: "shell.run"},
	}}}
	if !(PromptBuildOptions{}).HasTools() {
		t.Error("an unsupplied inventory is unknown, want HasTools true (no posture change)")
	}
	if !(PromptBuildOptions{Extensions: infos}).HasTools() {
		t.Error("two enabled tools, want HasTools true")
	}
	if (PromptBuildOptions{Extensions: StampDisabledTools(infos, []string{"files.read", "shell.run"})}).HasTools() {
		t.Error("every tool folded, want HasTools false")
	}
	if (PromptBuildOptions{Extensions: []ExtensionInfo{{Name: "empty"}}}).HasTools() {
		t.Error("an inventory carrying no tools at all, want HasTools false")
	}
}

func TestToolNamesIsTheWholeInventory(t *testing.T) {
	infos := []ExtensionInfo{
		{Name: "tools", ToolList: []ToolInfo{{Name: "shell.run"}, {Name: "files.read"}}},
		{Name: "empty"},
	}
	got := ToolNames(infos)
	want := []string{"files.read", "shell.run"}
	if len(got) != len(want) {
		t.Fatalf("ToolNames = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ToolNames = %v, want %v (sorted)", got, want)
		}
	}
	if ToolNames(nil) != nil {
		t.Error("ToolNames(nil) must stay nil - an empty fold, not an allocation")
	}
}
