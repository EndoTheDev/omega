// Command omega is the single binary entry point for the omega agent.
// It ties together the serve, run, and health subcommands.
package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/agent/loop"
	"github.com/EndoTheDev/omega/api"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/approval"
	browserModule "github.com/EndoTheDev/omega/harness/browser"
	"github.com/EndoTheDev/omega/harness/compactor"
	"github.com/EndoTheDev/omega/harness/continuous"
	"github.com/EndoTheDev/omega/harness/cron"
	"github.com/EndoTheDev/omega/harness/datadir"
	"github.com/EndoTheDev/omega/harness/delegate"
	"github.com/EndoTheDev/omega/harness/files"
	kanbanModule "github.com/EndoTheDev/omega/harness/kanban"
	"github.com/EndoTheDev/omega/harness/logging"
	"github.com/EndoTheDev/omega/harness/mcp"
	"github.com/EndoTheDev/omega/harness/memory"
	"github.com/EndoTheDev/omega/harness/notifications"
	"github.com/EndoTheDev/omega/harness/observability"
	"github.com/EndoTheDev/omega/harness/prompt"
	"github.com/EndoTheDev/omega/harness/provider"
	"github.com/EndoTheDev/omega/harness/skills"
	"github.com/EndoTheDev/omega/harness/store"
	"github.com/EndoTheDev/omega/harness/terminal"
	"github.com/EndoTheDev/omega/harness/tools"
	"github.com/EndoTheDev/omega/harness/trust"
	"github.com/EndoTheDev/omega/harness/verdict"
	"github.com/EndoTheDev/omega/llm"
)

func main() {
	home := omegaHome()
	// Stray guard (finding C5): a home that IS a checkout root gets one
	// visible warning BEFORE bootstrapHome writes the examples into it.
	// Hint, never a block - home resolution is unchanged.
	exeDir := ""
	if exe, err := os.Executable(); err == nil {
		exeDir = filepath.Dir(exe)
	}
	printRepoHomeHint(home, exeDir)
	// first-run bootstrap: write the example configs into the home so
	// any install (release folder, go install, dotfolder) is self-documenting
	bootstrapHome(home)
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "omega:", err)
		os.Exit(1)
	}
}

// repoHomeHint is the first-user stray guard (finding C5). Running the
// binary from a checkout root resolves home = the exe's own directory,
// so every omega write (data/, omega.log, the config examples) lands in
// the work tree - the observed omega.exe/omega.log residue a first user
// hits. The default resolution stays exactly as it is: a release-folder
// home beside the binary is legitimate, so this only warns. Returns ""
// when there is nothing worth saying.
//
// The condition is deliberately narrow. home == exe dir excludes an
// explicit OMEGA_HOME (an operator who points home at a checkout chose
// that), and home holding .git excludes the dev lane's bin/ home
// (inside the work tree, never its root) and every prod home.
func repoHomeHint(home, exeDir string) string {
	if home == "" || exeDir == "" {
		return ""
	}
	if !strings.EqualFold(canonPath(home), canonPath(exeDir)) {
		return ""
	}
	if _, err := os.Stat(filepath.Join(home, ".git")); err != nil {
		return ""
	}
	return fmt.Sprintf("omega: home is the repo checkout root (%s) - omega writes its db, log and config examples there, dirtying the tree.\n"+
		"omega: build into bin/ instead (go build -o bin/omega.exe ./cmd/omega), or set OMEGA_HOME to move the home.", canonPath(home))
}

// printRepoHomeHint writes repoHomeHint to stderr, once per process
// (main is the only caller).
func printRepoHomeHint(home, exeDir string) {
	if hint := repoHomeHint(home, exeDir); hint != "" {
		fmt.Fprintln(os.Stderr, hint)
	}
}

// helpText is the global --help output's static part: the built-in
// subcommands and flags. Module-owned subcommands are NOT listed here
// - the list is registry introspection (v0.12 phase D, Q6: the module
// owns its argv), appended by printModuleSubcommands so a module's
// absence makes its subcommand vanish from the help.
const helpText = `omega - a Go event-stream agent (Pi/Tau port)

Usage:
  omega                 interactive CLI loop (first run: walks you through setup)
  omega <path>          start the loop in <path> (chdir first)
  omega setup           write the first config (provider, model, api key)
  omega run <prompt>    run one prompt to stdout
  omega serve           start the HTTP server (SSE streaming; serves the webui)
  omega export <id|label>  export a session as JSONL
  omega insights [--days N]  show session usage analytics (default: 30 days)
  omega agents         list agents; ` + "`omega agents create <name>`" + ` scaffolds one
  omega tools          list wire tools; ` + "`omega tools disable <name>`" + ` takes one off the model
  omega settings [get [<key>]]   read config knobs (all, or one dotted key)
  omega settings set <key> <value>   write one knob (the webui's own validation)
  omega update              report N versions behind + prompt y/N; nothing
                            installs without an explicit y (or --yes)
  omega update --yes        install the latest stable release without a prompt
  omega update --nightly    check the newest -nightly.* prerelease channel instead
  omega update --rollback   swap back to omega.bak + verify it runs
  omega rollback            same as omega update --rollback
  omega health          check the server at the configured port

Flags:
  --config <path>       config file (default: <home>/config.yaml, or <home>/config/config.yaml)
  --append-system-prompt <text>   append to system prompt (repeatable)
  --approve             trust the current project's AGENTS.md
  --no-approve          skip the current project's AGENTS.md
  --version, -v         print version
  --help, -h            show this help

Update flags:
  --nightly             nightly channel: newest prerelease tag
  --allow-downgrade     permit installing a version lower than the running one
                        (a stale mirror must never quietly revert a box)
  --yes                 auto-accept the install prompt (unattended-safe: a
                        non-tty stdin or EOF always declines a bare update)
`

