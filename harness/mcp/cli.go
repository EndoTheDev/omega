package mcp

// cli.go: the mcp subcommand (card t_2a649d87) - `omega mcp
// list|add|edit|delete`, the terminal face of the same entry CRUD the
// HTTP routes serve. The capsule registers the name at mount
// (RegisterSubcommand, v0.12 phase D Q6: the module owns its argv),
// exactly like crons and skills.
//
// Every write rides saveServerAndReload / deleteServerAndReload - the
// SAME law POST /mcp/save and POST /mcp/delete ride (validate, node
// splice, atomic write, bridge swap). The CLI adds no second writer:
// `omega mcp add` and a wire save land byte-identically in mcp.yaml.
//
// Verb grammar is the crons card's (t_aca45258, reviewed and merged):
// the bare word lists, the name is the first positional of every write
// verb (Go's flag package stops at the first non-flag argument, so a
// trailing name would swallow the flags), edit applies only the flags
// actually passed, and a leading-dash first argument is a usage error -
// never a guessed name.

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
)

// mcpSummary is the one-line entry the global `omega --help` module
// section shows for this capsule (the listing is registry
// introspection).
const mcpSummary = "mcp servers: list, add, edit, delete (see: omega mcp --help)"

// mcpHelp is the module's own help: `omega mcp --help` prints it.
const mcpHelp = `omega mcp - Model Context Protocol server entries in mcp.yaml

Usage:
  omega mcp                          list every server (same as "omega mcp list")
  omega mcp list                     server table: name, transport, state, tools, error
  omega mcp add <name> [flags]       create a server entry
  omega mcp edit <name> [flags]      change the named entry (only the flags you pass)
  omega mcp delete <name>            remove an entry (its connections close on the swap)

Entry flags (add/edit):
  --command <path>     stdio transport: the MCP server executable
  --arg <value>        one stdio argument (repeat the flag; replaces the list on edit)
  --env <KEY=VALUE>    one stdio env var (repeat; edit merges key by key)
  --url <url>          http transport: the Streamable HTTP endpoint (http/https)
  --header <K:V>       one http header (repeat; edit merges key by key)
  --enabled[=false]    whether the bridge connects this server (add default: true)

A command entry and a url entry are exclusive; args/env belong to stdio,
headers to http. Writes land in the mcp.yaml the loader reads (comments and
style survive the splice), atomically, then the live bridge swaps - no
restart. Adding on a home with no mcp.yaml creates <home>/config/mcp.yaml.
`

// cmdMCP builds the subcommand handler. The closure keeps the mounted
// plugin - the same bridge instance the routes and the tool provider
// read.
func cmdMCP(p *Plugin) func([]string) error {
	return func(args []string) error {
		if len(args) == 0 {
			return mcpList(p, os.Stdout)
		}
		sub, rest := args[0], args[1:]
		switch sub {
		case "list":
			if len(rest) != 0 {
				return fmt.Errorf("mcp list: unexpected argument %q (list takes none, omega mcp --help lists them)", rest[0])
			}
			return mcpList(p, os.Stdout)
		case "add":
			return mcpAdd(p, rest)
		case "edit":
			return mcpEdit(p, rest)
		case "delete":
			return mcpDelete(p, rest)
		case "--help", "-h", "help":
			fmt.Print(mcpHelp)
			return nil
		default:
			fmt.Fprint(os.Stderr, mcpHelp)
			return fmt.Errorf("unknown mcp subcommand %q", sub)
		}
	}
}

