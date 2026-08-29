package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/ai"
	"github.com/EndoTheDev/omega/gateway"
)

// Plugin is the in-process adapter for the provider extension.
// It reads provider configuration from the Context.Config (a
// gateway.Config) and mounts a Provider into the provider seam.
type Plugin struct{}

// Name returns the plugin name shown in the /extensions listing.
func (Plugin) Name() string { return "provider" }

// Provides lists the seam names this plugin mounts.
func (Plugin) Provides() []string { return []string{"provider"} }

// Requires lists seam names that must be mounted before this plugin.
func (Plugin) Requires() []string { return nil }

// Mount reads the provider config from Context.Config and sets
// ctx.Provider. If Config is nil or not a gateway.Config, the
// provider is created with zero values (type defaults to ollama).
func (p *Plugin) Mount(ctx *agent.Context) error {
	var typ, model, host, key string
	if cfg, ok := ctx.Config.(gateway.Config); ok {
		typ = cfg.Provider.Type
		model = cfg.Provider.ModelName
		host = cfg.Provider.Host
		key = cfg.Provider.APIKey
	}
	prov := NewProvider(typ, model, host, key)
	ctx.Provider = prov

	// Register /model, /models, /thinking, and /provider commands.
	ctx.Commands = append(ctx.Commands,
		agent.ExtensionCommand{Name: "/model", Description: "switch the model (line # from /models, or name)"},
		agent.ExtensionCommand{Name: "/models", Description: "list available models"},
		agent.ExtensionCommand{Name: "/thinking", Description: "set or cycle thinking level [none|off|on|minimal|low|medium|high|extra high|max|ultra]"},
		agent.ExtensionCommand{Name: "/provider", Description: "show current provider and model"},
	)

	// Wire command handler, chaining after any previous handler.
	prev := ctx.CommandHandler
	ctx.CommandHandler = func(c context.Context, name, args string) (agent.CommandResult, error) {
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
func (p *Plugin) handleCommand(prov *Provider, name, args string) (agent.CommandResult, error) {
	switch name {
	case "/model":
		if args == "" {
			return agent.CommandResult{Text: "usage: /model <name>"}, nil
		}
		prov.SetModel(args)
		return agent.CommandResult{
			Text: "switched to " + args,
			Actions: []agent.CmdAction{
				{Type: "set_model", Value: args},
				{Type: "refresh_title"},
				{Type: "fetch_model_info"},
			},
		}, nil
	case "/models":
		models, err := prov.ListModels()
		if err != nil {
			return agent.CommandResult{}, fmt.Errorf("list models: %w", err)
		}
		if len(models) == 0 {
			return agent.CommandResult{Text: "[no models available]"}, nil
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
		return agent.CommandResult{
			Text:    sb.String(),
			Actions: []agent.CmdAction{{Type: "set_model_list", List: models}},
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
				return agent.CommandResult{}, fmt.Errorf("usage: /thinking [none|off|on|minimal|low|medium|high|extra high|max|ultra]")
			}
		}
		prov.SetThinkingLevel(level)
		return agent.CommandResult{
			Text:    "[thinking " + level + "]",
			Actions: []agent.CmdAction{{Type: "set_thinking", Value: level}},
		}, nil
	case "/provider":
		return agent.CommandResult{
			Text: fmt.Sprintf("provider: %s\nmodel: %s", prov.typ, prov.ModelName()),
		}, nil
	}
	return agent.CommandResult{}, fmt.Errorf("unknown command: %s", name)
}