// run parses the subcommand and dispatches. The first non-flag argument
// selects the subcommand; --config is accepted before or after it. With
// no argument the CLI loop starts. A non-subcommand argument is treated
// as a project path: omega chdirs there and starts the loop. The setup
// gate is the law (card t_0744eb8e): serve and run refuse without a
// config; bare `omega` walks the wizard instead of refusing.
func run(args []string) error {
	// Set OMEGA_HOME early so all child processes inherit it.
	home := omegaHome()
	os.Setenv("OMEGA_HOME", home)
	// Agent-home migration (card t_72ecca67): the retired
	// agents/default profile's persona files move UP to the home root
	// (ruling 3) - the reverse of the t_7e2b7002 migration. Exactly
	// once, before anything reads them.
	if err := agent.MigrateAgentHomes(home); err != nil {
		return err
	}
	// The ruling-4 row migration right behind it: shared-store agent
	// rows move into their own homes before any store opens.
	if err := migrateAgentStores(home); err != nil {
		return err
	}
	// Set OMEGA_SKILLS_DIR so the skills extension can read skills.
	os.Setenv("OMEGA_SKILLS_DIR", home+"/skills")
	// Set OMEGA_BIN so the delegate extension can spawn subagents.
	if exe, err := os.Executable(); err == nil {
		os.Setenv("OMEGA_BIN", exe)
	}
	// Root flags answer for the root's own argv: the flags BEFORE a
	// subcommand, and the flags after a built-in subcommand (the root
	// switch owns those argv too - `omega serve --help` has always
	// printed the root help). A module subcommand owns its whole
	// trailing argv (Q6): `omega kanban --help` reaches the module,
	// not the root (t_ed045792 - the old whole-argv scan answered it
	// with the root help, breaking the very promise the help text
	// makes).
	lead := args
	for i, a := range args {
		if !strings.HasPrefix(a, "-") {
			lead = args[:i]
			break
		}
	}
	sub, rest := splitSubcommand(args)
	flagScope := args
	if sub != "" && !isRootSubcommand(sub) {
		flagScope = lead
	}
	for _, a := range flagScope {
		switch a {
		case "--help", "-h":
			printHelp(args)
			return nil
		case "--version", "-v":
			fmt.Println("omega", omegaVersion)
			return nil
		}
	}
	appendPrompts := parseAppendPrompts(rest)
	trust := parseTrustArgs(rest)
	configPath := parseConfigFlag(rest)
	switch sub {
	case "setup":
		return cmdSetup()
	case "serve":
		if err := requireConfig(configPath); err != nil {
			return err
		}
		return cmdServe(parseConfigFlag(rest), appendPrompts, trust)
	case "run":
		if err := requireConfig(configPath); err != nil {
			return err
		}
		return cmdRun(parseConfigFlag(rest), rest, trust)
	case "health":
		return cmdHealth(parseConfigFlag(rest))
	case "export":
		return cmdExport(parseConfigFlag(rest), rest)
	case "insights":
		return cmdInsights(parseConfigFlag(rest), rest)
	case "agents":
		return cmdAgents(rest)
	case "tools":
		// `omega tools` owns its own argv, `--help` included: it is the
		// crons grammar's sibling (list/enable/disable) and behaves like
		// a module subcommand at the flag level - see isRootSubcommand.
		return cmdTools(rest)
	case "settings":
		return cmdSettings(parseConfigFlag(rest), rest)
	case "update":
		return cmdUpdate(rest)
	case "rollback":
		// Alias for `omega update --rollback` — same one-generation
		// swap back, health-verified.
		return cmdRollback()
	case "chat", "":
		// The chat subcommand is a legacy alias for the CLI loop.
		return cmdLoop(parseConfigFlag(rest), appendPrompts, trust)
	default:
		// An existing directory stays the project path - decided before
		// the module table, an existing directory is always a workspace.
		if info, serr := os.Stat(sub); serr == nil && info.IsDir() {
			if err := os.Chdir(sub); err != nil {
				return fmt.Errorf("chdir %s: %w", sub, err)
			}
			return cmdLoop(parseConfigFlag(rest), appendPrompts, trust)
		}
		// Module-owned subcommands (v0.12 phase D, Q6 - CLI homecoming):
		// the dispatch table is the mount registry, the module owns its
		// argv. No module claims the name: fall back to the project-path
		// behavior (chdir there, then the loop).
		if handled, err := dispatchModuleCLI(args, sub, rest); handled {
			return err
		}
		if err := os.Chdir(sub); err != nil {
			return fmt.Errorf("chdir %s: %w", sub, err)
		}
		return cmdLoop(parseConfigFlag(rest), appendPrompts, trust)
	}
}

// isRootSubcommand reports whether sub is a built-in switch case in
// run() (the root's own argv) rather than a module-registered or
// project-path name. The list must stay in sync with run()'s switch:
// a built-in keeps the root's flag short-circuit on its trailing
// argv, while a module subcommand owns the whole tail (Q6).
//
// `tools` is a built-in that is deliberately ABSENT here: it carries a
// module subcommand's flag contract (its own `--help`, its own argv
// parse) while living in cmd/omega, where the one config.yaml writer
// (writeDisabled) already sits. Listing it would make `omega tools
// --help` print the root help and break the grammar it shares with
// `omega crons`.
func isRootSubcommand(sub string) bool {
	switch sub {
	case "setup", "serve", "run", "health", "export", "insights",
		"agents", "settings", "update", "rollback", "chat":
		return true
	}
	return false
}

// printHelp prints the static help plus the module-owned subcommand
// lines. The listing is registry introspection (settings-list law), so
// a removed module's subcommand vanishes from `omega --help`. The
// mount is best effort: help never fails, a broken home just loses
// the module section (the static part always prints first).
func printHelp(args []string) {
	fmt.Print(helpText)
	subs := []agent.Subcommand{}
	if reg, pctx, err := mountRegistry(configForCLI(args)); err == nil {
		// The mount is borrowed for the listing only: dismount before
		// returning - the capsules' stores hold open file handles a
		// windows process leaves behind otherwise (agent.Dismounter).
		defer agent.DismountAll(reg, pctx)
		subs = reg.Subcommands()
	}
	printModuleSubcommands(subs, os.Stdout)
}

// printModuleSubcommands renders the module section of the global
// help - one line per registered subcommand, name-sorted. An empty
// table prints no section: a binary with no module CLIs looks exactly
// like the pre-phase-D help.
func printModuleSubcommands(subs []agent.Subcommand, w io.Writer) {
	if len(subs) == 0 {
		return
	}
	sort.Slice(subs, func(i, j int) bool { return subs[i].Name < subs[j].Name })
	width := len("kanban") + 2 // minimum: the description starts past the longest name today
	for _, s := range subs {
		if len(s.Name)+2 > width {
			width = len(s.Name) + 2
		}
	}
	fmt.Fprintf(w, "\nModule subcommands (a removed module's subcommand vanishes):\n")
	for _, s := range subs {
		fmt.Fprintf(w, "  omega %-*s %s\n", width, s.Name, s.Summary)
	}
}

