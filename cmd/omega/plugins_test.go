package main

import (
	"testing"

	"github.com/EndoTheDev/omega/engine/agent"
)

// TestBuildPlugins verifies buildPlugins returns all 18 in-process
// extensions, no error, and every plugin is non-nil, when given a
// default Config.
func TestBuildPlugins(t *testing.T) {
	cfg := DefaultConfig()

	plugins, err := buildPlugins(cfg)
	if err != nil {
		t.Fatalf("buildPlugins returned error: %v", err)
	}

	// TUI retired (t_0744eb8e): 19 in the web-module era; 18 since
	// v0.13 shell law 1 dissolved the web capsule into the channel
	// (t_f76ecdbe) after the earlier +kanban (t_33487fb6),
	// +notifications (t_5d9032d3), +verdict (t_2ecf9c51) +
	// continuous (t_72ecca67) = 19; the tui capsule leaves = 18.
	const want = 18
	if len(plugins) != want {
		t.Fatalf("buildPlugins returned %d plugins, want %d", len(plugins), want)
	}

	for i, p := range plugins {
		if p == nil {
			t.Errorf("plugins[%d] is nil", i)
		}
	}
}

// TestModuleOmissionDropsCommands gates the module-owned command law
// (card t_0744eb8e): a plugin left out of buildPlugins takes its
// commands out of the command table with it. Mount the default plugin
// set minus cron; no mounted module may serve /cron, and every module
// /command rides the RegisterCommand extension seam, so ctx.Commands
// never sees it. The webui half of the same law: without the cron
// plugin nothing provides the CronLister, so GET /cron/jobs serves an
// empty listing (covered by the http_channel inspector tests over a
// nil Cron seam).
func TestModuleOmissionDropsCommands(t *testing.T) {
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
	ctx := &agent.Context{Configs: agent.RouteConfigs(kept, cfg)}
	if _, err := agent.MountAllAPI(kept, ctx, nil, nil); err != nil {
		t.Fatalf("mount plugins: %v", err)
	}
	var cmds []string
	for _, c := range ctx.Commands {
		cmds = append(cmds, c.Name)
	}
	for _, gone := range []string{"/cron"} {
		for _, name := range cmds {
			if name == gone {
				t.Fatalf("unregistered module still serves %s: table = %v", gone, cmds)
			}
		}
	}
	// the mounted set keeps the other migrated commands; the law
	// is "omission removes", not "the table empties"
	for _, want := range []string{"/logs", "/verdict"} {
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