// mcpList renders the server table from the LIVE bridge - the same
// StatusReport GET /mcp/servers answers, so the terminal and the MCP
// tab speak one table.
func mcpList(p *Plugin, w io.Writer) error {
	rep := StatusReport{Source: "none", Servers: []ServerStatus{}}
	if active := p.activeBridge(); active != nil {
		rep = active.Status()
	}
	if len(rep.Servers) == 0 {
		fmt.Fprintf(w, "[mcp] no servers configured (source: %s)\n", rep.Source)
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tTRANSPORT\tENABLED\tSTATE\tTOOLS\tERROR")
	for _, s := range rep.Servers {
		state := "connected"
		switch {
		case !s.Enabled:
			state = "disabled"
		case s.Error != "":
			state = "error"
		}
		tools := "-"
		if len(s.Tools) != 0 {
			tools = strings.Join(s.Tools, ",")
		}
		errText := s.Error
		if errText == "" {
			errText = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%v\t%s\t%s\t%s\n", s.Name, s.Transport, s.Enabled, state, tools, errText)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(w, "(%d server(s), source: %s)\n", len(rep.Servers), rep.Source)
	return nil
}

// listValue collects a repeatable string flag (--arg, --env, --header).
type listValue []string

func (l *listValue) String() string { return strings.Join(*l, ",") }

func (l *listValue) Set(v string) error {
	*l = append(*l, v)
	return nil
}

// mcpFlags is the parsed flag set of add/edit. enabled defaults to true
// (a fresh entry connects); edit only applies it when the flag was
// passed (flag.Visit).
type mcpFlags struct {
	command string
	args    listValue
	env     listValue
	url     string
	headers listValue
	enabled bool
}

func parseMCPArgs(args []string) (*mcpFlags, []string, *flag.FlagSet, error) {
	f := &mcpFlags{enabled: true}
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&f.command, "command", "", "stdio server executable")
	fs.Var(&f.args, "arg", "stdio argument (repeatable)")
	fs.Var(&f.env, "env", "stdio env var KEY=VALUE (repeatable)")
	fs.StringVar(&f.url, "url", "", "http endpoint (http/https)")
	fs.Var(&f.headers, "header", "http header K:V (repeatable)")
	fs.BoolVar(&f.enabled, "enabled", true, "whether the bridge connects this server")
	err := fs.Parse(args)
	if err != nil {
		return nil, nil, nil, err
	}
	return f, fs.Args(), fs, nil
}

// serverName peels the one positional a write verb needs (the crons
// jobName law: name first, leftovers are a usage error).
func serverName(verb string, args []string) (string, []string, error) {
	if len(args) == 0 {
		return "", nil, fmt.Errorf("usage: omega mcp %s <name> [flags] (omega mcp --help lists them)", verb)
	}
	if strings.HasPrefix(args[0], "-") {
		return "", nil, fmt.Errorf("usage: omega mcp %s <name> [flags] - the server name comes first", verb)
	}
	return args[0], args[1:], nil
}

// splitPairs turns repeated KEY=VALUE / K:V flags into a map. A value
// without the separator is a usage error naming the flag - never a
// silent drop.
func splitPairs(flagName, sep string, values []string) (map[string]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(values))
	for _, v := range values {
		k, val, ok := strings.Cut(v, sep)
		k = strings.TrimSpace(k)
		if !ok || k == "" {
			return nil, fmt.Errorf("--%s %q: expected KEY%sVALUE", flagName, v, sep)
		}
		out[k] = val
	}
	return out, nil
}

// mcpAdd creates one entry. The schema law is validateServerEntry's
// (called by upsertServerEntry): a missing transport, both transports,
// a bad url - every rejection names the field and nothing reaches the
// disk.
func mcpAdd(p *Plugin, args []string) error {
	name, flagArgs, err := serverName("add", args)
	if err != nil {
		return err
	}
	f, pos, _, err := parseMCPArgs(flagArgs)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return fmt.Errorf("mcp add: unexpected argument %q (only the name is positional)", pos[0])
	}
	env, err := splitPairs("env", "=", f.env)
	if err != nil {
		return err
	}
	headers, err := splitPairs("header", ":", f.headers)
	if err != nil {
		return err
	}
	enabled := f.enabled
	sc := serverConfig{
		Name:    name,
		Command: f.command,
		Args:    f.args,
		Env:     env,
		URL:     f.url,
		Headers: headers,
		Enabled: &enabled,
	}
	rep, writeErr, reloadErr := p.saveServerAndReload(sc)
	if writeErr != nil {
		return fmt.Errorf("mcp add %s: %w", name, writeErr)
	}
	if reloadErr != nil {
		return fmt.Errorf("mcp add %s: reload: %w", name, reloadErr)
	}
	printMCPSaved(rep, name)
	return nil
}