// mountRegistry builds and mounts the plugin set with the API mount,
// returning the registration registry (the module CLI dispatch table
// is registry introspection) and the mounted context. The context is
// returned so a caller can drain seams; the CLI dispatch only needs
// the registry.
func mountRegistry(cfg Config) (*agent.Registry, *agent.Context, error) {
	plugins, err := buildPlugins(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("build plugins: %w", err)
	}
	pctx := &agent.Context{
		CWD:     cwd(),
		Configs: agent.RouteConfigs(plugins, cfg),
		// The disabled.tools fold rides the borrowed mount exactly as it
		// rides the serve boot (newAgent): a CLI read of the registry
		// shows the state the next boot will honor.
		DisabledTools: cfg.Disabled.Tools,
	}
	reg, err := agent.MountAllAPI(plugins, pctx, nil, cfg.Disabled.Modules)
	if err != nil {
		return nil, nil, fmt.Errorf("mount extensions: %w", err)
	}
	return reg, pctx, nil
}

// configForCLI resolves the config for a module CLI dispatch. A
// module subcommand keeps the old CLI contract - it never required a
// (valid) config.yaml, the capsules run on their own config sections -
// so a missing home or a broken global config falls back to the
// defaults instead of failing the dispatch. A bad --config path
// behaves the same: the module sections come up on their defaults.
func configForCLI(args []string) Config {
	cfg, err := LoadConfig(resolveConfigPath(parseConfigFlag(args)))
	if err != nil {
		return DefaultConfig()
	}
	return cfg
}

// dispatchModuleCLI mounts the capsules and dispatches a module-owned
// CLI subcommand. Returns handled=false when no module claims the
// name. The module handler receives the argv after the subcommand
// untouched - the module parses its own argv (Q6).
func dispatchModuleCLI(args []string, sub string, rest []string) (bool, error) {
	reg, pctx, err := mountRegistry(configForCLI(args))
	if err != nil {
		return false, err
	}
	handled, derr := dispatchSubcommand(reg, sub, rest)
	// The mount is borrowed for one dispatch: tear it down before
	// returning (agent.Dismounter) - a live store handle outlives the
	// short-lived CLI registry otherwise, and windows keeps the data
	// file locked to the dead handle. The command result is the
	// contract; the teardown's error never eats it.
	_ = agent.DismountAll(reg, pctx)
	return handled, derr
}

// dispatchSubcommand consults the registry's cli table. Linear scan:
// module subcommands are a handful at most.
func dispatchSubcommand(reg *agent.Registry, sub string, rest []string) (bool, error) {
	for _, c := range reg.Subcommands() {
		if c.Name == sub {
			return true, c.Handler(rest)
		}
	}
	return false, nil
}

// splitSubcommand returns the first non-flag argument as the subcommand
// and every other argument as rest - leading flags included, so
// --config/--approve/--no-approve work before a subcommand or project
// path exactly as the help text promises. With no subcommand at all
// (the bare REPL entry) rest is the full flag list: the bare entry must
// parse trust flags identically to subcommand paths (card t_18b13390 -
// `omega --approve` used to hand the loop an empty rest and the flag
// silently died).
func splitSubcommand(args []string) (string, []string) {
	for i, a := range args {
		if !strings.HasPrefix(a, "-") {
			rest := make([]string, 0, len(args)-1)
			rest = append(rest, args[:i]...)
			rest = append(rest, args[i+1:]...)
			return a, rest
		}
	}
	return "", args
}

// parseConfigFlag extracts the value of --config from args, or "" if
// absent. It is the only flag the CLI takes, so a manual scan is the
// laziest correct parse.
func parseConfigFlag(args []string) string {
	for i, a := range args {
		if a == "--config" && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(a, "--config=") {
			return strings.TrimPrefix(a, "--config=")
		}
	}
	return ""
}

// stripConfigFlag removes --config and its value from args, so the
// remaining arguments are the run prompt.
func stripConfigFlag(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if args[i] == "--config" {
			i++ // skip the value
			continue
		}
		if strings.HasPrefix(args[i], "--config=") {
			continue
		}
		out = append(out, args[i])
	}
	return out
}

// parseAppendPrompts extracts all --append-system-prompt values from
// args. Supports both --append-system-prompt "text" and
// --append-system-prompt="text" forms. Repeatable.
func parseAppendPrompts(args []string) []string {
	var prompts []string
	for i := 0; i < len(args); i++ {
		if args[i] == "--append-system-prompt" && i+1 < len(args) {
			prompts = append(prompts, args[i+1])
			i++
			continue
		}
		if strings.HasPrefix(args[i], "--append-system-prompt=") {
			prompts = append(prompts, strings.TrimPrefix(args[i], "--append-system-prompt="))
		}
	}
	return prompts
}

// stripAppendPrompts removes --append-system-prompt and its values from
// args, so the remaining arguments are the run prompt.
func stripAppendPrompts(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if args[i] == "--append-system-prompt" {
			i++ // skip the value
			continue
		}
		if strings.HasPrefix(args[i], "--append-system-prompt=") {
			continue
		}
		out = append(out, args[i])
	}
	return out
}

// canonPath is the boundary's separator policy (card t_630e595d, Endo's
// ruling on t_fb4cb3d2): every path this package CONSTRUCTS - the omega
// home, the resolved defaults off it, agent-layer joins, the resolved
// config path - carries forward-slash separators, one spelling. A raw
// windows join otherwise leaves mixed shapes ("C:\...\001/data/omega.db")
// in config.yaml, the db dsns, and the boot warnings. Explicitly
// configured values pass through untouched: the boundary canonizes what
// it builds, never what the operator wrote.
func canonPath(p string) string {
	return filepath.ToSlash(p)
}

// omegaHome returns the omega home directory: OMEGA_HOME env var,
// the binary's directory (release-folder installs), or ~/.omega for
// bin-dir installs (see homeForExe). This is where config, db, and
// skills live. The result is canon spelling (canonPath).
func omegaHome() string {
	exePath := ""
	if exe, err := os.Executable(); err == nil {
		exePath = exe
	}
	userHome, _ := os.UserHomeDir()
	return canonPath(homeForExe(os.Getenv("OMEGA_HOME"), exePath, userHome))
}

