// The files capsule's plugin.
package files

import (
	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/datadir"
	"github.com/EndoTheDev/omega/harness/store"
)

// Config is the capsule's config section (ConfigKey "files").
type Config struct {
	DefaultWorkspace string
}

// Plugin adapts the file domain into the agent.Plugin interface.
type Plugin struct {
	routes *Routes
}

// Name returns the capsule name.
func (p *Plugin) Name() string { return "files" }

// Description implements agent.Describer.
func (p *Plugin) Description() string {
	return "Files: the consolidated file tool (read/write/edit, atomic batch) and the webui's file explorer"
}

// ConfigKey names the config section this plugin reads from ctx.Configs.
func (p *Plugin) ConfigKey() string { return "files" }

// Provides lists seam names this plugin mounts: the additive tools rail.
func (p *Plugin) Provides() []string { return []string{"tools"} }

// Requires lists seam names that must be mounted first. The files
// tool needs nothing - the store seam is resolved per request (the
// root resolution law reads the workspaces live), never pinned.
func (p *Plugin) Requires() []string { return nil }

// Toggleable marks the files capsule a module capsule.
func (p *Plugin) Toggleable() bool { return true }

// MountWithAPI implements agent.APIPlugin.
func (p *Plugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	cfg := Config{}
	if c, ok := ctx.Configs["files"].(Config); ok {
		cfg = c
	}
	// The db-door guard needs the instance db's path (the store mount's config). Nil and
	// zero-value guards are pass-through.
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
		func() error { return api.RegisterRoute("/files/browse", p.routes.handleFilesBrowse) },
		func() error { return api.RegisterRoute("/files/read", p.routes.handleFilesRead) },
		func() error { return api.RegisterRoute("/files/raw", p.routes.handleFilesRaw) },
		func() error { return api.RegisterRoute("POST /files/write", p.routes.handleFilesWrite) },
		func() error { return api.RegisterRoute("POST /files/mkdir", p.routes.handleFilesMkdir) },
		func() error { return api.RegisterRoute("/workspaces/browse", p.routes.handleWorkspacesBrowse) },
		func() error { return api.RegisterRoute("/workspaces/files", p.routes.handleWorkspaceFiles) },
		// The default_workspace knob lands at mount.
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

// SetDefaultWorkspace is the host's hot-apply hook.
func (p *Plugin) SetDefaultWorkspace(ws string) {
	if p.routes != nil {
		p.routes.SetDefaultWorkspace(ws)
	}
}
