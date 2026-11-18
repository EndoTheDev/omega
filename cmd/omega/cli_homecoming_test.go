package main

// The CLI homecoming proof (v0.12 phase D, grill Q6 - the module owns its argv).

import (
	"os"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
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
	dismountCleanup(t, reg, ctx)
	return reg
}

// dismountCleanup registers the teardown of a bare test mount.
func dismountCleanup(t *testing.T, reg *agent.Registry, ctx *core.Context) {
	t.Helper()
	t.Cleanup(func() {
		_ = agent.DismountAll(reg, ctx)
	})
}

func fullPlugins(t *testing.T) []agent.Plugin {
	t.Helper()
	plugins, err := buildPlugins(DefaultConfig())
	if err != nil {
		t.Fatalf("buildPlugins: %v", err)
	}
	return plugins
}

// TestModuleCLIDispatch proves the migrated capsule's argv still works end to end.
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

	// the module owns the unknown subcommands of its own name too (Q6).
	if _, err := dispatchSubcommand(reg, "kanban", []string{"bogus"}); err == nil {
		t.Fatal("unknown kanban subcommand accepted")
	}
}

// TestModuleCLISubcommandVanishes is the vanish half.
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

	// the help listing is the same introspection: a removed module's line is gone.
	out, _ := captureStdout(t, func() error {
		printModuleSubcommands(reg.Subcommands(), os.Stdout)
		return nil
	})
	if strings.Contains(out, "kanban") {
		t.Fatalf("REMOVED MOUNT: help still lists the kanban subcommand: %q", out)
	}
	t.Log("PROOF help-vanish: the kanban line is gone from the help listing")
}

// TestHelpListsModuleSubcommands pins the settings-list law end to end.
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
	if strings.Contains(helpText, "omega kanban") {
		t.Fatal("static help hard-codes a module noun - the list must be registry introspection")
	}
}

// TestModuleHelpFromRealArgv routes the subcommand argv through run.
func TestModuleHelpFromRealArgv(t *testing.T) {
	t.Setenv("OMEGA_HOME", t.TempDir())

	// omega kanban --help == omega kanban: the module's own help, and never the root help.
	moduleHelp := func(t *testing.T, args []string, marker string) {
		t.Helper()
		out, err := captureStdout(t, func() error { return run(args) })
		if err != nil {
			t.Fatalf("omega %v: %v", args, err)
		}
		if !strings.Contains(out, marker) {
			t.Fatalf("omega %v did not print the module help (%s): %q", args, marker, out)
		}
		if strings.Contains(out, "omega - a Go event-stream agent") {
			t.Fatalf("omega %v printed the ROOT help: %q", args, out)
		}
	}

	moduleHelp(t, []string{"kanban", "--help"}, "omega kanban - the task board registry")
	t.Log("PROOF argv: omega kanban --help printed the module's own help")

	moduleHelp(t, []string{"curator", "--help"}, "omega curator - the weekly skills curator pass")
	t.Log("PROOF argv: omega curator --help printed the module's own help")

	out, err := captureStdout(t, func() error { return run([]string{"--help"}) })
	if err != nil || !strings.Contains(out, "omega - a Go event-stream agent") {
		t.Fatalf("omega --help: %q err %v", out, err)
	}
	out, err = captureStdout(t, func() error { return run([]string{"--version"}) })
	if err != nil || !strings.HasPrefix(strings.TrimSpace(out), "omega ") {
		t.Fatalf("omega --version: %q err %v", out, err)
	}
}
