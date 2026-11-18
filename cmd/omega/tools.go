package main

// cmd/omega/tools.go: the tools subcommand.

import (
	"fmt"
	"io"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// toolsHelp is the module's own help: `omega tools --help` prints it.
const toolsHelp = `omega tools - the wire tool registry (enable/disable)

Usage:
  omega tools                     every tool with its state (same as "omega tools list")
  omega tools list                tool table: name, state, extension, description
  omega tools enable <name>       put the tool back on the wire
  omega tools disable <name>      take the tool off the wire (absent from the model's tool list)

The state lives in config.yaml's disabled.tools fold - the same key the
settings tab's tools toggle writes, so the terminal and the tab never
disagree. A disabled tool keeps its listing row (that is how you find
the name to re-enable); it never reaches the model, and the system
prompt's tool section stops advertising it.
`

// cmdTools is the subcommand handler.
func cmdTools(args []string) error {
	for _, a := range args {
		if a == "--help" || a == "-h" {
			fmt.Print(toolsHelp)
			return nil
		}
	}
	sub, rest := "list", args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, rest = args[0], args[1:]
	}
	switch sub {
	case "list":
		return toolsList(rest, os.Stdout)
	case "enable":
		return toolsToggle(rest, true, os.Stdout)
	case "disable":
		return toolsToggle(rest, false, os.Stdout)
	case "help":
		fmt.Print(toolsHelp)
		return nil
	default:
		fmt.Fprint(os.Stderr, toolsHelp)
		return fmt.Errorf("unknown tools subcommand %q", sub)
	}
}

// toolRow is one listing row.
type toolRow struct {
	Name     string
	State    string
	Origin   string
	Brief    string
	Disabled bool
}

// toolRows renders the fold-aware inventory: every registered tool with its enable flag
// stamped from the fold, name-sorted. The input is the mount Context's extension
// inventory (the full registry, before the fold filters the loop's union), so a
// disabled tool keeps its row - exactly the law GET /tools?disabled=1 follows.
func toolRows(infos []core.ExtensionInfo, disabled []string) []toolRow {
	var rows []toolRow
	for _, info := range core.StampDisabledTools(infos, disabled) {
		for _, t := range info.ToolList {
			state := "enabled"
			if t.Disabled {
				state = "disabled"
			}
			rows = append(rows, toolRow{
				Name:     t.Name,
				State:    state,
				Origin:   info.Name,
				Brief:    toolBrief(t.Description),
				Disabled: t.Disabled,
			})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	return rows
}

// toolBrief is the one-line listing form of a description.
func toolBrief(desc string) string {
	for _, line := range strings.Split(desc, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) > 60 {
			return line[:57] + "..."
		}
		return line
	}
	return ""
}

// toolsList prints the inventory.
func toolsList(args []string, w io.Writer) error {
	if extra := stripConfigFlag(args); len(extra) > 0 {
		return fmt.Errorf("tools list takes no arguments (got %q)", extra[0])
	}
	cfgPath := resolveConfigPath(parseConfigFlag(args))
	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		return fmt.Errorf("%s: %w", cfgPath, err)
	}
	reg, pctx, err := mountRegistry(cfg)
	if err != nil {
		return err
	}
	rows := toolRows(pctx.Infos, pctx.DisabledTools)
	_ = agent.DismountAll(reg, pctx)

	if len(rows) == 0 {
		fmt.Fprintln(w, "[no tools registered]")
		return nil
	}
	nameWidth, originWidth := 0, len("EXTENSION")
	for _, r := range rows {
		if len(r.Name) > nameWidth {
			nameWidth = len(r.Name)
		}
		if len(r.Origin) > originWidth {
			originWidth = len(r.Origin)
		}
	}
	fmt.Fprintf(w, "%-*s  %-8s  %-*s  %s\n", nameWidth, "TOOL", "STATE", originWidth, "EXTENSION", "DESCRIPTION")
	disabled := 0
	for _, r := range rows {
		if r.Disabled {
			disabled++
		}
		fmt.Fprintf(w, "%-*s  %-8s  %-*s  %s\n", nameWidth, r.Name, r.State, originWidth, r.Origin, r.Brief)
	}
	fmt.Fprintf(w, "\n%d tools, %d disabled. omega tools disable <name> | omega tools enable <name>\n", len(rows), disabled)
	return nil
}

