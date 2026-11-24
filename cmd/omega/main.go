// Command omega is the single binary entry point for the omega agent.
package main

import (
	"context"
	"errors"
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
	// Stray guard (finding C5): a home that IS a checkout root gets one visible warning.
	// Hint, never a block - home resolution is unchanged.
	exeDir := ""
	if exe, err := os.Executable(); err == nil {
		exeDir = filepath.Dir(exe)
	}
	printRepoHomeHint(home, exeDir)
	bootstrapHome(home)
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "omega:", err)
		os.Exit(1)
	}
}

// repoHomeHint is the first-user stray guard (finding C5). The default resolution stays
// exactly as it is: a release-folder home beside the binary is legitimate, so this only
// warns. home == exe dir excludes an explicit OMEGA_HOME (an operator who points home
// at a checkout chose that), and home holding.git excludes the dev lane's bin/ home
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

// helpText is the global --help output's static part.
const helpText = `omega - a Go event-stream agent (Pi/Tau port)

Usage:
  omega                 interactive CLI loop (first run: walks you through setup)
  omega <path>          start the loop in <path> (chdir first)
  omega setup           write the first config (provider, model, api key)
  omega run <prompt>    run one prompt to stdout
  omega serve           start the HTTP server (SSE streaming; serves the webui)
  omega desktop         open the desktop window (one process: the core + the webui)
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

// run parses the subcommand and dispatches.
func run(args []string) error {
	home := omegaHome()
	os.Setenv("OMEGA_HOME", home)
	// Agent-home migration: the retired agents/default profile's persona files move UP.
	// Exactly once, before anything reads them.
	if err := agent.MigrateAgentHomes(home); err != nil {
		return err
	}
	if err := migrateAgentStores(home); err != nil {
		return err
	}
	// Set OMEGA_SKILLS_DIR so the skills extension can read skills.
	os.Setenv("OMEGA_SKILLS_DIR", home+"/skills")
	// Set OMEGA_BIN so the delegate extension can spawn subagents.
	if exe, err := os.Executable(); err == nil {
		os.Setenv("OMEGA_BIN", exe)
	}
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
	case "desktop":
		return cmdDesktop(rest)
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
		// `omega tools` owns its own argv, `--help` included.
		return cmdTools(rest)
	case "settings":
		return cmdSettings(parseConfigFlag(rest), rest)
	case "update":
		return cmdUpdate(rest)
	case "rollback":
		// Alias for `omega update --rollback` — same one-generation swap back.
		return cmdRollback()
	case "chat", "":
		// The chat subcommand is a legacy alias for the CLI loop.
		return cmdLoop(parseConfigFlag(rest), appendPrompts, trust)
	default:
		if info, serr := os.Stat(sub); serr == nil && info.IsDir() {
			if err := os.Chdir(sub); err != nil {
				return fmt.Errorf("chdir %s: %w", sub, err)
			}
			return cmdLoop(parseConfigFlag(rest), appendPrompts, trust)
		}
		// Module-owned subcommands.
		if handled, err := dispatchModuleCLI(args, sub, rest); handled {
			return err
		}
		if err := os.Chdir(sub); err != nil {
			return fmt.Errorf("chdir %s: %w", sub, err)
		}
		return cmdLoop(parseConfigFlag(rest), appendPrompts, trust)
	}
}

// isRootSubcommand reports whether sub is a built-in switch case in run.
// The list must stay in sync with run's switch: a built-in keeps the root's flag
// short-circuit on its trailing argv, while a module subcommand owns the whole tail.
// `tools` is a built-in that is deliberately ABSENT here: it carries a module
// subcommand's flag contract (its own `--help`, its own argv parse) while living in
// cmd/omega, where the one config.yaml writer (writeDisabled) already sits.
func isRootSubcommand(sub string) bool {
	switch sub {
	case "setup", "serve", "desktop", "run", "health", "export", "insights",
		"agents", "settings", "update", "rollback", "chat":
		return true
	}
	return false
}

// printHelp prints the static help plus the module-owned subcommand lines. The mount is
// best effort: help never fails, a broken home just loses the module section (the
// static part always prints first).
func printHelp(args []string) {
	fmt.Print(helpText)
	subs := []agent.Subcommand{}
	if reg, pctx, err := mountRegistry(configForCLI(args)); err == nil {
		defer agent.DismountAll(reg, pctx)
		subs = reg.Subcommands()
	}
	printModuleSubcommands(subs, os.Stdout)
}

// printModuleSubcommands renders the module section of the global help - one line per
// registered subcommand, name-sorted. An empty table prints no section: a binary with
// no module CLIs looks exactly like the pre-phase-D help.
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

// mountRegistry builds and mounts the plugin set with the API mount, returning the
// registry and the mounted context. The context is returned so a caller can drain
// seams; the CLI dispatch only needs the registry.
func mountRegistry(cfg Config) (*agent.Registry, *agent.Context, error) {
	plugins, err := buildPlugins(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("build plugins: %w", err)
	}
	pctx := &agent.Context{
		CWD:     cwd(),
		Configs: agent.RouteConfigs(plugins, cfg),
		// The disabled.tools fold rides the borrowed mount exactly as it rides the serve.
		DisabledTools: cfg.Disabled.Tools,
	}
	reg, err := agent.MountAllAPI(plugins, pctx, nil, cfg.Disabled.Modules)
	if err != nil {
		return nil, nil, fmt.Errorf("mount extensions: %w", err)
	}
	return reg, pctx, nil
}

// configForCLI resolves the config for a module CLI dispatch. A module subcommand keeps
// the old CLI contract - it never required a (valid) config.yaml, the capsules run on
// their own config sections - so a missing home or a broken global config falls back to
// the defaults instead of failing the dispatch.
func configForCLI(args []string) Config {
	cfg, err := LoadConfig(resolveConfigPath(parseConfigFlag(args)))
	if err != nil {
		return DefaultConfig()
	}
	return cfg
}

// dispatchModuleCLI mounts the capsules and dispatches a module-owned CLI subcommand.
func dispatchModuleCLI(args []string, sub string, rest []string) (bool, error) {
	reg, pctx, err := mountRegistry(configForCLI(args))
	if err != nil {
		return false, err
	}
	handled, derr := dispatchSubcommand(reg, sub, rest)
	// The mount is borrowed for one dispatch. The command result is the contract; the
	// teardown's error never eats it.
	_ = agent.DismountAll(reg, pctx)
	return handled, derr
}

// dispatchSubcommand consults the registry's cli table.
func dispatchSubcommand(reg *agent.Registry, sub string, rest []string) (bool, error) {
	for _, c := range reg.Subcommands() {
		if c.Name == sub {
			return true, c.Handler(rest)
		}
	}
	return false, nil
}

// splitSubcommand returns the first non-flag argument as the subcommand and every other
// argument as rest - leading flags included, so --config/--approve/--no-approve work
// before a subcommand or project path exactly as the help text promises. With no
// subcommand at all (the bare REPL entry) rest is the full flag list: the bare entry
// must parse trust flags identically to subcommand paths.
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

// parseConfigFlag extracts the value of --config from args, or "" if absent.
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

// stripConfigFlag removes --config and its value from args, so the remaining.
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

// parseAppendPrompts extracts all --append-system-prompt values from args.
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

// stripAppendPrompts removes --append-system-prompt and its values from args,.
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

// canonPath is the boundary's separator policy. Explicitly configured values pass
// through untouched: the boundary canonizes what it builds, never what the operator
// wrote.
func canonPath(p string) string {
	return filepath.ToSlash(p)
}

// omegaHome returns the omega home directory.
func omegaHome() string {
	exePath := ""
	if exe, err := os.Executable(); err == nil {
		exePath = exe
	}
	userHome, _ := os.UserHomeDir()
	return canonPath(homeForExe(os.Getenv("OMEGA_HOME"), exePath, userHome))
}

// resolveConfigPath returns the --config value, or the home config.yaml when it exists,
// or "" to skip YAML entirely.
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

// requireConfig is the setup gate.
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

// resolveHomePaths fills in home-relative defaults for DBPath and Skills.Dir.
// Returns an error if the home directory cannot be created.
func resolveHomePaths(cfg *Config) error {
	home := omegaHome()
	if cfg.Store.DBPath == "omega.db" {
		cfg.Store.DBPath = canonPath(datadir.DBPath(home, "omega.db"))
	}
	if cfg.Kanban.DBPath == "kanban.db" {
		// one kanban.db owns all boards.
		cfg.Kanban.DBPath = canonPath(datadir.DBPath(home, "kanban.db"))
	}
	if cfg.Skills.Dir == "skills" {
		cfg.Skills.Dir = home + "/skills"
	}
	if cfg.Memory.File == "memory.md" {
		cfg.Memory.File = home + "/memory.md"
	}
	if cfg.Memory.UserProfileFile == "user.md" {
		cfg.Memory.UserProfileFile = home + "/user.md"
	}
	if cfg.Logging.File == "" || cfg.Logging.File == "omega.log" {
		// "" is the construction default. Both mean the home-rooted log - never a
		// CWD-relative file.
		cfg.Logging.File = home + "/omega.log"
	}
	if err := os.MkdirAll(home, 0755); err != nil {
		return fmt.Errorf("create omega home %s: %w", home, err)
	}
	return nil
}

// buildPlugins creates the list of in-process extensions from config.
func buildPlugins(cfg Config) ([]agent.Plugin, error) {
	mcpPlugin, err := mcp.NewPluginFromEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "omega: mcp bridge: %v\n", err)
		mcpPlugin = mcp.NewPlugin(nil)
	}
	return []agent.Plugin{
		observability.NewPlugin(),
		loop.NewPlugin(),
		&provider.Plugin{},
		store.NewPlugin(),
		skills.NewPlugin(),
		compactor.NewPlugin(),
		memory.NewPlugin(),
		// mount order is declared data - the notifications consumers (delegate, kanban, cron)
		// carry Requires edges naming the seam, and agent.MountAllAPI's topoSort is the only
		// ordering.
		notifications.NewPlugin(),
		prompt.NewPlugin(),
		tools.NewPlugin(),
		files.NewPlugin(),
		// The shell capsule: ONE registration for both faces of the shell domain.
		terminal.NewPlugin(cwd(), cfg.Server.DefaultWorkspace),
		verdict.NewPlugin(),
		approval.NewPlugin(),
		mcpPlugin,
		delegate.NewPlugin(),
		api.NewPlugin(),
		trust.NewPlugin(),
		cron.NewPlugin(),
		kanbanModule.NewPlugin(),
		// The browser capsule: the tab's last-URL memory, the only server surface.
		browserModule.NewPlugin(),
		// The continuous capsule: agent homes, the wake subprocess, the canonical chats. The
		// home passes through the host - exe-dir resolution is cmd/omega business, the
		// capsule never re-guesses it.
		continuous.NewPluginWithHome(omegaHome()),
	}, nil
}

// newAgent wires config into a provider, agent, store, and extensions via the mount
// surface. The store and plugin context are returned so the caller can close the store
// and start channels.
func newAgent(cfg Config, configPath string, appendPrompts []string, trust trustFlags) (*agent.Agent, agent.StoreProvider, agent.LoggerProvider, *agent.Context, *agent.Registry, func([]string), func(string), func(string), func(string), func(string, int), error) {
	plugins, err := buildPlugins(cfg)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("build plugins: %w", err)
	}
	ctx := &agent.Context{
		CWD:     cwd(),
		Configs: agent.RouteConfigs(plugins, cfg),
		DisabledTools: cfg.Disabled.Tools,
	}
	// The API mount honors the config.yaml disabled.modules toggle list.
	// ALWAYS OFF: an agent home mounts the locked pair plus exactly the capsules its
	// definition enables - everything else toggleable is off, whether or not main runs it.
	// The host's own fold still applies (the subset law: an agent can never enable more
	// than the host).
	disabledModules := cfg.Disabled.Modules
	if cfg.agentName != "" {
		alwaysOff, err := agent.AlwaysOffDisabled(plugins, cfg.agentToggles)
		if err != nil {
			return nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("agent %s toggles: %w", cfg.agentName, err)
		}
		disabledModules = append(append([]string{}, disabledModules...), alwaysOff...)
	}
	reg, err := agent.MountAllAPI(plugins, ctx, nil, disabledModules)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("mount extensions: %w", err)
	}
	// The registry-toggle adapter needs the one continuous plugin instance the mount. The
	// hook threads as a func, never as the capsule's type - the nm-purity scrub deletes
	// the whole strand when the capsule leaves, and the host keeps a nil hook (nm-purity
	// scrub shape).
	var setAgents func([]string)
	var setFilesWorkspace func(string)
	// setChatWorkspace is the HTTP channel plugin's default-workspace hot knob.
	var setChannelWorkspace func(string)
	// setTerminalWorkspace is the terminal capsule's default-workspace hot knob.
	var setTerminalWorkspace func(string)
	// setAgentPromptHook is the HTTP channel plugin's serving-agent hot knob.
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
			hp.SetAgentRunFactory(agentRunFactory(configPath, appendPrompts, trust))
		}
		if tp, ok := p.(*terminal.Plugin); ok {
			setTerminalWorkspace = tp.SetDefaultWorkspace
		}
	}

	// The store resolves BEFORE the agent is built.
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

	// setAgentConfig is the runtime-knob hot hook: ONE closure covers BOTH agents a
	// settings save of system_prompt / max_turns must reach - the host-owned agent here
	// (cron and the continuous carriers ride it via StartDeps) and every mounted HTTP
	// bind's serving agent (the channel builds its own agent with empty AgentOptions, so
	// the boot values reach it only through the wire-time push the serve boot calls
	// below); the channel store doubles as the boot bake for a late wire.
	setAgentConfig := func(prompt string, maxTurns int) {
		if setAgentPromptHook != nil {
			setAgentPromptHook(prompt, maxTurns)
		}
		ag.SetPromptCustom(prompt)
		ag.SetMaxTurns(maxTurns)
	}

	return ag, st2, logger, ctx, reg, setAgents, setFilesWorkspace, setChannelWorkspace, setTerminalWorkspace, setAgentConfig, nil
}

// wireCaptioner builds the image captioner (vision_model knob) onto the context. It is
// installed UNCONDITIONALLY as of - the settings save flips the knob live through
// Captioner.SetModel, and a captioner with no model is simply off (CaptioningEnabled
// false, the agent skips the aux hop exactly like a nil one).
func wireCaptioner(cfg Config, pctx *agent.Context) {
	if pctx == nil {
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
	if cfg.VisionModel != "" && sink == nil && lg != nil {
		lg.Warnf("omega: vision_model set but the store does not support captions - captions cache in-process only")
	}
}

func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

// openDefaultSupply opens the substrate-class default SUPPLY when no store capsule. The
// capsule's mount resolves the same file its own way (config DBPath - datadir.AgentHome
// / DBPath); the supply fallback opens the same <home>/data/omega.db, never:memory: - a
// fresh chat boots and persists across a restart through this supply, only the
// management face (the capsule's tab + routes) is gone. The handle offers back under
// "store"/"host": the chat floor reads it exactly like a capsule-opened store, and the
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

// serveBinds renders the resolved HTTP bind strings for the startup banner.
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

// serveBanner is the boot line a person can read.
func serveBanner(version string, channels int, binds []string, configPath, dataDir, model string) string {
	return fmt.Sprintf("omega %s · serving %d channel(s) on %s · config %s · data %s · model %s",
		version, channels, strings.Join(binds, ", "), configPath, dataDir, model)
}

// serveRuntime is one booted serve: the live plugin mount, the started channels and
// module fence, and the handle that tears every one of them down.
type serveRuntime struct {
	URL      string
	ctx      context.Context
	stop     context.CancelFunc
	errs     chan error
	pctx     *agent.Context
	reg      *agent.Registry
	channels []core.Channel
	logger   agent.LoggerProvider
	store    agent.StoreProvider
	once     sync.Once
}

// startRuntime boots everything a serve does after config load, and returns once
// /health answers ok. The caller owns the runtime and must shutdown it.
func startRuntime(cfg Config, configPath string, appendPrompts []string, trust trustFlags) (*serveRuntime, error) {
	return startRuntimeWithin(cfg, configPath, appendPrompts, trust, 5*time.Second)
}

// startRuntimeWithin is startRuntime with an explicit health deadline: a probe run
// unattended may allow a slower first boot than a person waiting on a terminal.
func startRuntimeWithin(cfg Config, configPath string, appendPrompts []string, trust trustFlags, healthTimeout time.Duration) (*serveRuntime, error) {
	llm.SetHTTPTimeout(cfg.HTTPTimeout)
	ag, store, logger, pctx, reg, setAgents, setFilesWorkspace, setChannelWorkspace, setTerminalWorkspace, setAgentConfig, err := newAgent(cfg, configPath, appendPrompts, trust)
	if err != nil {
		return nil, err
	}

	ctx, stop := signalContext()
	rt := &serveRuntime{ctx: ctx, stop: stop, pctx: pctx, reg: reg, logger: logger, store: store}
	if len(pctx.Channels) == 0 {
		rt.shutdown()
		return nil, fmt.Errorf("no channels configured — mount a channel extension (e.g. http_channel)")
	}
	configFile := resolveConfigPath(configPath)
	if configFile == "" {
		// Fresh install, no config.yaml anywhere.
		configFile = omegaHome() + "/config/config.yaml"
	}
	// The module-toggle surface: the registry behind the seam, arming the fence
	// handles exactly like the boot-time StartAll below, persisting the toggle list to the
	// config.yaml disabled: key boot reads. The registry-toggle seam rides next to it:
	// the skills/tools/agents namespaces of the same fold, one shared live fold
	// pointer so a module toggle and a registry toggle can never clobber each other's
	// namespaces.
	fold := &DisabledNamespaces{
		Modules:    cfg.Disabled.Modules,
		SkillNames: cfg.Disabled.SkillNames,
		Tools:      cfg.Disabled.Tools,
		Agents:     cfg.Disabled.Agents,
	}
	// one mutex for BOTH toggle surfaces.
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
	watchWorkspace := cfg.Server.DefaultWorkspace
	WatchConfig(configFile, func(newCfg Config) {
		hotApply(newCfg, watchWorkspace, pctx, setFilesWorkspace, setChannelWorkspace, setTerminalWorkspace, setAgentConfig)
		watchWorkspace = newCfg.Server.DefaultWorkspace
		registries.Sync(newCfg.Disabled) // hand edits follow the toggle path
	})
	if setAgentConfig != nil {
		setAgentConfig(cfg.SystemPrompt, cfg.MaxTurns)
	}
	// The cron fire's cap, boot half: the scheduler bounds a hung turn with
	// server.run_timeout and cannot see the config file THIS process booted on (an
	// explicit --config path, or an exe-dir home without OMEGA_HOME), so the host pushes
	// its resolution before the fence starts the runner.
	pushRunTimeout(pctx, cfg)
	rt.errs = make(chan error, len(pctx.Channels))
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
			rt.errs <- c.Start(ctx, deps)
		}(ch)
	}
	rt.channels = pctx.Channels

	// The fence: module background work starts here, after the mount commit.
	if err := pctx.RunGroup().StartAll(ctx, core.StartDeps{
		Ctx:   pctx,
		Agent: ag,
		Store: store,
		Run:   ctx,
	}, ""); err != nil {
		rt.shutdown()
		return nil, fmt.Errorf("start module lifecycle: %w", err)
	}

	binds := serveBinds(cfg)
	rt.URL = "http://" + binds[0]
	banner := serveBanner(omegaVersion, len(pctx.Channels), binds,
		configFile, canonPath(datadir.Dir(omegaHome())), ag.ModelName())
	fmt.Println(banner)
	if logger != nil {
		logger.Printf("%s", banner)
	}
	return waitHealthy(rt, binds[0], healthTimeout)
}

// waitHealthy polls /health until the body reports status ok, or a channel fails, or
// the deadline passes. A boot that never answers is torn down before the error returns.
func waitHealthy(rt *serveRuntime, addr string, timeout time.Duration) (*serveRuntime, error) {
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(timeout)
	lastErr := errors.New("no answer")
	for {
		select {
		case cerr := <-rt.errs:
			rt.shutdown()
			if cerr == nil {
				cerr = errors.New("channel exited without an error")
			}
			return nil, fmt.Errorf("serve %s: %w", addr, cerr)
		default:
		}
		body, err := healthBody(client, rt.URL)
		if err == nil && strings.Contains(body, `"status":"ok"`) {
			return rt, nil
		}
		if err != nil {
			lastErr = err
		}
		if time.Now().After(deadline) {
			rt.shutdown()
			return nil, fmt.Errorf("serve %s: /health not ok within %s: %w", addr, timeout, lastErr)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// healthBody reads one /health body.
func healthBody(client *http.Client, url string) (string, error) {
	resp, err := client.Get(url + "/health")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return string(body), fmt.Errorf("/health answered %s", resp.Status)
	}
	return string(body), nil
}

// shutdown tears the runtime down: channel Stop, cron Stop, the module dismount (the
// capsules close what they opened - the kanban and notifications stores, the continuous
// ledger), logger Close, store Close. Idempotent - a failed boot and the caller's defer
// may both reach it.
func (rt *serveRuntime) shutdown() {
	rt.once.Do(func() {
		rt.stop()
		for _, ch := range rt.channels {
			if err := ch.Stop(); err != nil {
				fmt.Fprintf(os.Stderr, "omega: channel stop error: %v\n", err)
			}
		}
		if rt.pctx != nil {
			if cron, ok := rt.pctx.Services().Resolve("cron").(interface{ Stop() error }); ok {
				if err := cron.Stop(); err != nil {
					fmt.Fprintf(os.Stderr, "omega: cron stop error: %v\n", err)
				}
			}
			if rt.reg != nil {
				if err := agent.DismountAll(rt.reg, rt.pctx); err != nil {
					fmt.Fprintf(os.Stderr, "omega: dismount error: %v\n", err)
				}
			}
		}
		if rt.logger != nil {
			rt.logger.Close()
		}
		if rt.store != nil {
			rt.store.Close()
		}
	})
}

// cmdServe loads config, wires the agent, and starts all mounted channels.
func cmdServe(configPath string, appendPrompts []string, trust trustFlags) error {
	cfgPath := resolveConfigPath(configPath)
	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		return err
	}
	if err := resolveHomePaths(&cfg); err != nil {
		return err
	}
	rt, err := startRuntime(cfg, configPath, appendPrompts, trust)
	if err != nil {
		return err
	}
	defer rt.shutdown()

	select {
	case <-rt.ctx.Done():
	case err := <-rt.errs:
		if err != nil && rt.ctx.Err() == nil {
			return fmt.Errorf("channel error: %w", err)
		}
	}
	return nil
}

// parseRunSession lifts the --session <id> flag out of the run argv. A missing or empty
// value is an error - a wake pointing at no session must fail the spawn, not fork a new
// chat.
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

// cmdRun loads config and runs the agent once with the given prompt, printing.
// Without --session the run persists nothing, exactly.
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
	// Delegated child running AS an agent.
	if name := resolveAgentName(); name != "" {
		if err := ApplyAgentLayer(&cfg, omegaHome(), name); err != nil {
			return fmt.Errorf("agent layer %s: %w", name, err)
		}
		// The model override is caller intent (the wake contract. Layers apply AFTER
		// applyEnv, so an agent layer defining provider.model would silently clobber it -
		// re-assert the env.
		applyModelEnvOverride(&cfg)
		// The store pin is caller intent too (the delegate child).
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

	// Subagent origin: a delegated `omega run` child sets OMEGA_SUBAGENT=1. The verdict
	// tool refuses to issue from any non-interactive origin - a subagent must not
	// hard-pause waiting for a click its parent surface would have to surface.
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
	// session <id>: persist the run into the existing session (the wake contract). The
	// session must exist - a typo'd id must fail loud, not fork a new one.
	if sessionID != "" {
		if _, err := store.GetSession(context.Background(), sessionID); err != nil {
			return fmt.Errorf("run: --session %s: %w", sessionID, err)
		}
		ctx = agent.WithTurnSink(ctx, agent.NewStoreTurnSink(store, sessionID, ""))
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
// go build -ldflags "-X main.omegaVersion=v0.9.1-3c2ab0db"./cmd/omega
//
// go install builds carry no ldflags, so init falls back to the module
// version Go stamps into module-built binaries (debug.ReadBuildInfo),
// already trimmed to the short tag-hash shape.
var omegaVersion = ""

func init() {
	if omegaVersion == "" {
		omegaVersion = resolveVersionFromBuildInfo()
	}
}

// versionShapeRe is the one shape a version string may take.
var versionShapeRe = regexp.MustCompile(`^(v[0-9.]+(-nightly\.[0-9]+)?)(-[0-9a-f]{8})?$`)

// tagShapeRe is versionShapeRe without the hash suffix.
var tagShapeRe = regexp.MustCompile(`^(v[0-9]+\.[0-9]+\.[0-9]+)(-nightly\.[0-9]+)?$`)

// pseudoVersionRe matches the pseudo-versions the go command stamps into module-built.
var pseudoVersionRe = regexp.MustCompile(`^(v[0-9]+\.[0-9]+\.[0-9]+(?:-nightly\.[0-9]+)?)\.[0-9]+\.[0-9]+-([0-9a-f]+)$`)

// shortBuildVersion trims a version to the short shape (tag-hash).
func shortBuildVersion(v string) string {
	if m := pseudoVersionRe.FindStringSubmatch(v); m != nil && len(m[2]) >= 8 {
		return m[1] + "-" + m[2][:8]
	}
	if tagShapeRe.MatchString(v) {
		return v
	}
	return "dev"
}

// resolveVersionFromBuildInfo picks the module version for go install builds, trimmed.
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
