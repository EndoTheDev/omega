package delegate

import (
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/engine/agent"
)

// Plugin implements agent.Plugin. It wraps a Delegate and mounts it
// into the agent Context as a ToolProvider plus the run-affinity
// InjectedStream (ctx.InjectedMessages, t_f3fb7c52) and
// PendingDelegations func.
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

// ConfigKey names the config section this plugin reads from
// ctx.Configs (see agent.Describer). Empty = reads no section.
func (p *Plugin) ConfigKey() string { return "" }

func (p *Plugin) Provides() []string { return []string{"tools"} }

// Requires: logging (the logger is read at mount time to dispatch the
// spawns). The notifications edge died with its capture: the
// dispatched-run emitter is a registration now - the seam arrives at
// mount end, nil when the module is absent (recipe law 4,
// docs/migrating-a-module.md, v0.12 phase C2).
func (p *Plugin) Requires() []string { return []string{"logging"} }

// MountWithAPI implements agent.APIPlugin. This registration is the
// module's whole surface (arc 6 capsule, card t_47675b6a): the inject
// seam (both Context fields ride one RegisterDelegation), the two
// namespaced tools (delegate.task, delegate.status) and the
// dispatched-run emitter capture ship from here, and the module ships
// nothing else - no command, no REST route, no skills, no canvas panel
// (nothing rendered delegate before the arc). Removal proof:
// TestDelegateRemovalProof (cmd/omega/removal_test.go).
func (p *Plugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	// the logger is read at mount time - the logging Requires edge
	// stays (recipe law 4)
	p.delegate.logger = ctx.Logger
	// the store seam is an opportunistic capture, NOT a Requires edge:
	// the store mounts before delegate (slice-order tiebreak) and is
	// not toggleable, but an edge naming delegate from here would be
	// the removal recipe's stop signal in the other direction. Nil-safe.
	p.delegate.store = ctx.Store
	// card t_5d9032d3: dispatched runs report to the finished-things
	// store. The hook receives the mounted notifications seam at mount
	// end, nil when the module is absent (nil is legal - the emitter
	// is nil-safe by the classic law).
	d := p.delegate
	if err := api.RegisterEmitter(func(n core.NotificationsProvider) {
		d.notifier = n
	}); err != nil {
		return err
	}
	// The recursion guard's mount face (THE delegation ruling, depth):
	// inside a subagent child (OMEGA_SUBAGENT set) the delegate tools
	// do not register at all - the child cannot spawn children. The
	// inject seam still mounts (no ops ever drain it); the plugin
	// census keeps counting the module.
	if d.spawnable {
		for name, tool := range d.Tools() {
			if err := api.RegisterTool(name, tool); err != nil {
				return err
			}
		}
	}
	// Mount the run-affinity drain seam (t_f3fb7c52): the Delegate
	// itself is the InjectedStream - results are queued per spawning
	// run and drained by the run's own affinity key. The channel bridge
	// goroutine this replaces offered no affinity: any loop could drain
	// any run's result.
	if err := api.RegisterDelegation(d, d.PendingCount); err != nil {
		return err
	}
	return nil
}

// Toggleable marks this capsule as a module capsule (v0.12 E1, the
// tier law): the config.yaml disabled: list may name it.
func (p *Plugin) Toggleable() bool { return true }
