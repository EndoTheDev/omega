package compactor

import (
	"context"
	"fmt"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/gateway"
)

// Plugin adapts the compactor to the agent.Plugin interface.
type Plugin struct{}

func (p *Plugin) Name() string       { return "compactor" }
func (p *Plugin) Provides() []string { return []string{"compactor"} }
func (p *Plugin) Requires() []string { return []string{"provider"} }

// Mount reads CompactionConfig from ctx.Config (type-asserted to
// gateway.Config), wires the provider from the already-mounted
// provider slot, sets ctx.Compactor, and registers the /compact
// command.
func (p *Plugin) Mount(ctx *agent.Context) error {
	var compactor *Compactor
	if cfg, ok := ctx.Config.(gateway.Config); ok {
		compactor = &Compactor{
			provider: ctx.Provider,
			config:   cfg.Compaction,
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