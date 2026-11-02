package process

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/engine/agent"
	"github.com/EndoTheDev/omega/modules/datadir"
)

// Plugin adapts Registry into the agent.Plugin interface. The process
// capsule is an additive tools-seam born-capsule: the five
// process.* tools are the whole surface - no command, no REST route,
// no prompt section, no webui panel. Removal proof:
// TestProcessRemovalProof (cmd/omega/removal_test.go).
type Plugin struct{}

// NewPlugin creates the module plugin.
func NewPlugin() *Plugin { return &Plugin{} }

// Name returns the plugin name shown in /extensions.
func (p *Plugin) Name() string { return "process" }

// Description implements agent.Describer.
func (p *Plugin) Description() string {
	return "background process registry: detached long-running commands (spawn/list/read/kill/wait)"
}

// ConfigKey names the config section this plugin reads from
// ctx.Configs.
func (p *Plugin) ConfigKey() string { return "process" }

// Provides lists the seams this plugin mounts. tools is additive -
// multiple plugins can contribute tool providers.
func (p *Plugin) Provides() []string { return []string{"tools"} }

// Requires lists seams that must be mounted first. process needs
// none - the registry starts even on a fresh home.
func (p *Plugin) Requires() []string { return nil }

// Toggleable marks the capsule: the config.yaml disabled list may name
// it.
func (p *Plugin) Toggleable() bool { return true }

// MountWithAPI implements agent.APIPlugin. Mount builds the registry
// (lazy: the state file is read on the first tool call, never during
// Mount - the goroutine fence) and registers the tool face.
func (p *Plugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	cfg := Config{}
	if c, ok := ctx.Configs["process"].(Config); ok {
		cfg = c
	}
	if cfg.Home == "" {
		cfg.Home = datadir.Home()
	}
	registry := NewRegistry(cfg)
	prov := processProvider{reg: registry}
	for name, t := range prov.Tools() {
		if err := api.RegisterTool(name, t); err != nil {
			return err
		}
	}
	return nil
}

// processProvider adapts Registry to core.Tool values. The
// machine-facing identities are the module.verb pairs (Q10, the v0.12
// mount migration): five verbs, one shared dispatcher - each Run stamps
// its action and falls through.
type processProvider struct {
	reg *Registry
}

// description constants (the first line is the prompt's one-liner,
// the firstLine law of the tools module).
const (
	descriptionSpawn = "Spawn a command detached; it survives agent restarts and its output goes to a log, not the context.\nUse shell.run for anything expected to finish in seconds - process.spawn is for LONG ops only (builds, transfers, servers) whose output would block the turn and flood the context."
	descriptionList  = "List background processes: state (running/exited with exit code), started, log size."
	descriptionRead  = "Tail the last N lines of a background process's log - the tail, never the whole log (context law)."
	descriptionKill  = "Stop a background process: SIGTERM first, escalating to SIGKILL after the grace period."
	descriptionWait  = "Wait for a background process to exit and report its exit code."
)

// Tools returns the five process.* verbs.
func (p processProvider) Tools() map[string]core.Tool {
	verb := func(params map[string]any, action, description string) core.Tool {
		return core.Tool{
			Description: description,
			Parameters:  params,
			Run: func(ctx context.Context, args map[string]any) (string, error) {
				args["action"] = action
				return p.run(ctx, args)
			},
		}
	}
	spawnParams := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "Full shell command line (required). LONG ops only.",
			},
			"name": map[string]any{
				"type":        "string",
				"description": "Handle for the later read/kill/wait calls. Generated (p-<timestamp>) when omitted.",
			},
		},
		"required": []string{"command"},
	}
	listParams := map[string]any{
		"type":       "object",
		"properties": map[string]any{},
		"required":   []string{},
	}
	readParams := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "Spawn handle (required).",
			},
			"lines": map[string]any{
				"type":        "integer",
				"description": "Tail count (default 100, max 2000).",
			},
		},
		"required": []string{"name"},
	}
	nameOnlyParams := func() map[string]any {
		return map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "Spawn handle (required).",
				},
			},
			"required": []string{"name"},
		}
	}
	waitParams := nameOnlyParams()
	waitParams["properties"].(map[string]any)["timeout_seconds"] = map[string]any{
		"type":        "integer",
		"description": "Max seconds to block (0 or omitted: until the run budget).",
	}
	return map[string]core.Tool{
		"process.spawn": verb(spawnParams, "spawn", descriptionSpawn),
		"process.list":  verb(listParams, "list", descriptionList),
		"process.read":  verb(readParams, "read", descriptionRead),
		"process.kill":  verb(nameOnlyParams(), "kill", descriptionKill),
		"process.wait":  verb(waitParams, "wait", descriptionWait),
	}
}

