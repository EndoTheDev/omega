package compactor

import (
	"context"
	"fmt"

	"github.com/EndoTheDev/omega/agent"
)

// Plugin adapts the compactor to the agent.Plugin interface.
type Plugin struct{}

func (p *Plugin) Name() string       { return "compactor" }
func (p *Plugin) Provides() []string { return []string{"compactor"} }
func (p *Plugin) Requires() []string { return []string{"provider"} }

// Mount reads CompactionConfig from ctx.Configs, wires the provider
// from the already-mounted provider slot, sets ctx.Compactor, and
// registers the /compact command.
func (p *Plugin) Mount(ctx *agent.Context) error {
	var compactor *Compactor
	if cfg, ok := ctx.Configs["compactor"].(agent.CompactionConfig); ok {
		compactor = &Compactor{
			provider: ctx.Provider,
			config:   cfg,
		}
		// context_window 0 = auto: probe the provider for the model's
		// context window once at server start and cache it for the
		// process lifetime (one probe per process, the same pattern
		// http_channel's ctxWindow cache uses). A failed probe - or a
		// provider that cannot report a window - leaves autoWindow at
		// 0, so the DefaultContextWindow fallback in Budget engages
		// and the server still boots. Manual values > 0 never probe
		// and keep overriding exactly as today.
		if cfg.ContextWindow <= 0 && ctx.Provider != nil {
			if info, err := ctx.Provider.ModelInfo(); err == nil {
				compactor.autoWindow = info.ContextWindow
			}
		}
	} else {
		compactor = &Compactor{
			provider: ctx.Provider,
		}
	}
	ctx.Compactor = compactor

	// Register /compact command.
	ctx.Commands = append(ctx.Commands,
		agent.ExtensionCommand{Name: "/compact", Description: "summarize conversation history"},
	)

	// Wire command handler, chaining after any previous handler.
	prev := ctx.CommandHandler
	ctx.CommandHandler = func(c context.Context, name, args string) (agent.CommandResult, error) {
		if name == "/compact" {
			return agent.CommandResult{
				Actions: []agent.CmdAction{{Type: "run_compact"}},
			}, nil
		}
		if prev != nil {
			return prev(c, name, args)
		}
		return agent.CommandResult{}, fmt.Errorf("unknown command: %s", name)
	}

	return nil
}

// NewPlugin creates a compactor plugin instance.
func NewPlugin() *Plugin { return &Plugin{} }
