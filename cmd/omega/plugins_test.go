package main

import (
	"testing"

	"github.com/EndoTheDev/omega/agent"
)

// TestBuildPlugins verifies buildPlugins returns all 19 in-process
// extensions, no error, and every plugin is non-nil, when given a
// default Config.
func TestBuildPlugins(t *testing.T) {
	cfg := DefaultConfig()

	plugins, err := buildPlugins(cfg)
	if err != nil {
		t.Fatalf("buildPlugins returned error: %v", err)
	}

	const want = 19 // +kanban (t_33487fb6), +notifications (t_5d9032d3), +verdict (t_2ecf9c51)
	if len(plugins) != want {
		t.Fatalf("buildPlugins returned %d plugins, want %d", len(plugins), want)
	}

	for i, p := range plugins {
		if p == nil {
			t.Errorf("plugins[%d] is nil", i)
		}
	}
}

// TestModuleOmissionDropsTUICommands gates the module-owned TUI law
// (card t_88bb7dcf): a plugin left out of buildPlugins takes its TUI
// commands out of the command table with it. Mount the default plugin
// set minus cron; no mounted TUIProvider may serve /cron. The webui
// half of the same law: without the cron plugin nothing provides the
// CronLister, so GET /cron/jobs serves an empty listing (covered by
// the http_channel inspector tests over a nil Cron seam); the removal
// recipe in docs/tui.md walks both ends.
func TestModuleOmissionDropsTUICommands(t *testing.T) {
	cfg := DefaultConfig()
	plugins, err := buildPlugins(cfg)
	if err != nil {
		t.Fatalf("buildPlugins returned error: %v", err)
	}
	// omit the cron plugin, exactly what a build-tag gate does
	var kept []agent.Plugin
	for _, p := range plugins {
		if p.Name() != "cron" {
			kept = append(kept, p)
		}
	}
	ctx := &agent.Context{Configs: buildConfigs(cfg)}
	if err := agent.MountAll(kept, ctx); err != nil {
		t.Fatalf("mount plugins: %v", err)
	}
	var cmds []string
	for _, p := range ctx.TUIProviders {
		for _, c := range p.TUICommands() {
			cmds = append(cmds, c.Name)
		}
	}
	for _, gone := range []string{"/cron"} {
		for _, name := range cmds {
			if name == gone {
				t.Fatalf("unregistered module still serves %s: table = %v", gone, cmds)
			}
		}
	}
	// the mounted set keeps the other two migrated commands; the law
	// is "omission removes", not "the table empties"
	for _, want := range []string{"/files", "/logs"} {
		found := false
		for _, name := range cmds {
			if name == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("mounted table missing %s: %v", want, cmds)
		}
	}
}

// TestSignalContext verifies signalContext returns a non-nil context
// and cancel func, the context is not initially done, and cancel does
// not panic.
func TestSignalContext(t *testing.T) {
	ctx, cancel := signalContext()
	if ctx == nil {
		t.Fatal("signalContext returned nil context")
	}
	if cancel == nil {
		t.Fatal("signalContext returned nil cancel func")
	}

	select {
	case <-ctx.Done():
		t.Fatal("context is already done before cancel")
	default:
	}

	// Calling cancel must not panic.
	cancel()

	// After cancel, the context should become done.
	select {
	case <-ctx.Done():
	default:
		t.Fatal("context not done after cancel")
	}
}