// resolveConfigPath returns the --config value, or the home config.yaml
// when it exists, or "" to skip YAML entirely. The legacy home-root
// location is checked first (existing installs keep working unchanged);
// the config/ subdirectory (Endo 2026-10-18 layout) is the fallback.
func resolveConfigPath(flagPath string) string {
	if flagPath != "" {
		return flagPath
	}
	home := omegaHome()
	for _, p := range []string{home + "/config.yaml", home + "/config/config.yaml"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// requireConfig is the setup gate (card t_0744eb8e): config-dependent
// subcommands refuse in a home without a valid config.yaml and point at
// the wizard - the gate is the message, not a stack trace.
func requireConfig(configPath string) error {
	path := resolveConfigPath(configPath)
	if path == "" {
		return fmt.Errorf("no config found in %s - run `omega setup` first", omegaHome())
	}
	if _, err := LoadConfig(path); err != nil {
		return fmt.Errorf("%s: %w - fix it or run `omega setup`", path, err)
	}
	return nil
}

// resolveHomePaths fills in home-relative defaults for DBPath and
// Skills.Dir when the config left them at their relative defaults.
// This lets omega find its db and skills from any CWD. Returns an
// error if the home directory cannot be created.
func resolveHomePaths(cfg *Config) error {
	home := omegaHome()
	if cfg.Store.DBPath == "omega.db" {
		cfg.Store.DBPath = canonPath(datadir.DBPath(home, "omega.db"))
	}
	if cfg.Kanban.DBPath == "kanban.db" {
		// one kanban.db owns all boards (t_feae19d3): the pointer file
		// is retired, resolution is the single database
		cfg.Kanban.DBPath = canonPath(datadir.DBPath(home, "kanban.db"))
	}
	if cfg.Skills.Dir == "skills" {
		cfg.Skills.Dir = home + "/skills"
	}
	if cfg.Memory.File == "memory.md" {
		// card t_72ecca67 ruling 3: main's persona files live at the
		// home root (agents/default is retired and its files migrated
		// UP by MigrateAgentHomes in run(), before any command).
		cfg.Memory.File = home + "/memory.md"
	}
	if cfg.Memory.UserProfileFile == "user.md" {
		cfg.Memory.UserProfileFile = home + "/user.md"
	}
	if cfg.Logging.File == "" || cfg.Logging.File == "omega.log" {
		// "" is the construction default; the bare "omega.log" literal
		// is a legacy alias from configs written before the default went
		// empty. Both mean the home-rooted log - never a CWD-relative
		// file (card t_0ccda501).
		cfg.Logging.File = home + "/omega.log"
	}
	if err := os.MkdirAll(home, 0755); err != nil {
		return fmt.Errorf("create omega home %s: %w", home, err)
	}
	return nil
}

// buildPlugins creates the list of in-process extensions from config.
// Extensions are compiled into omega — config controls their settings,
// not whether they're loaded. Each plugin reads its config from
// ctx.Configs during Mount.
func buildPlugins(cfg Config) ([]agent.Plugin, error) {
	mcpPlugin, err := mcp.NewPluginFromEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "omega: mcp bridge: %v\n", err)
		mcpPlugin = mcp.NewPlugin(nil)
	}
	return []agent.Plugin{
		logging.NewPlugin(),
		loop.NewPlugin(),
		&provider.Plugin{},
		store.NewPlugin(),
		skills.NewPlugin(),
		compactor.NewPlugin(),
		memory.NewPlugin(),
		// card t_13a2af8a: mount order is declared data - the
		// notifications consumers (delegate, kanban, cron) carry
		// Requires edges naming the seam, and agent.MountAllAPI's topoSort
		// is the only ordering. Slice order below is a stable tiebreak
		// only (it keeps prompt sections in authoring order among
		// independent plugins; it carries no dependency semantics).
		notifications.NewPlugin(),
		prompt.NewPlugin(),
		tools.NewPlugin(),
		// card t_48eeb015 (the shell-ring shrink wave 1): the file
		// domain leaves the tools capsule - the files capsule is the
		// files tool + the /files explorer routes (Toggleable: the
		// disabled fold may name it), and the shell tool rides the
		// terminal capsule's package (the builtin shell plugin). Both
		// contribute the same additive tools rail the registry owns.
		files.NewPlugin(),
		terminal.NewShellPlugin(),
		// The process registry does not exist as its own module any
		// more (card t_5a49790b): the registry machinery lives in
		// harness/terminal underneath the one shell domain
		// (shell.run/list/read/kill).
		// card t_2ecf9c51: verdict cards - verdict tool, the
		// verdicts seam, and the /verdict command. Requires store
		// (the hub captures it at Mount to persist card + verdict rows);
		// placed after the tools module so the prompt section lands
		// after the tools list.
		verdict.NewPlugin(),
		// card t_c2b52cb5: the smart approval gate - risky shell
		// commands reach the decision model + deterministic ladder.
		// Requires verdicts (an escalation hard-pauses on a card);
		// mode off mounts nothing (Q10).
		approval.NewPlugin(),
		mcpPlugin,
		delegate.NewPlugin(),
		api.NewPlugin(),
		trust.NewPlugin(),
		cron.NewPlugin(),
		kanbanModule.NewPlugin(),
		// The browser capsule (card t_95c91f9c, the general web browser
		// pane): the tab's last-URL memory, the only server surface the
		// client-side pane drives.
		browserModule.NewPlugin(),
		// The continuous capsule (card t_72ecca67): agent homes, the
		// wake subprocess, the canonical chats, the evidence ledger.
		// The home passes through the host - exe-dir resolution is
		// cmd/omega business, the capsule never re-guesses it.
		continuous.NewPluginWithHome(omegaHome()),
		// The terminal capsule (card t_55a3fc4b, Endo's ruling
		// 2026-11-01): the webui's integrated shell - pty routes +
		// lifecycle fence, no seams, no config, no gates. The spawn
		// fallback is the process's working directory; the
		// default_workspace knob rides the constructor and the
		// hot-apply hook (card t_62d72028).
		terminal.NewPlugin(cwd(), cfg.Server.DefaultWorkspace),
		// The observability capsule (card t_8de82cff): the structured
		// run-event ring + the omega.log tail behind the webui logging
		// tab. No seam, no tool, no config - two read-only GETs, and a
		// spine subscription the capsule owns.
		observability.NewPlugin(),
	}, nil
}

