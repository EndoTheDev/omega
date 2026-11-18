package cron

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// Plugin adapts the cron Runner into omega's plugin system.
type Plugin struct {
	cfg     *Config
	home    string
	runner  *Runner  // the mounted scheduler; nil = not mounted
	capsule *capsule // the canvas surface; nil = not mounted
}

// Compile-time assertion that *Plugin implements agent.Plugin.
var _ agent.Plugin = (*Plugin)(nil)

// NewPlugin creates the cron plugin.
func NewPlugin() *Plugin { return &Plugin{} }

// NewPluginWithConfig creates the plugin from an explicit config and home (tests.
func NewPluginWithConfig(cfg *Config, home string) *Plugin {
	return &Plugin{cfg: cfg, home: home}
}

// Name returns the plugin name shown in the /extensions listing.
func (p *Plugin) Name() string { return "cron" }

// Description implements agent.Describer.
func (p *Plugin) Description() string {
	return "cron: schedules omega prompts on cron expressions, one agent turn per fire"
}

// Provides lists the seams this plugin mounts.
func (p *Plugin) ConfigKey() string { return "" }

func (p *Plugin) Provides() []string { return []string{"cron"} }

// Requires: notifications. The runner's notifier arrives through CronDeps.Ctx at Start
// (host wiring, post-mount) - the edge is the presence declaration: with notifications
// toggled off, cron must fail the boot (Q3), not silently drop its finished-things
// records.
func (p *Plugin) Requires() []string { return []string{"notifications"} }

// MountWithAPI loads crons.yaml and registers every face.
func (p *Plugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	home := p.home
	if home == "" {
		home = cronHome()
	}
	cfg := p.cfg
	if cfg == nil {
		var err error
		// One cron module for every home.
		cfg, err = LoadAllConfigs(home)
		if err != nil {
			return err
		}
	}
	p.cfg = cfg
	p.home = home
	r := NewRunner(cfg, home)
	p.runner = r
	if err := api.Offer("cron", r); err != nil {
		return err
	}
	// The goroutine fence (v0.12 E1). Runner.Start blocks until cancellation or Stop, so
	// the handle arms it on its own goroutine (the fence Start law: Start returns
	// promptly) and Stop ends it.
	if err := api.RegisterLifecycle(
		"runner",
		func(ctx context.Context, deps core.StartDeps) error {
			go func() {
				var ag *agent.Agent
				var st agent.StoreProvider
				ag, _ = deps.Agent.(*agent.Agent)
				st, _ = deps.Store.(agent.StoreProvider)
				if err := r.Start(ctx, runnerDeps{Ctx: deps.Ctx, Agent: ag, Store: st}); err != nil {
					fmt.Fprintf(os.Stderr, "omega: cron runner: %v\n", err)
				}
			}()
			return nil
		},
		func() error { return r.Stop() },
	); err != nil {
		return err
	}
	if err := api.RegisterCommand("/cron", "list cron jobs (read-only)", []string{}, cronHandler(p)); err != nil {
		return err
	}
	// The crons subcommand: the terminal face of the same editor the Cron tab writes. The
	// module owns its argv (v0.12 phase D Q6); every write rides SaveJob/DeleteJob above,
	// so `omega crons add` and a tab save land byte-identically.
	if err := api.RegisterSubcommand("crons", cronsSummary, cmdCrons(p)); err != nil {
		return err
	}
	// The per-job toggle route rides the mcp toggle route's reasoning
	// (harness/mcp/plugin.go). The route always mounts - the empty tab's toggle is refused
	// at the wire (501), never a module-rooted miss.
	if err := api.RegisterRoute("POST /cron/toggle", p.handleToggle); err != nil {
		return err
	}
	// The job editor routes: POST /cron/save upserts one full job definition into the.
	// Both answer the fresh job set - the one-response contract the toggle carries.
	if err := api.RegisterRoute("POST /cron/save", p.handleSaveJob); err != nil {
		return err
	}
	if err := api.RegisterRoute("POST /cron/delete", p.handleDeleteJob); err != nil {
		return err
	}
	// Capsule law: the module ships its view-model with its scheduler.
	p.capsule = newCapsule(r)
	r.panels = p.capsule
	return api.RegisterPanelProvider(p.capsule)
}

// homeFile returns the crons.yaml path inside home.
func homeFile(home string) string {
	root := filepath.Join(home, stateFileName)
	if _, err := os.Stat(root); err == nil {
		return root
	}
	return filepath.Join(home, "config", stateFileName)
}

// Toggleable marks this capsule as a module capsule (v0.12 E1, the tier law).
func (p *Plugin) Toggleable() bool { return true }
