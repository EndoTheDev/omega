package delegate

import (
	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// Plugin implements agent.Plugin.
type Plugin struct {
	delegate *Delegate
}

// NewPlugin returns a delegate Plugin ready for MountAllAPI.
func NewPlugin() *Plugin {
	return &Plugin{delegate: NewDelegate()}
}

func (p *Plugin) Name() string { return "delegate" }

// Description implements agent.Describer.
func (p *Plugin) Description() string {
	return "delegate: spawns subagents to run subtasks in isolated sessions"
}

func (p *Plugin) ConfigKey() string { return "" }

func (p *Plugin) Provides() []string { return []string{"tools"} }

// Requires: logging, read at mount time to dispatch the spawns. The notifications seam is
// a mount-end registration, nil when the capsule is absent
// (docs/migrating-a-module.md).
func (p *Plugin) Requires() []string { return []string{"logger"} }

// MountWithAPI implements agent.APIPlugin.
func (p *Plugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	p.delegate.logger = seam[agent.LoggerProvider](ctx, "logger")
	// the store seam is an opportunistic capture, NOT a Requires edge. Nil-safe.
	p.delegate.store = seam[agent.StoreProvider](ctx, "store")
	// The child is spawned AS an agent, and the agent layer points an unset store at the
	// agent's own data dir - a different database than the one this host pre-creates the
	// child session in, so `run --session <id>` would die "session not found". The path
	// rides the child env as OMEGA_DB_PATH, so the spawned run opens its caller's store.
	// Consumer-owned local interface: the store capsule never learns its consumers.
	if pth, ok := p.delegate.store.(interface{ DBPath() string }); ok {
		p.delegate.storeDBPath = pth.DBPath()
	}
	// dispatched runs report to the finished-things store; the hook receives the seam at
	// mount end, nil when the module is absent (the emitter is nil-safe).
	d := p.delegate
	if err := api.RegisterEmitter(func(svc any) {
		d.notifier, _ = svc.(finishedThings)
	}); err != nil {
		return err
	}
	// The recursion guard's mount face: inside a subagent child (OMEGA_SUBAGENT set) the
	// delegate tools do not register at all - the child cannot spawn children.
	if d.spawnable {
		for name, tool := range d.Tools() {
			if err := api.RegisterTool(name, tool); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *Plugin) Toggleable() bool { return true }
