package compactor

import (
	"context"
	"fmt"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/engine/agent"
)

// Plugin adapts the compactor to the agent.Plugin interface. It
// mounts through the ExtensionAPI exclusively (v0.12 phase F).
type Plugin struct{}

func (p *Plugin) Name() string { return "compactor" }

// Description implements agent.Describer.
func (p *Plugin) Description() string {
	return "context compaction: keeps the transcript head and tail, summarizes the middle when the token budget is exceeded"
}

// ConfigKey names the config section this plugin reads from
// ctx.Configs (see agent.Describer). Empty = reads no section.
func (p *Plugin) ConfigKey() string { return "compactor" }

func (p *Plugin) Provides() []string { return []string{"compactor"} }
func (p *Plugin) Requires() []string { return []string{"provider"} }

// MountWithAPI implements agent.APIPlugin. Reads CompactionConfig from
// ctx.Configs, wires the provider from the already-mounted provider
// slot (the Requires edge orders the read), registers the compactor
// seam and the /compact command.
func (p *Plugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	var compactor *Compactor
	if cfg, ok := ctx.Configs["compactor"].(core.CompactionConfig); ok {
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
	// /compact stays an extension command (it returns CommandResult
	// with a display-mode action the shell interprets). Registration
	// order mirrors the classic body: seam first, command second - the
	// api chain-on-miss wrapper produces the same fall-through.
	if err := api.RegisterCompactor(compactor); err != nil {
		return err
	}
	return api.RegisterCommand("/compact", "summarize conversation history", nil, p.handleCompact)
}

// handleCompact answers the /compact command (the command body moved
// here so the registration and the behavior sit in one file).
func (p *Plugin) handleCompact(ctx context.Context, name, args string) (core.CommandResult, error) {
	if name != "/compact" {
		return core.CommandResult{}, fmt.Errorf("no handler for %s", name)
	}
	return core.CommandResult{
		Actions: []core.CmdAction{{Type: "run_compact"}},
	}, nil
}

// NewPlugin creates a compactor plugin instance.
func NewPlugin() *Plugin { return &Plugin{} }