// buildConfigs and webBaseURL moved out in arc 2b (t_889388cc): the
// plugins declare their config section (agent.Describer's ConfigKey),
// agent.RouteConfigs builds the per-plugin map from the raw Config
// via (*Config).ConfigSection in config.go.

// newAgent wires config into a provider, agent, store, and extensions
// via the in-process plugin system. The store and plugin context are
// returned so the caller can close the store and start channels. The
// mount registry rides along for the E2 module-toggle surface.
func newAgent(cfg Config, configPath string, appendPrompts []string, trust trustFlags) (*agent.Agent, agent.StoreProvider, agent.LoggerProvider, *agent.Context, *agent.Registry, func([]string), func(string), func(string), func(string), func(string, int), error) {
	plugins, err := buildPlugins(cfg)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("build plugins: %w", err)
	}
	ctx := &agent.Context{
		CWD:     cwd(),
		Configs: agent.RouteConfigs(plugins, cfg),
		// v0.14.1: the disabled.tools fold rides the Context - every
		// NewFromContext agent installs the guard, every live toggle
		// rewrites it here before the serving surface re-reads.
		DisabledTools: cfg.Disabled.Tools,
	}
	// The API mount honors the config.yaml disabled.modules toggle list
	// (v0.12 E1, Q1): disabled capsules skip Mount, and a capsule
	// depending on one fails the boot naming the edge (Q3).
	reg, err := agent.MountAllAPI(plugins, ctx, nil, cfg.Disabled.Modules)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("mount extensions: %w", err)
	}
	// The registry-toggle adapter needs the one continuous plugin
	// instance the mount created (agent soft-toggles live-swap its
	// machine); a second buildPlugins would hand back a different one.
	// The hook threads as a func, never as the capsule's type - the
	// nm-purity scrub deletes the whole strand when the capsule leaves,
	// and the host keeps a nil hook (nm-purity scrub shape).
	var setAgents func([]string)
	var setFilesWorkspace func(string)
	// setChatWorkspace is the HTTP channel plugin's default-workspace
	// hot knob (card t_ccf74d88): the settings pane and the config
	// watcher push a changed default_workspace onto the channel the
	// same way the files capsule's knob travels.
	var setChannelWorkspace func(string)
	// setTerminalWorkspace is the terminal capsule's default-workspace
	// hot knob (card t_62d72028): the spawn workspace chain reads the
	// knob per claim, so a settings save re-roots the NEXT spawned
	// shell live - the files capsule's knob, same discipline.
	var setTerminalWorkspace func(string)
	// setAgentPromptHook is the HTTP channel plugin's serving-agent
	// hot knob (card t_f78acfd6): the system_prompt / max_turns save
	// re-seeds every mounted bind's agent through it.
	var setAgentPromptHook func(string, int)
	for _, p := range plugins {
		if cp, ok := p.(*continuous.Plugin); ok {
			setAgents = cp.SetDisabledAgents
		}
		if fp, ok := p.(*files.Plugin); ok {
			setFilesWorkspace = fp.SetDefaultWorkspace
		}
		if hp, ok := p.(*api.Plugin); ok {
			setChannelWorkspace = hp.SetDefaultWorkspace
			setAgentPromptHook = hp.SetAgentPromptConfig
			// Per-agent run config (card t_a7fab745): the chat floor
			// serves an agent-owned session on that home's agent.
			hp.SetAgentRunFactory(agentRunFactory(configPath, appendPrompts, trust))
		}
		if tp, ok := p.(*terminal.Plugin); ok {
			setTerminalWorkspace = tp.SetDefaultWorkspace
		}
	}

	// The store resolves BEFORE the agent is built: the captioner
	// (vision_model knob) holds the caption sink from the start, and
	// NewFromContext copies ctx.Captioner onto every agent it creates.
	// Part 3: needs resolve by key from the universal registry; the
	// host's in-memory fallback is its own offer under the same key
	// when the store capsule is absent (the owner ledger records
	// "host"). Wrong shape is the loud named-need boot failure.
	logger, _ := ctx.Services().Resolve("logger").(agent.LoggerProvider)
	st2 := agent.StoreProvider(nil)
	if svc := ctx.Services().Resolve("store"); svc != nil {
		st, ok := svc.(agent.StoreProvider)
		if !ok {
			return nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("seam \"store\": the offered service (%T) does not satisfy the store need (wrong shape) - fix the offering capsule", svc)
		}
		st2 = st
	}
	if st2 == nil {
		st2, err = openDefaultSupply(ctx, logger)
		if err != nil {
			return nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, err
		}
	}
	wireCaptioner(cfg, ctx)

	ag := agent.NewFromContext(ctx, agent.AgentOptions{
		MaxTurns:      cfg.MaxTurns,
		MaxToolOutput: cfg.Compaction.MaxToolOutput,
		PromptCustom:  cfg.SystemPrompt,
		PromptAppend:  appendPrompts,
		PromptContext: trustGate(ctx, cwd(), trust, false),
		CWD:           cwd(),
	})

	// setAgentConfig is the runtime-knob hot hook (card t_f78acfd6):
	// ONE closure covers BOTH agents a settings save of
	// system_prompt / max_turns must reach - the host-owned agent
	// here (cron and the continuous carriers ride it via StartDeps)
	// and every mounted HTTP bind's serving agent (the channel builds
	// its own agent with empty AgentOptions, so the boot values reach
	// it only through the wire-time push the serve boot calls below);
	// the channel store doubles as the boot bake for a late wire.
	setAgentConfig := func(prompt string, maxTurns int) {
		if setAgentPromptHook != nil {
			setAgentPromptHook(prompt, maxTurns)
		}
		ag.SetPromptCustom(prompt)
		ag.SetMaxTurns(maxTurns)
	}

	return ag, st2, logger, ctx, reg, setAgents, setFilesWorkspace, setChannelWorkspace, setTerminalWorkspace, setAgentConfig, nil
}

