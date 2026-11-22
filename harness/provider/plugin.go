package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/llm"
)

type Plugin struct{}

func (Plugin) Name() string { return "provider" }

func (Plugin) Description() string {
	return "model provider: LLM transport (Ollama, OpenAI, Anthropic), streaming and model listing"
}

func (p *Plugin) ConfigKey() string { return "provider" }

func (Plugin) Provides() []string { return []string{"provider"} }

func (Plugin) Requires() []string { return nil }

func (p *Plugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	cfg := Default()
	if c, ok := ctx.Configs["provider"].(Config); ok {
		cfg = c
	}
	prov := NewProvider(cfg.Type, cfg.ModelName, cfg.Host, cfg.APIKey)
	// The instances: one resolver per config provider.instances row, eager at mount.
	// The shell resolves a run's pinned instance through agent.ProviderInstances and fails
	// loudly on a missing name, which is how a removed instance can never strand a
	// session's run into a silent fallback.
	prov.SetConfig(cfg)
	if err := api.Offer("provider", prov); err != nil {
		return err
	}
	commands := []struct {
		name, description string
	}{
		{"/model", "switch the model (line # from /models, or name)"},
		{"/models", "list available models"},
		{"/thinking", "set or cycle thinking level [none|off|on|minimal|low|medium|high|extra high|max|ultra]"},
		{"/provider", "show current provider and model"},
	}
	for _, c := range commands {
		if err := api.RegisterCommand(c.name, c.description, nil,
			func(ctx context.Context, name, args string) (core.CommandResult, error) {
				return p.handleCommand(prov, name, args)
			}); err != nil {
			return err
		}
	}
	return nil
}

func (p *Plugin) handleCommand(prov *Provider, name, args string) (core.CommandResult, error) {
	switch name {
	case "/model":
		if args == "" {
			return core.CommandResult{Text: "usage: /model <name>"}, nil
		}
		prov.SetModel(args)
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
			current := prov.thinkingLevel
			if current == "" {
				current = "medium"
			}
			idx := 0
			for i, l := range llm.ThinkingLevels {
				if l == current {
					idx = i
					break
				}
			}
			level = llm.ThinkingLevels[(idx+1)%len(llm.ThinkingLevels)]
		} else {
			valid := false
			for _, l := range llm.ThinkingLevels {
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
	return core.CommandResult{}, core.CommandMiss(name)
}
