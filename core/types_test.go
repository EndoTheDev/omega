package core

// types_test.go: the registry entry's enable flag (card t_441deb04) -
// the flag is a per-entry stamp, never stored on the mount inventory,
// so a live fold change re-stamps without a remount and the inventory
// the capsules hold never mutates under them.

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
