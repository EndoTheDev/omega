package main

// tools_test.go: the `omega tools` verb grammar + the one-write law
// (card t_441deb04). The toggle's persistence half is exercised against
// a real config.yaml through writeDisabled - the same writer the PUT
// /disabled/tools route rides - so the test asserts landed file truth
// (the fold a boot would read), never a snapshot of internal state.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

func TestToggleToolFold(t *testing.T) {
	// disable appends and sorts
	next, changed := toggleToolFold([]string{"files.write"}, "files.grep", false)
	if !changed {
		t.Fatal("disable of a new name reported no change")
	}
	if strings.Join(next, ",") != "files.grep,files.write" {
		t.Fatalf("fold = %v, want sorted [files.grep files.write]", next)
	}
	// disable of an already-disabled name is an honest no-op
	if _, changed := toggleToolFold([]string{"files.grep"}, "files.grep", false); changed {
		t.Fatal("double disable reported a change")
	}
	// enable removes
	next, changed = toggleToolFold([]string{"files.grep", "files.write"}, "files.grep", true)
	if !changed || strings.Join(next, ",") != "files.write" {
		t.Fatalf("enable fold = %v (changed %v), want [files.write]", next, changed)
	}
	// enable of a name that is not folded is a no-op
	if _, changed := toggleToolFold([]string{"files.write"}, "files.grep", true); changed {
		t.Fatal("enable of an unfolded name reported a change")
	}
	// the last removal empties the namespace
	next, changed = toggleToolFold([]string{"files.write"}, "files.write", true)
	if !changed || len(next) != 0 {
		t.Fatalf("last enable = %v (changed %v), want empty", next, changed)
	}
}

func TestToolRowsStampStates(t *testing.T) {
	infos := []core.ExtensionInfo{
		{Name: "files", ToolList: []core.ToolInfo{
			{Name: "files.write", Description: "write a file"},
			{Name: "files.read", Description: "read a file"},
		}},
		{Name: "tools", ToolList: []core.ToolInfo{
			{Name: "files.grep", Description: "search file contents"},
		}},
	}
	rows := toolRows(infos, []string{"files.write"})
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3 (a disabled tool keeps its row)", len(rows))
	}
	// name-sorted, regardless of extension grouping
	got := []string{rows[0].Name, rows[1].Name, rows[2].Name}
	want := []string{"files.grep", "files.read", "files.write"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
	if rows[2].State != "disabled" || !rows[2].Disabled {
		t.Fatalf("files.write row = %+v, want disabled", rows[2])
	}
	if rows[1].State != "enabled" || rows[1].Disabled {
		t.Fatalf("files.read row = %+v, want enabled", rows[1])
	}
	if rows[0].Origin != "tools" {
		t.Fatalf("origin = %q, want the owning extension", rows[0].Origin)
	}
}

func TestToolBrief(t *testing.T) {
	if got := toolBrief("first line\nsecond line"); got != "first line" {
		t.Fatalf("toolBrief = %q, want the first line", got)
	}
	if got := toolBrief("   \n  spaced  "); got != "spaced" {
		t.Fatalf("toolBrief = %q, want the trimmed first non-empty line", got)
	}
	long := strings.Repeat("x", 80)
	if got := toolBrief(long); len(got) != 60 {
		t.Fatalf("toolBrief len = %d, want 60 (57 + ...)", len(got))
	}
}

// TestPersistToolFoldRoundTrip is the CLI's round-trip receipt in
// miniature: disable lands in the config.yaml fold a boot reads, a
// reload sees it, enable takes it back out, and the surrounding
// document (comments, other keys) survives the write.
func TestPersistToolFoldRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	body := "# the operator's config\nprovider:\n  model_name: test-model\n# trailing comment stays\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	changed, err := persistToolFold(path, DisabledNamespaces{}, "files.write", false)
	if err != nil {
		t.Fatalf("disable: %v", err)
	}
	if !changed {
		t.Fatal("disable reported no change")
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("reload after disable: %v", err)
	}
	if strings.Join(cfg.Disabled.Tools, ",") != "files.write" {
		t.Fatalf("landed fold = %v, want [files.write]", cfg.Disabled.Tools)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, keep := range []string{"# the operator's config", "# trailing comment stays", "model_name: test-model"} {
		if !strings.Contains(string(data), keep) {
			t.Fatalf("write lost %q:\n%s", keep, data)
		}
	}

	// a second disable of the same name writes nothing
	before := string(data)
	changed, err = persistToolFold(path, cfg.Disabled, "files.write", false)
	if err != nil || changed {
		t.Fatalf("double disable = changed %v err %v, want a no-op", changed, err)
	}
	if after, _ := os.ReadFile(path); string(after) != before {
		t.Fatalf("no-op disable rewrote the file:\n%s", after)
	}

	// enable takes it back out; the empty namespace leaves no key
	changed, err = persistToolFold(path, cfg.Disabled, "files.write", true)
	if err != nil || !changed {
		t.Fatalf("enable = changed %v err %v", changed, err)
	}
	cfg, err = LoadConfig(path)
	if err != nil {
		t.Fatalf("reload after enable: %v", err)
	}
	if len(cfg.Disabled.Tools) != 0 {
		t.Fatalf("fold survived the enable: %v", cfg.Disabled.Tools)
	}
}

// TestPersistToolFoldKeepsSiblingNamespaces pins the fold's sibling law:
// a tools write must not eat the skills/agents/modules namespaces the
// same config.yaml key carries.
func TestPersistToolFoldKeepsSiblingNamespaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("provider:\n  model_name: test-model\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fold := DisabledNamespaces{
		Modules:    []string{"kanban"},
		SkillNames: []string{"noisy"},
		Agents:     []string{"scout"},
	}
	if _, err := persistToolFold(path, fold, "files.write", false); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(cfg.Disabled.Modules, ",") != "kanban" ||
		strings.Join(cfg.Disabled.SkillNames, ",") != "noisy" ||
		strings.Join(cfg.Disabled.Agents, ",") != "scout" ||
		strings.Join(cfg.Disabled.Tools, ",") != "files.write" {
		t.Fatalf("fold = %+v, want every namespace intact", cfg.Disabled)
	}
}

// TestCmdToolsRefusals pins the loud-refusal lanes: a missing name, an
// extra argument, a flag where the name belongs, and an unknown verb.
// None of them may touch config.yaml.
func TestCmdToolsRefusals(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	path := filepath.Join(home, "config.yaml")
	body := "provider:\n  model_name: test-model\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"no name", []string{"disable"}, "usage: omega tools disable <name>"},
		{"extra args", []string{"enable", "a", "b"}, "takes one tool name"},
		{"flag as name", []string{"disable", "--force"}, "is a flag, not a tool name"},
		{"unknown verb", []string{"frobnicate"}, `unknown tools subcommand "frobnicate"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := cmdTools(tc.args)
			if err == nil {
				t.Fatalf("args %v: no error", tc.args)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %q, want %q", err, tc.want)
			}
		})
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != body {
		t.Fatalf("a refusal touched config.yaml: %q (%v)", got, err)
	}
}

// TestCmdToolsListRejectsTrailingArgs pins the same law the crons list
// nit called out: list takes no positionals, it does not silently
// ignore them.
func TestCmdToolsListRejectsTrailingArgs(t *testing.T) {
	if err := cmdTools([]string{"list", "stray"}); err == nil ||
		!strings.Contains(err.Error(), "takes no arguments") {
		t.Fatalf("err = %v, want a trailing-arg refusal", err)
	}
}