// run is the shared dispatcher behind the five verbs.
func (p processProvider) run(ctx context.Context, args map[string]any) (string, error) {
	switch action, _ := args["action"].(string); action {
	case "spawn":
		command, err := argString(args, "command")
		if err != nil {
			return "", err
		}
		name, _ := args["name"].(string)
		e, err := p.reg.Spawn(ctx, command, name)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("spawned name=%s pid=%d state=%s\nlog: %s\nread the tail: process.read name=%s",
			e.Name, e.PID, stateOf(e), e.LogPath, e.Name), nil
	case "list":
		return renderList(p.reg.List()), nil
	case "read":
		name, err := argString(args, "name")
		if err != nil {
			return "", err
		}
		lines, err := argInt(args, "lines")
		if err != nil {
			return "", err
		}
		text, _, cut, err := p.reg.Read(name, lines)
		if err != nil {
			return "", err
		}
		if cut {
			text += "\n... [log truncated: showing only the tail - the agent reads the tail, never the whole log]"
		}
		return text, nil
	case "kill":
		name, err := argString(args, "name")
		if err != nil {
			return "", err
		}
		return p.reg.Kill(name)
	case "wait":
		name, err := argString(args, "name")
		if err != nil {
			return "", err
		}
		timeout, err := argInt(args, "timeout_seconds")
		if err != nil {
			return "", err
		}
		return p.reg.Wait(ctx, name, timeout)
	default:
		return "", fmt.Errorf("unknown process action %q", action)
	}
}

// renderList formats the registry snapshot as aligned text.
func renderList(entries []Entry) string {
	if len(entries) == 0 {
		return "[no background processes]"
	}
	nameWidth := len("NAME")
	for _, e := range entries {
		if len(e.Name) > nameWidth {
			nameWidth = len(e.Name)
		}
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%-*s  %-44s  %-9s  %s\n", nameWidth, "NAME", "COMMAND", "STATE", "LOG")
	for _, e := range entries {
		command := e.Command
		if len(command) > 42 {
			command = command[:39] + "..."
		}
		fmt.Fprintf(&sb, "%-*s  %-44s  %-9s  %s\n",
			nameWidth, e.Name, command, stateOf(&e), humanBytes(logSize(e.LogPath)))
	}
	return sb.String()
}

// logSize stats a log file (0 when missing).
func logSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

// humanBytes renders a byte count compactly.
func humanBytes(n int64) string {
	switch {
	case n >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	case n >= 1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// argString extracts a required string argument.
func argString(args map[string]any, key string) (string, error) {
	v, ok := args[key]
	if !ok {
		return "", fmt.Errorf("missing required argument %q", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("argument %q must be a string, got %T", key, v)
	}
	return s, nil
}

// argInt extracts an optional integer argument.
func argInt(args map[string]any, key string) (int, error) {
	v, ok := args[key]
	if !ok || v == nil {
		return 0, nil
	}
	switch n := v.(type) {
	case int:
		return n, nil
	case float64:
		return int(n), nil
	default:
		return 0, fmt.Errorf("argument %q must be an integer, got %T", key, v)
	}
}

// Compile-time assertion that the plugin is a Plugin.
var _ agent.Plugin = (*Plugin)(nil)