// toolsToggle applies one enable/disable. Disabling validates the name against the
// mounted registry (an unknown name would persist a dead fold entry that no listing
// could ever show); enabling never validates - a name whose registry entry has moved on
// must still be cleanable out of the fold by hand-typed name.
func toolsToggle(args []string, enabled bool, w io.Writer) error {
	pos := stripConfigFlag(args)
	if len(pos) == 0 {
		return fmt.Errorf("usage: omega tools %s <name>", stateVerb(enabled))
	}
	if len(pos) > 1 {
		return fmt.Errorf("omega tools %s takes one tool name (got %q)", stateVerb(enabled), strings.Join(pos, " "))
	}
	name := pos[0]
	if strings.HasPrefix(name, "-") {
		return fmt.Errorf("omega tools %s: %q is a flag, not a tool name", stateVerb(enabled), name)
	}
	cfgPath := resolveConfigPath(parseConfigFlag(args))
	if cfgPath == "" {
		return fmt.Errorf("no config found in %s - run `omega setup` first", omegaHome())
	}
	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		return fmt.Errorf("%s: %w", cfgPath, err)
	}
	if _, changed := toggleToolFold(cfg.Disabled.Tools, name, enabled); !changed {
		fmt.Fprintf(w, "tool %s is already %s\n", name, stateWord(enabled))
		return nil
	}
	if !enabled {
		reg, pctx, err := mountRegistry(cfg)
		if err != nil {
			return err
		}
		known := toolInInfos(pctx.Infos, name)
		_ = agent.DismountAll(reg, pctx)
		if !known {
			return fmt.Errorf("tools disable refused: no tool named %q in the registry", name)
		}
	}
	if _, err := persistToolFold(cfgPath, cfg.Disabled, name, enabled); err != nil {
		return err
	}
	fmt.Fprintf(w, "tool %s %s (config.yaml disabled.tools)\n", name, stateWord(enabled))
	return nil
}

// persistToolFold applies one fold edit through the one writer (writeDisabled).
func persistToolFold(cfgPath string, fold DisabledNamespaces, name string, enabled bool) (bool, error) {
	next, changed := toggleToolFold(fold.Tools, name, enabled)
	if !changed {
		return false, nil
	}
	fold.Tools = next
	if err := writeDisabled(cfgPath, fold); err != nil {
		return false, err
	}
	return true, nil
}

// toggleToolFold returns the next sorted fold list and whether it changed.
func toggleToolFold(cur []string, name string, enabled bool) ([]string, bool) {
	next := slices.Clone(cur)
	if enabled {
		if !slices.Contains(next, name) {
			return cur, false
		}
		return slices.DeleteFunc(next, func(n string) bool { return n == name }), true
	}
	if slices.Contains(next, name) {
		return cur, false
	}
	next = append(next, name)
	sort.Strings(next)
	return next, true
}

// toolInInfos reports whether the mounted inventory registers name. The
// check is the full registry (every extension's ToolList), never the
// fold-filtered union - a disabled tool must stay disable-able/known.
func toolInInfos(infos []core.ExtensionInfo, name string) bool {
	for _, info := range infos {
		for _, t := range info.ToolList {
			if t.Name == name {
				return true
			}
		}
	}
	return false
}

// stateWord is the printed state of an enable flag.
func stateWord(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}

// stateVerb is the verb that produces that state.
func stateVerb(enabled bool) string {
	if enabled {
		return "enable"
	}
	return "disable"
}
