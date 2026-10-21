package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/modules/store"
)

// Plugin adapts ToolProvider into the agent.Plugin interface.
// It registers shell and file tools as an additive tools-seam provider.
type Plugin struct{}

// Name returns the extension name shown in /extensions.
func (p *Plugin) Name() string { return "tools" }

// Description implements agent.Describer.
func (p *Plugin) Description() string {
	return "built-in tools: shell.run and the files domain (read/write/edit, batch operations)"
}

// Provides lists seam names this plugin mounts. "tools" is additive,
// so multiple plugins can contribute tool providers without conflict.
func (p *Plugin) Provides() []string { return []string{"tools", "prompt_sections"} }

// Requires lists seam names that must be mounted first. tools
// has no dependencies — it only provides tools, which need nothing.
func (p *Plugin) Requires() []string { return nil }

// Mount appends the ToolProvider to ctx.ToolProviders and registers
// the /tools command. The provider also carries the store-door
// deny-guard (ENDO'S LAW 2026-10-18) built from the store config.
func (p *Plugin) Mount(ctx *agent.Context) error {
	provider := &ToolProvider{}
	if c, ok := ctx.Configs["store"].(store.Config); ok {
		provider.dbGuard = newDBGuard(c.DBPath)
	} else {
		provider.dbGuard = newDBGuard("")
	}
	ctx.ToolProviders = append(ctx.ToolProviders, provider)
	ctx.PromptSectioners = append(ctx.PromptSectioners, p)

	ctx.Commands = append(ctx.Commands,
		agent.ExtensionCommand{Name: "/tools", Description: "list tools or toggle tool result display [on|off|auto|list]"},
	)

	prev := ctx.CommandHandler
	ctx.CommandHandler = func(c context.Context, name, args string) (agent.CommandResult, error) {
		if name == "/tools" {
			return handleToolsCommand(ctx, args)
		}
		if prev != nil {
			return prev(c, name, args)
		}
		return agent.CommandResult{}, fmt.Errorf("unknown command: %s", name)
	}

	return nil
}

// handleToolsCommand handles the /tools command. "list" (or no args)
// lists all tools from all extensions. "on|off|auto" toggles display
// state via a set_tool_display action.
func handleToolsCommand(ctx *agent.Context, args string) (agent.CommandResult, error) {
	arg := strings.TrimSpace(args)
	if arg == "" || arg == "list" {
		return listTools(ctx), nil
	}
	switch arg {
	case "on", "expanded":
		return agent.CommandResult{
			Text:    "[tool results expanded]",
			Actions: []agent.CmdAction{{Type: "set_tool_display", Value: "expanded"}},
		}, nil
	case "off", "collapsed":
		return agent.CommandResult{
			Text:    "[tool results collapsed]",
			Actions: []agent.CmdAction{{Type: "set_tool_display", Value: "collapsed"}},
		}, nil
	case "auto":
		return agent.CommandResult{
			Text:    "[tool results auto]",
			Actions: []agent.CmdAction{{Type: "set_tool_display", Value: "auto"}},
		}, nil
	default:
		return agent.CommandResult{}, fmt.Errorf("usage: /tools [on|off|auto|list]")
	}
}

// listTools formats the tool list from ctx.Infos as text.
func listTools(ctx *agent.Context) agent.CommandResult {
	var sb strings.Builder
	sb.WriteString("\n")

	nameWidth := 0
	for _, ext := range ctx.Infos {
		for _, t := range ext.ToolList {
			if len(t.Name) > nameWidth {
				nameWidth = len(t.Name)
			}
		}
	}

	for _, ext := range ctx.Infos {
		if len(ext.ToolList) == 0 {
			continue
		}
		sb.WriteString(ext.Name)
		sb.WriteString("\n")
		for _, t := range ext.ToolList {
			fmt.Fprintf(&sb, "  %-*s  %s\n", nameWidth, t.Name, firstLineOfDesc(t.Description))
		}
		sb.WriteString("\n")
	}

	if nameWidth == 0 {
		sb.WriteString("[no tools available]\n")
	}

	return agent.CommandResult{Text: sb.String()}
}

// firstLineOfDesc returns the first line of a tool description,
// truncated to 60 chars.
func firstLineOfDesc(desc string) string {
	if i := strings.IndexByte(desc, '\n'); i >= 0 {
		desc = desc[:i]
	}
	if len(desc) > 60 {
		return desc[:57] + "..."
	}
	return desc
}

// NewPlugin creates a new tools plugin.
func NewPlugin() *Plugin { return &Plugin{} }