package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/EndoTheDev/omega/ai"
	"github.com/EndoTheDev/omega/core"
)

// Plugin is the in-process adapter for the provider extension.
// It reads provider configuration from Context.Configs and mounts
// a Provider into the provider seam.
type Plugin struct{}

// Name returns the plugin name shown in the /extensions listing.
func (Plugin) Name() string { return "provider" }

// Description implements agent.Describer.
func (Plugin) Description() string {
	return "model provider: LLM transport (Ollama, OpenAI, Anthropic), streaming and model listing"
}

// Provides lists the seam names this plugin mounts.
// ConfigKey names the config section this plugin reads from
// ctx.Configs (see agent.Describer). Empty = reads no section.
func (p *Plugin) ConfigKey() string { return "provider" }

func (Plugin) Provides() []string { return []string{"provider"} }

// Requires lists seam names that must be mounted before this plugin.
func (Plugin) Requires() []string { return nil }

// Mount reads the provider config from Context.Configs and sets
// ctx.Provider. If config is missing, the provider is created with
// zero values (type defaults to ollama).
func (p *Plugin) Mount(ctx *core.Context) error {
	cfg := Default()
	if c, ok := ctx.Configs["provider"].(Config); ok {
		cfg = c
	}
	prov := NewProvider(cfg.Type, cfg.ModelName, cfg.Host, cfg.APIKey)
	ctx.Provider = prov

	// Register /model, /models, /thinking, and /provider commands.
	ctx.Commands = append(ctx.Commands,
		core.ExtensionCommand{Name: "/model", Description: "switch the model (line # from /models, or name)"},
		core.ExtensionCommand{Name: "/models", Description: "list available models"},
		core.ExtensionCommand{Name: "/thinking", Description: "set or cycle thinking level [none|off|on|minimal|low|medium|high|extra high|max|ultra]"},
		core.ExtensionCommand{Name: "/provider", Description: "show current provider and model"},
	)

	// Wire command handler, chaining after any previous handler.
	prev := ctx.CommandHandler
	ctx.CommandHandler = func(c context.Context, name, args string) (core.CommandResult, error) {
		result, err := p.handleCommand(prov, name, args)
		if err == nil {
			return result, nil
		}
		if prev != nil {
			return prev(c, name, args)
		}
		return result, err
	}

	return nil
}

// handleCommand handles /model and /provider commands.
func (p *Plugin) handleCommand(prov *Provider, name, args string) (core.CommandResult, error) {
	switch name {
	case "/model":
		if args == "" {
			return core.CommandResult{Text: "usage: /model <name>"}, nil
		}
		// Base-provider model set (single user, applied outside a run);
		// runs themselves carry their own WithModel copy.
		*prov = *prov.WithModel(args).(*Provider)
		return core.CommandResult{
			Text: "switched to " + args,
			Actions: []core.CmdAction{
				{Type: "set_model", Value: args},
				{Type: "refresh_title"},
				{Type: "fetch_model_info"},
			},
		}, nil
	case "/models":
		models, err := prov.ListModels()
		if err != nil {
			return core.CommandResult{}, fmt.Errorf("list models: %w", err)
		}
		if len(models) == 0 {
			return core.CommandResult{Text: "[no models available]"}, nil
		}
		nameWidth := 4
		for _, n := range models {
			if len(n) > nameWidth {
				nameWidth = len(n)
			}
		}
		var sb strings.Builder
		sb.WriteString("\n")
		fmt.Fprintf(&sb, "  %-3s  %-*s\n", "#", nameWidth, "NAME")
		for i, n := range models {
			marker := "  "
			if n == prov.ModelName() {
				marker = "> "
			}
			fmt.Fprintf(&sb, "%s%-3d  %-*s\n", marker, i+1, nameWidth, n)
		}
		return core.CommandResult{
			Text:    sb.String(),
			Actions: []core.CmdAction{{Type: "set_model_list", List: models}},
		}, nil
	case "/thinking":
		level := strings.TrimSpace(args)
		if level == "" {
			// Cycle to next level.
			current := prov.thinkingLevel
			if current == "" {
				current = "medium"
			}
			idx := 0
			for i, l := range ai.ThinkingLevels {
				if l == current {
					idx = i
					break
				}
			}
			level = ai.ThinkingLevels[(idx+1)%len(ai.ThinkingLevels)]
		} else {
			valid := false
			for _, l := range ai.ThinkingLevels {
				if l == level {
					valid = true
					break
				}
			}
			if !valid {
				return core.CommandResult{}, fmt.Errorf("usage: /thinking [none|off|on|minimal|low|medium|high|extra high|max|ultra]")
			}
		}
		prov.SetThinkingLevel(level)
		return core.CommandResult{
			Text:    "[thinking " + level + "]",
			Actions: []core.CmdAction{{Type: "set_thinking", Value: level}},
		}, nil
	case "/provider":
		return core.CommandResult{
			Text: fmt.Sprintf("provider: %s\nmodel: %s", prov.typ, prov.ModelName()),
		}, nil
	}
	return core.CommandResult{}, fmt.Errorf("unknown command: %s", name)
}