// mcpEdit changes the named entry. The current definition is the base -
// only flags the operator actually passed are applied, so `omega mcp
// edit docs --enabled=false` never rewrites the command or the args.
// Repeatable flags: --arg replaces the whole argument list, --env and
// --header merge key by key into the existing maps.
func mcpEdit(p *Plugin, args []string) error {
	name, flagArgs, err := serverName("edit", args)
	if err != nil {
		return err
	}
	f, pos, fs, err := parseMCPArgs(flagArgs)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return fmt.Errorf("mcp edit: unexpected argument %q (only the name is positional)", pos[0])
	}
	existing, ok := p.ServerEntry(name)
	if !ok {
		return fmt.Errorf("mcp edit: no server %q in the live config", name)
	}
	sc := existing
	changed := false
	var editErr error
	fs.Visit(func(fl *flag.Flag) {
		changed = true
		switch fl.Name {
		case "command":
			sc.Command = f.command
		case "arg":
			sc.Args = append([]string(nil), f.args...)
		case "env":
			env, err := splitPairs("env", "=", f.env)
			if err != nil {
				editErr = err
				return
			}
			merged := make(map[string]string, len(sc.Env)+len(env))
			for k, v := range sc.Env {
				merged[k] = v
			}
			for k, v := range env {
				merged[k] = v
			}
			sc.Env = merged
		case "url":
			sc.URL = f.url
		case "header":
			headers, err := splitPairs("header", ":", f.headers)
			if err != nil {
				editErr = err
				return
			}
			merged := make(map[string]string, len(sc.Headers)+len(headers))
			for k, v := range sc.Headers {
				merged[k] = v
			}
			for k, v := range headers {
				merged[k] = v
			}
			sc.Headers = merged
		case "enabled":
			enabled := f.enabled
			sc.Enabled = &enabled
		}
	})
	if editErr != nil {
		return editErr
	}
	if !changed {
		return fmt.Errorf("mcp edit %s: nothing to change - pass at least one entry flag (omega mcp --help lists them)", name)
	}
	rep, writeErr, reloadErr := p.saveServerAndReload(sc)
	if writeErr != nil {
		return fmt.Errorf("mcp edit %s: %w", name, writeErr)
	}
	if reloadErr != nil {
		return fmt.Errorf("mcp edit %s: reload: %w", name, reloadErr)
	}
	printMCPSaved(rep, name)
	return nil
}

// mcpDelete removes one entry. The returned report is the landed
// truth; the confirmation counts what remains.
func mcpDelete(p *Plugin, args []string) error {
	name, rest, err := serverName("delete", args)
	if err != nil {
		return err
	}
	if len(rest) != 0 {
		return fmt.Errorf("mcp delete: unexpected argument %q (only the name is positional)", rest[0])
	}
	rep, writeErr, reloadErr := p.deleteServerAndReload(name)
	if writeErr != nil {
		return fmt.Errorf("mcp delete %s: %w", name, writeErr)
	}
	if reloadErr != nil {
		return fmt.Errorf("mcp delete %s: reload: %w", name, reloadErr)
	}
	fmt.Printf("deleted server %q; %d server(s) remain\n", name, len(rep.Servers))
	return nil
}

// printMCPSaved reports the landed row (the same truth the HTTP route
// answers): transport, enablement, connection state and tool count.
func printMCPSaved(rep StatusReport, name string) {
	for _, s := range rep.Servers {
		if s.Name != name {
			continue
		}
		state := "connected"
		switch {
		case !s.Enabled:
			state = "disabled"
		case s.Error != "":
			state = "error: " + s.Error
		}
		fmt.Printf("saved server %q: transport=%s, enabled=%v, state=%s, tools=%d\n",
			s.Name, s.Transport, s.Enabled, state, len(s.Tools))
		return
	}
	fmt.Printf("saved server %q\n", name)
}