// wireCaptioner builds the image captioner (vision_model knob) onto the
// context: a pinned vision-model provider copy, the store as the caption
// sink, and the logger. An unset knob (or a store without caption
// support) changes nothing. Called after the store resolves, before any
// agent is built from the context - serve, loop, and cron agents all
// inherit through NewFromContext.
func wireCaptioner(cfg Config, pctx *agent.Context) {
	if cfg.VisionModel == "" || pctx == nil {
		return
	}
	resolve := func(key string) any { return pctx.Services().Resolve(key) }
	var sink agent.CaptionStore
	if cs, ok := resolve("store").(agent.CaptionStore); ok {
		sink = cs
	}
	var prov agent.Provider
	if p, ok := resolve("provider").(agent.Provider); ok {
		prov = p
	}
	var lg agent.LoggerProvider
	if l, ok := resolve("logger").(agent.LoggerProvider); ok {
		lg = l
	}
	pctx.Captioner = provider.NewCaptioner(prov, cfg.VisionModel, sink, lg)
	if sink == nil && lg != nil {
		lg.Warnf("omega: vision_model set but the store does not support captions - captions cache in-process only")
	}
}

func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

// openDefaultSupply opens the substrate-class default SUPPLY when no
// store capsule offered the "store" seam (the three-layer sessions
// law, the shell-ring shrink wave 2 - ADR
// Workshop/omega-shell-ring-shrink.md). The capsule's mount resolves
// the same file its own way (config DBPath - datadir.AgentHome /
// DBPath); the supply fallback opens the same <home>/data/omega.db,
// never :memory: - a fresh chat boots and persists across a restart
// through this supply, only the management face (the capsule's tab +
// routes) is gone. The handle offers back under "store"/"host": the
// chat floor reads it exactly like a capsule-opened store, and the
// owner ledger records which side answered.
func openDefaultSupply(pctx *agent.Context, logger agent.LoggerProvider) (agent.StoreProvider, error) {
	dsn := store.DefaultSupplyPath()
	if logger != nil {
		logger.Printf("omega: no store capsule loaded - serving the default supply (%s)", dsn)
	}
	s, err := store.Open(dsn)
	if err != nil {
		return nil, fmt.Errorf("open default supply (%s): %w", dsn, err)
	}
	pctx.ApplyOffer("store", "host", s)
	return s, nil
}

// serveBinds renders the resolved HTTP bind strings for the startup
// banner: every configured bind joined to the resolved port, the
// loopback default standing in for an unset bind list - the same
// default the api capsule binds with (api/plugin.go).
func serveBinds(cfg Config) []string {
	port := cfg.Server.Port
	binds := cfg.Server.Bind
	if len(binds) == 0 {
		binds = []string{"127.0.0.1"}
	}
	out := make([]string, 0, len(binds))
	for _, b := range binds {
		out = append(out, fmt.Sprintf("%s:%d", b, port))
	}
	return out
}

// serveBanner is the boot line a person can read: version, the binds
// being served, the config file actually read, the data dir, and the
// model. One line, real values only. The model comes from the agent's
// own API - the host does not reach into provider config here (the
// nm-purity law: no new provider wire in the host shell).
func serveBanner(version string, channels int, binds []string, configPath, dataDir, model string) string {
	return fmt.Sprintf("omega %s · serving %d channel(s) on %s · config %s · data %s · model %s",
		version, channels, strings.Join(binds, ", "), configPath, dataDir, model)
}

