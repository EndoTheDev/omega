package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/harness/datadir"
	"github.com/EndoTheDev/omega/harness/store"
)

type Plugin struct {
	search *SearchProvider
}

func (p *Plugin) Name() string { return "tools" }

func (p *Plugin) Description() string {
	return "the tools registry: the grep/glob search tools, tool claim and conflict law, the /tools command, the ToolsList panel and the prompt section"
}

func (p *Plugin) ConfigKey() string { return "" }

func (p *Plugin) Provides() []string { return []string{"prompt_sections", "tools"} }

func (p *Plugin) Requires() []string { return nil }

func (p *Plugin) Toggleable() bool { return true }

func (p *Plugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	// The search tools pass the same db-door guard the file tools use: the guard is built
	// from the store config (the instance db path); nil/zero-value guards are
	// pass-through.
	var guard *datadir.Guard
	if c, ok := ctx.Configs["store"].(store.Config); ok {
		guard = datadir.NewGuard(c.DBPath)
	} else {
		guard = datadir.NewGuard("")
	}
	p.search = NewSearchProvider(guard)

	steps := []func() error{
		func() error {
			// files.grep / files.glob: the search verbs keep their model-facing names
			for name, t := range p.search.Tools() {
				if err := api.RegisterBuiltinTool(name, t); err != nil {
					return err
				}
			}
			return nil
		},
		func() error { return api.RegisterPromptSectioner(p) },
		// /tools stays an extension command.
		func() error {
			return api.RegisterCommand("/tools", "list tools or toggle tool result display [on|off|auto|list]",
				[]string{"on", "off", "auto", "list"}, func(_ context.Context, name, args string) (core.CommandResult, error) {
					return handleToolsCommand(ctx, name, args)
				})
		},
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}
	return nil
}

func handleToolsCommand(ctx *core.Context, name, args string) (core.CommandResult, error) {
	if name != "/tools" {
		return core.CommandResult{}, core.CommandMiss(name)
	}
	arg := strings.TrimSpace(args)
	if arg == "" || arg == "list" {
		return listTools(ctx), nil
	}
	switch arg {
	case "on", "expanded":
		return core.CommandResult{
			Text:    "[tool results expanded]",
			Actions: []core.CmdAction{{Type: "set_tool_display", Value: "expanded"}},
		}, nil
	case "off", "collapsed":
		return core.CommandResult{
			Text:    "[tool results collapsed]",
			Actions: []core.CmdAction{{Type: "set_tool_display", Value: "collapsed"}},
		}, nil
	case "auto":
		return core.CommandResult{
			Text:    "[tool results auto]",
			Actions: []core.CmdAction{{Type: "set_tool_display", Value: "auto"}},
		}, nil
	default:
		return core.CommandResult{}, fmt.Errorf("usage: /tools [on|off|auto|list]")
	}
}

func listTools(ctx *core.Context) core.CommandResult {
	var sb strings.Builder
	sb.WriteString("\n")

	infos := core.StampDisabledTools(ctx.Infos, ctx.DisabledTools)
	nameWidth := 0
	disabled := 0
	for _, ext := range infos {
		for _, t := range ext.ToolList {
			if len(t.Name) > nameWidth {
				nameWidth = len(t.Name)
			}
			if t.Disabled {
				disabled++
			}
		}
	}

	for _, ext := range infos {
		if len(ext.ToolList) == 0 {
			continue
		}
		sb.WriteString(ext.Name)
		sb.WriteString("\n")
		for _, t := range ext.ToolList {
			line := fmt.Sprintf("  %-*s  %s", nameWidth, t.Name, firstLineOfDesc(t.Description))
			if t.Disabled {
				line = strings.TrimRight(line, " ") + "  [disabled]"
			}
			sb.WriteString(line)
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	if nameWidth == 0 {
		sb.WriteString("[no tools available]\n")
	} else if disabled > 0 {
		fmt.Fprintf(&sb, "[%d disabled: /tools stays the listing, omega tools enable <name> turns one back on]\n", disabled)
	}

	return core.CommandResult{Text: sb.String()}
}

func firstLineOfDesc(desc string) string {
	if i := strings.IndexByte(desc, '\n'); i >= 0 {
		desc = desc[:i]
	}
	if len(desc) > 60 {
		return desc[:57] + "..."
	}
	return desc
}

func NewPlugin() *Plugin { return &Plugin{} }
