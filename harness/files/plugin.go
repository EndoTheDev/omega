// The files capsule's plugin: the consolidated files tool (the
// tools-seam contributor) and the explorer's REST face (the module
// REST rail), one domain, one capsule - the shell-ring shrink wave 1
// (ADR Workshop/omega-shell-ring-shrink.md). Toggleable: the Modules
// fold may name it; disabling it unmounts the tool AND the routes -
// the explorer is the file tools wearing a second face.
package files

import (
	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/datadir"
	"github.com/EndoTheDev/omega/harness/store"
)

// Config is the capsule's config section (ConfigKey "files"). The
// default_workspace knob rides cfg.Server.DefaultWorkspace; the host's
// ConfigSection copies it here. Hot saves push through
// SetDefaultWorkspace (the "files" service), not a remount.
type Config struct {
	DefaultWorkspace string
}

// Plugin adapts the file domain into the agent.Plugin interface.
type Plugin struct {
	routes *Routes
}

// Name returns the capsule name (the modules registry row, every
// "files"-owned registration's tenant).
func (p *Plugin) Name() string { return "files" }

// Description implements agent.Describer.
func (p *Plugin) Description() string {
	return "Files: the consolidated file tool (read/write/edit, atomic batch) and the webui's file explorer"
}

// ConfigKey names the config section this plugin reads from
// ctx.Configs (see agent.Describer).
func (p *Plugin) ConfigKey() string { return "files" }

// Provides lists seam names this plugin mounts: the additive tools
// rail. Multiple plugins can contribute tool providers without
// conflict.
func (p *Plugin) Provides() []string { return []string{"tools"} }

// Requires lists seam names that must be mounted first. The files
// tool needs nothing - the store seam is resolved per request (the
// root resolution law reads the workspaces live), never pinned.
func (p *Plugin) Requires() []string { return nil }

// Toggleable marks the files capsule a module capsule: the config.yaml
// disabled: list may name it. Removing (or disabling) it unmounts the
// tool and the explorer routes together.
func (p *Plugin) Toggleable() bool { return true }

// MountWithAPI implements agent.APIPlugin. Registers the file tool
// (+ the legacy alias shims), the five /files explorer routes, and
// pushes the mounted config's default_workspace into the routes. The
// db-door guard (ENDO'S LAW 2026-10-18) is built from the store
// config.
func (p *Plugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	cfg := Config{}
	if c, ok := ctx.Configs["files"].(Config); ok {
		cfg = c
	}
	// The db-door guard needs the instance db's path (the store
	// mount's config); the empty-path fallback is the standard layout
	// (datadir). Nil and zero-value guards are pass-through.
	var guard *datadir.Guard
	if c, ok := ctx.Configs["store"].(store.Config); ok {
		guard = datadir.NewGuard(c.DBPath)
	} else {
		guard = datadir.NewGuard("")
	}
	provider := &FileProvider{guard: guard}
	p.routes = newRoutes(ctx)

	steps := []func() error{
		func() error {
			for name, t := range provider.Tools() {
				if err := api.RegisterTool(name, t); err != nil {
					return err
				}
			}
			return nil
		},
		// The explorer routes are the file tools wearing a second
		// face - they mount (and die) with the capsule.
		func() error { return api.RegisterRoute("/files/browse", p.routes.handleFilesBrowse) },
		func() error { return api.RegisterRoute("/files/read", p.routes.handleFilesRead) },
		func() error { return api.RegisterRoute("/files/raw", p.routes.handleFilesRaw) },
		func() error { return api.RegisterRoute("POST /files/write", p.routes.handleFilesWrite) },
		func() error { return api.RegisterRoute("POST /files/mkdir", p.routes.handleFilesMkdir) },
		// The default_workspace knob lands at mount; the host's
		// settings hot-apply pushes later changes through
		// SetDefaultWorkspace (a bare func hook the host threads,
		// the continuous capsule's setAgents shape - no service
		// registry key, the namespaces stay closed).
		func() error { p.routes.SetDefaultWorkspace(cfg.DefaultWorkspace); return nil },
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}
	return nil
}

// NewPlugin creates the files capsule plugin.
func NewPlugin() *Plugin { return &Plugin{} }

// SetDefaultWorkspace is the host's hot-apply hook: the settings pane
// (and the config watcher) push a changed default_workspace through it
// without a remount.
func (p *Plugin) SetDefaultWorkspace(ws string) {
	if p.routes != nil {
		p.routes.SetDefaultWorkspace(ws)
	}
}
