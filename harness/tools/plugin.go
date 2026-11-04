package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/agent"
)

// Plugin adapts the tools registry into the agent.Plugin interface.
// It provides NO tools itself: the built-in tool capsules (files,
// shell) register their own providers through the same additive rail.
// This capsule is the REGISTRY - the merge machinery's claim law, the
// /tools command, the ToolsList panel and the prompt section (v0.12
// phase F; the shell-ring shrink wave 1 moved the built-ins out).
type Plugin struct{}

// Name returns the extension name shown in /extensions.
func (p *Plugin) Name() string { return "tools" }

// Description implements agent.Describer.
func (p *Plugin) Description() string {
	return "the tools registry: tool claims and conflict law, the /tools command, the ToolsList panel and the prompt section"
}

// Provides lists seam names this plugin mounts: the prompt section
// (the Tools section renders every extension's tool list from the
// mount Context's inventory). The tools-seam rail is contributed by
// the tool capsules themselves (files, shell) - the registry owns
// none of it.
// ConfigKey names the config section this plugin reads from
// ctx.Configs (see agent.Describer). Empty = reads no section.
func (p *Plugin) ConfigKey() string { return "" }

func (p *Plugin) Provides() []string { return []string{"prompt_sections"} }

// Requires lists seam names that must be mounted first. The registry
// has no dependencies.
func (p *Plugin) Requires() []string { return nil }

// MountWithAPI implements agent.APIPlugin. Registers the prompt
// section and the /tools command. The /tools command stays the
// display-toggle feature: it returns CommandResult with display-mode
// actions the shell interprets, and its list view reads the mount
// Context's inventory - the same capture the classic wrapper held.
func (p *Plugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	steps := []func() error{
		func() error { return api.RegisterPromptSectioner(p) },
		// /tools stays an extension command (it returns CommandResult
		// with display-mode actions the shell interprets); its enum
		// completions ride on the command itself now, not a shell-side
		// map. The list view reads the mount Context's inventory - the
		// same capture the classic wrapper held.
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

// handleToolsCommand handles the /tools command. "list" (or no args)
// lists all tools from all extensions (the mount Context's inventory,
// the same read the classic wrapper made). "on|off|auto" toggles
// display state via a set_tool_display action.
func handleToolsCommand(ctx *core.Context, name, args string) (core.CommandResult, error) {
	if name != "/tools" {
		return core.CommandResult{}, fmt.Errorf("no handler for %s", name)
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

// listTools formats the tool list from ctx.Infos as text.
func listTools(ctx *core.Context) core.CommandResult {
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

	return core.CommandResult{Text: sb.String()}
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