// cmdServe loads config, wires the agent, and starts all mounted
// channels (HTTP, Discord, etc.) until a signal triggers graceful
// shutdown.
func cmdServe(configPath string, appendPrompts []string, trust trustFlags) error {
	cfgPath := resolveConfigPath(configPath)
	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		return err
	}
	if err := resolveHomePaths(&cfg); err != nil {
		return err
	}
	llm.SetHTTPTimeout(cfg.HTTPTimeout)
	ag, store, logger, pctx, reg, setAgents, setFilesWorkspace, setChannelWorkspace, setTerminalWorkspace, setAgentConfig, err := newAgent(cfg, configPath, appendPrompts, trust)
	if err != nil {
		return err
	}
	defer store.Close()
	if logger != nil {
		defer logger.Close()
	}

	ctx, stop := signalContext()
	defer stop()

	// Start all mounted channels. Each channel creates its own agents
	// from pctx as needed.
	if len(pctx.Channels) == 0 {
		return fmt.Errorf("no channels configured — mount a channel extension (e.g. http_channel)")
	}
	// Expose the config knobs to the webui settings pane, and hot-reload
	// non-disruptive knobs (http timeout, compaction) when config.yaml
	// changes on disk — hand edits and pane saves take the same path.
	configFile := resolveConfigPath(configPath)
	if configFile == "" {
		// Fresh install, no config.yaml anywhere: pane saves need a
		// real target - writing "" would drop a litter file in the
		// server's CWD and 400 every save. Default to the config/
		// layout; writeConfigFile creates parents.
		configFile = omegaHome() + "/config/config.yaml"
	}
	// The E2 module-toggle surface (Q7): the registry behind the seam,
	// arming the fence handles exactly like the boot-time StartAll
	// below, persisting the toggle list to the config.yaml disabled:
	// key boot reads. v0.14.1 adds the registry-toggle seam next to it:
	// the skills/tools/agents namespaces of the same fold, one shared
	// live fold pointer so a module toggle and a registry toggle can
	// never clobber each other's namespaces.
	fold := &DisabledNamespaces{
		Modules:    cfg.Disabled.Modules,
		SkillNames: cfg.Disabled.SkillNames,
		Tools:      cfg.Disabled.Tools,
		Agents:     cfg.Disabled.Agents,
	}
	// one mutex for BOTH toggle surfaces (gate must-fix 1): module and
	// registry toggles mutate the same fold pointer and write the same
	// config.yaml - one lock, injected into both adapters.
	foldMu := &sync.Mutex{}
	mods := &omegaModules{
		pctx:       pctx,
		reg:        reg,
		fold:       fold,
		mu:         foldMu,
		deps:       core.StartDeps{Ctx: pctx, Agent: ag, Store: store, Run: ctx},
		configPath: configFile,
		logger:     logger,
	}
	registries := &omegaRegistries{
		pctx:       pctx,
		reg:        reg,
		fold:       fold,
		mu:         foldMu,
		configPath: configFile,
		home:       omegaHome(),
		setAgents:  setAgents,
		logger:     logger,
	}
	// The workspace hot-apply pushes only on a value change (card
	// t_9825147c part 3): the watch tracks what the last reload saw,
	// boot started with the boot config's value.
	watchWorkspace := cfg.Server.DefaultWorkspace
	WatchConfig(configFile, func(newCfg Config) {
		hotApply(newCfg, watchWorkspace, pctx, setFilesWorkspace, setChannelWorkspace, setTerminalWorkspace, setAgentConfig)
		watchWorkspace = newCfg.Server.DefaultWorkspace
		registries.Sync(newCfg.Disabled) // hand edits follow the toggle path
	})
	// The serving agent's boot bake (card t_f78acfd6): the HTTP
	// channel builds its own agent with empty AgentOptions, so the
	// boot config's system_prompt and max_turns reach the serving
	// agent only through this pre-start push - the hook stores the
	// channel fields, wire applies them at start. Runs through the
	// same path the hot-apply uses.
	if setAgentConfig != nil {
		setAgentConfig(cfg.SystemPrompt, cfg.MaxTurns)
	}
	// The cron fire's cap, boot half (card t_e635df3a): the scheduler
	// bounds a hung turn with server.run_timeout and cannot see the
	// config file THIS process booted on (an explicit --config path,
	// or an exe-dir home without OMEGA_HOME), so the host pushes its
	// resolution before the fence starts the runner. The same helper
	// rides every settings save/reload.
	pushRunTimeout(pctx, cfg)
	errCh := make(chan error, len(pctx.Channels))
	for _, ch := range pctx.Channels {
		deps := core.ChannelDeps{
			Ctx:        pctx,
			Store:      store,
			Settings:   &omegaSettings{path: configFile, pctx: pctx, setFilesWorkspace: setFilesWorkspace, setChannelWorkspace: setChannelWorkspace, setTerminalWorkspace: setTerminalWorkspace, setAgentConfig: setAgentConfig, maxTurns: ag.MaxTurns},
			Modules:    mods,
			Registries: registries,
			Version:    omegaVersion,
		}
		go func(c core.Channel) {
			errCh <- c.Start(ctx, deps)
		}(ch)
	}

	// The fence (v0.12 E1): module background work starts here, after
	// the mount commit. The run group owns the Start/Stop handles the
	// capsules registered in Mount (cron first); the channels stay
	// host-started - shell, no module handles.
	if err := pctx.RunGroup().StartAll(ctx, core.StartDeps{
		Ctx:   pctx,
		Agent: ag,
		Store: store,
		Run:   ctx,
	}, ""); err != nil {
		return fmt.Errorf("start module lifecycle: %w", err)
	}

	// The startup banner (card t_18b13390): a serve tells a person
	// WHERE it serves and WHAT it is - version (the same stamp /health
	// serves), the resolved bind strings, the config file actually read,
	// the data dir, the model and its endpoint. Every value is real
	// boot-resolved shape, nothing hardcoded.
	banner := serveBanner(omegaVersion, len(pctx.Channels), serveBinds(cfg),
		cfgPath, canonPath(datadir.Dir(omegaHome())), ag.ModelName())
	fmt.Println(banner)
	if logger != nil {
		logger.Printf("%s", banner)
	}

	// Wait for signal or first channel error.
	select {
	case <-ctx.Done():
	case err := <-errCh:
		if err != nil && ctx.Err() == nil {
			stop()
			return fmt.Errorf("channel error: %w", err)
		}
	}

	// Stop all channels.
	for _, ch := range pctx.Channels {
		if err := ch.Stop(); err != nil {
			fmt.Fprintf(os.Stderr, "omega: channel stop error: %v\n", err)
		}
	}
	if cron, ok := pctx.Services().Resolve("cron").(interface{ Stop() error }); ok {
		if err := cron.Stop(); err != nil {
			fmt.Fprintf(os.Stderr, "omega: cron stop error: %v\n", err)
		}
	}
	return nil
}

// parseRunSession lifts the --session <id> flag out of the run argv.
// The flag may sit before or after the prompt; it survives neither
// config/trust stripping (it is consumed before those run). A missing
// or empty value is an error - a wake pointing at no session must
// fail the spawn, not fork a new chat.
func parseRunSession(args []string) (string, []string, error) {
	sessionID := ""
	clean := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if args[i] == "--session" {
			if i+1 >= len(args) {
				return "", nil, fmt.Errorf("run: --session requires a session id")
			}
			sessionID = strings.TrimSpace(args[i+1])
			if sessionID == "" {
				return "", nil, fmt.Errorf("run: --session requires a session id")
			}
			i++
			continue
		}
		clean = append(clean, args[i])
	}
	return sessionID, clean, nil
}

