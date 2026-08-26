package delegate

import (
	"github.com/EndoTheDev/omega/agent"
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

func (p *Plugin) Name() string       { return "delegate" }
func (p *Plugin) Provides() []string { return []string{"tools"} }
func (p *Plugin) Requires() []string { return nil }

// Mount appends the delegate ToolProvider, sets InjectedMessages and
// PendingDelegations on the Context.
func (p *Plugin) Mount(ctx *agent.Context) error {
	ctx.ToolProviders = append(ctx.ToolProviders, p.delegate)

	// Bridge internal injectedMsg channel to agent.InjectedMessage.
	agentCh := make(chan agent.InjectedMessage, 16)
	ctx.InjectedMessages = agentCh
	ctx.PendingDelegations = p.delegate.PendingCount

	go func() {
		for msg := range p.delegate.InjectedChannel() {
			agentCh <- agent.InjectedMessage{Text: msg.text, Source: msg.source}
		}
	}()

	return nil
}