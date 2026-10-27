package main

// The CLI homecoming proof (v0.12 phase D, grill Q6 - the module owns
// its argv): the binary's dispatch table is the mount registry, so
// `omega <module> cmd` works for a migrated capsule and the
// subcommand vanishes from the registry and the help when the module
// is absent. The two mounts are the proof, mirroring the
// removal-proof family in removal_test.go.

import (
	"os"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
)

func mountWithRegistry(t *testing.T, cfg Config, plugins []agent.Plugin) *agent.Registry {
	t.Helper()
	ctx := &agent.Context{
		CWD:     cwd(),
		Configs: agent.RouteConfigs(plugins, cfg),
	}
	reg, err := agent.MountAllAPI(plugins, ctx, nil, nil)
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	return reg
}

func fullPlugins(t *testing.T) []agent.Plugin {
	t.Helper()
	plugins, err := buildPlugins(DefaultConfig())
	if err != nil {
		t.Fatalf("buildPlugins: %v", err)
	}
	return plugins
}

// TestModuleCLIDispatch proves the migrated capsule's argv still works
// end to end: the registration exists in the registry, dispatch routes
// `omega kanban board current` to the module's own handler, and
// `omega kanban --help` prints the module's own help.
func TestModuleCLIDispatch(t *testing.T) {
	t.Setenv("OMEGA_HOME", t.TempDir())
	reg := mountWithRegistry(t, DefaultConfig(), fullPlugins(t))

	var sub *agent.Subcommand
	for _, s := range reg.Subcommands() {
		if s.Name == "kanban" {
			s := s
			sub = &s
		}
	}
	if sub == nil {
		t.Fatalf("FULL MOUNT: no kanban subcommand registered; got %+v", reg.Subcommands())
	}
	if sub.Module != "kanban" {
		t.Fatalf("subcommand %q registered by %q, want kanban", sub.Name, sub.Module)
	}
	t.Logf("PROOF registry: %q claimed by %q (%s)", sub.Name, sub.Module, sub.Summary)

	// omega kanban board current - the module parses its own argv
	out, err := captureStdout(t, func() error {
		handled, err := dispatchSubcommand(reg, "kanban", []string{"board", "current"})
		if !handled {
			t.Fatal("kanban subcommand not dispatched although registered")
		}
		return err
	})
	if err != nil || strings.TrimSpace(out) != "main" {
		t.Fatalf("omega kanban board current = %q err %v", out, err)
	}
	t.Logf("PROOF dispatch: omega kanban board current -> %q", strings.TrimSpace(out))

	// omega kanban --help prints the module's own help
	out, err = captureStdout(t, func() error {
		handled, err := dispatchSubcommand(reg, "kanban", []string{"--help"})
		if !handled {
			t.Fatal("kanban --help not dispatched")
		}
		return err
	})
	if err != nil || !strings.Contains(out, "omega kanban - the task board registry") {
		t.Fatalf("omega kanban --help: %q err %v", out, err)
	}
	t.Log("PROOF help: omega kanban --help printed the module's own help")

	// the module owns the unknown subcommands of its own name too (Q6):
	// the binary routed, the module rejected
	if _, err := dispatchSubcommand(reg, "kanban", []string{"bogus"}); err == nil {
		t.Fatal("unknown kanban subcommand accepted")
	}
}

// TestModuleCLISubcommandVanishes is the vanish half: with the kanban
// module absent from the plugin set, the registry carries no kanban
// subcommand, dispatch falls through, and the help listing (the same
// introspection) prints no module section.
func TestModuleCLISubcommandVanishes(t *testing.T) {
	t.Setenv("OMEGA_HOME", t.TempDir())
	plugins := fullPlugins(t)
	var removed []agent.Plugin
	for _, p := range plugins {
		if p.Name() == "kanban" {
			continue
		}
		removed = append(removed, p)
	}
	reg := mountWithRegistry(t, DefaultConfig(), removed)

	for _, s := range reg.Subcommands() {
		if s.Name == "kanban" {
			t.Fatalf("REMOVED MOUNT: kanban subcommand still registered (%s)", s.Summary)
		}
	}
	if handled, err := dispatchSubcommand(reg, "kanban", []string{"board", "current"}); handled || err != nil {
		t.Fatalf("REMOVED MOUNT: kanban dispatched (handled=%v err=%v)", handled, err)
	}
	t.Log("PROOF vanish: kanban absent from the registry, dispatch falls through")

	// the help listing is the same introspection: an empty module
	// table prints no section, a removed module's line is gone
	out, _ := captureStdout(t, func() error {
		printModuleSubcommands(reg.Subcommands(), os.Stdout)
		return nil
	})
	if out != "" {
		t.Fatalf("REMOVED MOUNT: help still lists module subcommands: %q", out)
	}
	t.Log("PROOF help-vanish: the module section is gone from the help listing")
}

// TestHelpListsModuleSubcommands pins the settings-list law end to
// end: printHelp's module section reads the registry, one line per
// subcommand, and the static help text carries no hard-coded module
// noun (the registry owns the list).
func TestHelpListsModuleSubcommands(t *testing.T) {
	t.Setenv("OMEGA_HOME", t.TempDir())
	out, err := captureStdout(t, func() error {
		printHelp([]string{})
		return nil
	})
	if err != nil {
		t.Fatalf("help: %v", err)
	}
	if err != nil || !strings.Contains(out, "Module subcommands") {
		t.Fatalf("help missing the module section: %q", out)
	}
	if !strings.Contains(out, "omega kanban") || !strings.Contains(out, "the task board registry (see: omega kanban --help)") {
		t.Fatalf("help missing the kanban entry: %q", out)
	}
	// the introspection, not a literal: the static const has no kanban
	if strings.Contains(helpText, "omega kanban") {
		t.Fatal("static help hard-codes a module noun - the list must be registry introspection")
	}
}