// cmdRun loads config and runs the agent once with the given prompt,
// printing the final response. --session <id> binds the run to an
// existing session: the run persists into that session through a
// TurnSink (cron-style), input included - the wake-subprocess entry
// the continuous module spawns against an agent home (card
// t_72ecca67). Without --session the run persists nothing, exactly as
// before.
func cmdRun(configPath string, args []string, trust trustFlags) error {
	sessionID, args, err := parseRunSession(args)
	if err != nil {
		return err
	}
	appendPrompts := parseAppendPrompts(args)
	args = stripAppendPrompts(stripTrustArgs(stripConfigFlag(args)))
	prompt, images, err := parseFileArgs(args)
	if err != nil {
		return err
	}
	if prompt == "" && len(images) == 0 {
		return fmt.Errorf("run requires a prompt argument")
	}

	cfg, err := LoadConfig(resolveConfigPath(configPath))
	if err != nil {
		return err
	}
	if err := resolveHomePaths(&cfg); err != nil {
		return err
	}
	// Delegated child running AS an agent (OMEGA_AGENT, card
	// t_7e2b7002): the agent's config.yaml layers over the home's -
	// model, provider, system_prompt - and its skills/memory/logs are
	// its own. The default surface ignores the var entirely.
	if name := resolveAgentName(); name != "" {
		if err := ApplyAgentLayer(&cfg, omegaHome(), name); err != nil {
			return fmt.Errorf("agent layer %s: %w", name, err)
		}
		// The model override is caller intent (the wake contract:
		// config.go's applyEnv table honors OMEGA_MODEL over the
		// child's own provider.model). Layers apply AFTER applyEnv, so
		// an agent layer defining provider.model would silently
		// clobber it - re-assert the env. Empty env = inherit (the
		// defaulting law): the layer's model IS the session model.
		applyModelEnvOverride(&cfg)
		// The store pin is caller intent too (the delegate child):
		// the layer's data isolation would otherwise re-point it at
		// the agent's own database and orphan --session (t_93b4e521).
		applyDBPathEnvOverride(&cfg)
	}
	llm.SetHTTPTimeout(cfg.HTTPTimeout)
	ag, store, logger, _, _, _, _, _, _, _, err := newAgent(cfg, configPath, appendPrompts, trust)
	if err != nil {
		return err
	}
	defer store.Close()
	if logger != nil {
		defer logger.Close()
	}

	ctx, stop := signalContext()
	defer stop()

	// Subagent origin (verdict cards v1): a delegated `omega run` child
	// sets OMEGA_SUBAGENT=1. The verdict tool refuses to issue from any
	// non-interactive origin - a subagent must not hard-pause
	// waiting for a click its parent surface would have to surface.
	if os.Getenv("OMEGA_SUBAGENT") != "" {
		ctx = core.WithRunOrigin(ctx, "delegate")
	} else {
		ctx = core.WithRunOrigin(ctx, "interactive")
	}

	var userMsg core.Message
	if len(images) > 0 {
		userMsg = core.NewUserWithImages(prompt, images)
	} else {
		userMsg = core.NewUser(prompt)
	}
	messages := []core.Message{userMsg}
	// --session <id>: persist the run into the existing session (the
	// wake contract). The session must exist - a typo'd id must fail
	// loud, not fork a new one.
	if sessionID != "" {
		if _, err := store.GetSession(context.Background(), sessionID); err != nil {
			return fmt.Errorf("run: --session %s: %w", sessionID, err)
		}
		ctx = agent.WithTurnSink(ctx, agent.NewStoreTurnSink(store, sessionID, ""))
		// The wake report shape (card t_72ecca67, ruling 11): inside a
		// wake child the closing message is citation-stripped and
		// evidence-stamped before it lands in the canonical chat. Any
		// other process returns the sink untouched.
		ctx = agent.WithTurnSink(ctx, continuous.WrapWakeSink(agent.RunTurnSink(ctx)))
		ctx = agent.WithPersistInput(ctx, []core.Message{userMsg})
	}
	var response strings.Builder
	for event := range ag.Run(ctx, messages, nil) {
		switch e := event.(type) {
		case core.StreamEvent:
			if chunk, ok := e.Event.(core.ResponseChunk); ok {
				response.WriteString(chunk.Content)
			}
		case core.AgentEnd:
			if e.Error != "" {
				return fmt.Errorf("agent: %s", e.Error)
			}
		}
	}
	fmt.Print(response.String())
	if response.Len() > 0 && !strings.HasSuffix(response.String(), "\n") {
		fmt.Println()
	}
	return nil
}

// omegaVersion is set via ldflags at build time:
//
//	go build -ldflags "-X main.omegaVersion=v0.9.1-3c2ab0db" ./cmd/omega
//
// go install builds carry no ldflags, so init() falls back to the module
// version Go stamps into module-built binaries (debug.ReadBuildInfo),
// already trimmed to the short tag-hash shape.
var omegaVersion = ""

func init() {
	if omegaVersion == "" {
		omegaVersion = resolveVersionFromBuildInfo()
	}
}

// versionShapeRe is the one shape a version string may take: a semver
// tag with an optional -nightly.DATE prerelease, optionally followed by
// a dash and an 8-char commit hash ("v0.9.1", "v0.9.1-nightly.20261022",
// "v0.9.1-nightly.20261022-c268097e"). Anything else is not a version.
var versionShapeRe = regexp.MustCompile(`^(v[0-9.]+(-nightly\.[0-9]+)?)(-[0-9a-f]{8})?$`)

// tagShapeRe is versionShapeRe without the hash suffix.
var tagShapeRe = regexp.MustCompile(`^(v[0-9]+\.[0-9]+\.[0-9]+)(-nightly\.[0-9]+)?$`)

// pseudoVersionRe matches the pseudo-versions the go command stamps
// into module-built binaries ("<tag>.<n>.<yyyymmddhhmmss>-<12 hex>",
// e.g. v0.9.1-nightly.20261022.0.20261023012859-c268097e2689): group 1
// is the base tag, group 2 the commit revision.
var pseudoVersionRe = regexp.MustCompile(`^(v[0-9]+\.[0-9]+\.[0-9]+(?:-nightly\.[0-9]+)?)\.[0-9]+\.[0-9]+-([0-9a-f]+)$`)

// shortBuildVersion trims a version to the short shape (tag-hash):
// a pseudo-version collapses to its base tag plus the first 8 hash
// chars, a plain tag passes through, anything else ("dev", "(devel)")
// is not a version and reports dev.
func shortBuildVersion(v string) string {
	if m := pseudoVersionRe.FindStringSubmatch(v); m != nil && len(m[2]) >= 8 {
		return m[1] + "-" + m[2][:8]
	}
	if tagShapeRe.MatchString(v) {
		return v
	}
	return "dev"
}

// resolveVersionFromBuildInfo picks the module version for go install
// builds, trimmed to the short tag-hash shape; returns "dev" for bare
// go builds (no ldflags, no module stamp).
func resolveVersionFromBuildInfo() string {
	if bi, ok := debug.ReadBuildInfo(); ok {
		if v := bi.Main.Version; v != "" && v != "(devel)" {
			return shortBuildVersion(v)
		}
	}
	return "dev"
}

// cmdHealth checks whether the server is reachable at the configured port.
func cmdHealth(configPath string) error {
	cfg, err := LoadConfig(resolveConfigPath(configPath))
	if err != nil {
		return err
	}
	host := os.Getenv("OMEGA_HEALTH_HOST")
	if host == "" {
		host = "localhost"
	}
	url := fmt.Sprintf("http://%s:%d/health", host, cfg.Server.Port)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("server not reachable at %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server at %s returned %s", url, resp.Status)
	}
	fmt.Printf("ok: %s\n", url)
	return nil
}
