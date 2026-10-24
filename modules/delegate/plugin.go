package delegate

import (
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

// Requires: the notifications seam (Mount captures ctx.Notifications
// as the dispatched-run emitter, card t_5d9032d3) and logging (Mount
// captures ctx.Logger). Card t_13a2af8a: mount order is declared
// data - the edge, not slice position.
func (p *Plugin) Requires() []string { return []string{"notifications", "logging"} }

// Mount appends the delegate ToolProvider and sets InjectedMessages
// and PendingDelegations on the Context.
//
// This Mount is the module's whole surface (arc 6 capsule, card
// t_47675b6a): the inject seam, the consolidated delegate tool with its
// task/status alias shims, and the notifications emitter capture all
// ship from here, and the module ships nothing else - no command, no
// REST route, no skills, no canvas panel (nothing rendered delegate
// before the arc). Removal proof:
// TestDelegateRemovalProof (cmd/omega/removal_test.go).
func (p *Plugin) Mount(ctx *core.Context) error {
	p.delegate.logger = ctx.Logger
	p.delegate.notifier = ctx.Notifications
	ctx.ToolProviders = append(ctx.ToolProviders, p.delegate)

	// Bridge internal injectedMsg channel to core.InjectedMessage.
	agentCh := make(chan core.InjectedMessage, 16)
	ctx.InjectedMessages = agentCh
	ctx.PendingDelegations = p.delegate.PendingCount

	go func() {
		for msg := range p.delegate.InjectedChannel() {
			agentCh <- core.InjectedMessage{Text: msg.text, Source: msg.source}
		}
	}()

	return nil
}
