package delegate

import (
	"fmt"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// Plugin implements agent.Plugin. It wraps a Delegate and mounts it
// into the agent Context as a ToolProvider plus InjectedMessages channel
// and PendingDelegations func.
type Plugin struct {
	delegate *Delegate
}

// NewPlugin returns a delegate Plugin ready for MountAll.
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

// Mount satisfies agent.Plugin (the interface keeps its classic shape
// until phase F). It is never dispatched: MountWithAPI exists, and
// APIPlugin wins (the strangler rule). If ever reached it fails loud -
// delegate does not mount the old way, which is the proof of migration.
func (p *Plugin) Mount(ctx *core.Context) error {
	return fmt.Errorf("delegate: classic mount retired - the capsule mounts through the ExtensionAPI (v0.12 phase C2, docs/migrating-a-module.md)")
}

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
	for name, tool := range d.Tools() {
		if err := api.RegisterTool(name, tool); err != nil {
			return err
		}
	}
	// Bridge the internal injectedMsg channel to core.InjectedMessage.
	agentCh := make(chan core.InjectedMessage, 16)
	if err := api.RegisterDelegation(agentCh, d.PendingCount); err != nil {
		return err
	}
	go func() {
		for msg := range d.InjectedChannel() {
			agentCh <- core.InjectedMessage{Text: msg.text, Source: msg.source}
		}
	}()
	return nil
}

// Toggleable marks this capsule as a module capsule (v0.12 E1, the
// tier law): the omega.json disable list may name it.
func (p *Plugin) Toggleable() bool { return true }
