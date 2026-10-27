package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/engine/agent"
	"github.com/EndoTheDev/omega/modules/store"
)

// Plugin adapts ToolProvider into the agent.Plugin interface.
// It registers shell and file tools as an additive tools-seam provider
// and mounts through the ExtensionAPI exclusively (v0.12 phase F).
type Plugin struct{}

// Name returns the extension name shown in /extensions.
func (p *Plugin) Name() string { return "tools" }

// Description implements agent.Describer.
func (p *Plugin) Description() string {
	return "built-in tools: shell.run and the files domain (read/write/edit, batch operations)"
}

// Provides lists seam names this plugin mounts. "tools" is additive,
// so multiple plugins can contribute tool providers without conflict.
// ConfigKey names the config section this plugin reads from
// ctx.Configs (see agent.Describer). Empty = reads no section.
func (p *Plugin) ConfigKey() string { return "" }

func (p *Plugin) Provides() []string { return []string{"tools", "prompt_sections"} }

// Requires lists seam names that must be mounted first. tools
// has no dependencies — it only provides tools, which need nothing.
func (p *Plugin) Requires() []string { return nil }

// MountWithAPI implements agent.APIPlugin. Registers the tool
// provider, the prompt section, the /tools command and the /files
// browser renderer. The provider carries the store-door deny-guard
// (ENDO'S LAW 2026-10-18) built from the store config.
func (p *Plugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	provider := &ToolProvider{}
	if c, ok := ctx.Configs["store"].(store.Config); ok {
		provider.dbGuard = newDBGuard(c.DBPath)
	} else {
		provider.dbGuard = newDBGuard("")
	}
	steps := []func() error{
		func() error {
			for name, t := range provider.Tools() {
				if err := api.RegisterTool(name, t); err != nil {
					return err
				}
			}
			return nil
		},
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
		// The /files browser rides the module TUI seam (the read view
		// over the files domain). A tools plugin left out of
		// buildPlugins takes /files out of the command table with it
		// (card t_88bb7dcf).
		func() error { return api.RegisterRenderer(&tui{}) },
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
