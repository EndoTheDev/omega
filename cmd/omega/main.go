// Command omega is the single binary entry point for the omega agent.
// It ties together the serve, run, and health subcommands.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/ai"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/modules/agent_loop"
	"github.com/EndoTheDev/omega/modules/compactor"
	"github.com/EndoTheDev/omega/modules/cron"
	"github.com/EndoTheDev/omega/modules/datadir"
	"github.com/EndoTheDev/omega/modules/delegate"
	httpchannel "github.com/EndoTheDev/omega/http_channel"
	kanbanModule "github.com/EndoTheDev/omega/modules/kanban"
	"github.com/EndoTheDev/omega/modules/logging"
	"github.com/EndoTheDev/omega/modules/mcp"
	"github.com/EndoTheDev/omega/modules/memory"
	"github.com/EndoTheDev/omega/modules/notifications"
	"github.com/EndoTheDev/omega/modules/prompt"
	"github.com/EndoTheDev/omega/modules/provider"
	"github.com/EndoTheDev/omega/modules/skills"
	"github.com/EndoTheDev/omega/modules/store"
	"github.com/EndoTheDev/omega/modules/tools"
	"github.com/EndoTheDev/omega/modules/trust"
	"github.com/EndoTheDev/omega/tui"
	"github.com/EndoTheDev/omega/modules/verdict"
	"github.com/EndoTheDev/omega/modules/web"
)

func main() {
	// first-run bootstrap: write the example configs into the home so
	// any install (release folder, go install, dotfolder) is self-documenting
	bootstrapHome(omegaHome())
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "omega:", err)
		os.Exit(1)
	}
}

// helpText is the global --help output. One help for all subcommands;
// per-subcommand help is deferred until a subcommand has enough flags
// to warrant it.
const helpText = `omega - a Go event-stream agent (Pi/Tau port)

Usage:
  omega                 start the interactive TUI
  omega <path>          start the TUI in <path> (chdir first)
  omega chat            start the interactive TUI
  omega run <prompt>    run one prompt to stdout
  omega serve           start the HTTP server (SSE streaming)
  omega export <id|label>  export a session as JSONL
  omega insights [--days N]  show session usage analytics (default: 30 days)
  omega agents         list agents; ` + "`omega agents create <name>`" + ` scaffolds one
  omega update              install the latest stable release (nightlies invisible)
  omega update --nightly    install the newest -nightly.* prerelease instead
  omega update --rollback   swap back to omega.bak + verify it runs
  omega rollback            same as omega update --rollback
  omega health          check the server at the configured port
  omega kanban          the task board registry (see: omega kanban --help)

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
`

// run parses the subcommand and dispatches. The first non-flag argument
// selects the subcommand; --config is accepted before or after it. With
// no argument, the TUI starts. A non-subcommand argument is treated as a
// project path: omega chdirs there and starts the TUI.
func run(args []string) error {
	// Set OMEGA_HOME early so all child processes inherit it.
	home := omegaHome()
	os.Setenv("OMEGA_HOME", home)
	// Default-agent migration (card t_7e2b7002): agents/ appears on
	// first boot; legacy home memory.md/user.md move into
	// agents/default/ - exactly once, before anything reads them.
	if err := agent.MigrateDefaultAgent(home); err != nil {
		return err
	}
	// Set OMEGA_SKILLS_DIR so the skills extension can read skills.
	os.Setenv("OMEGA_SKILLS_DIR", home+"/skills")
	// Set OMEGA_BIN so the delegate extension can spawn subagents.
	if exe, err := os.Executable(); err == nil {
		os.Setenv("OMEGA_BIN", exe)
	}
	for _, a := range args {
		switch a {
		case "--help", "-h":
			fmt.Print(helpText)
			return nil
		case "--version", "-v":
			fmt.Println("omega", omegaVersion)
			return nil
		}
	}
	sub, rest := splitSubcommand(args)
	appendPrompts := parseAppendPrompts(rest)
	trust := parseTrustArgs(rest)
	switch sub {
	case "serve":
		return cmdServe(parseConfigFlag(rest), appendPrompts, trust)
	case "run":
		return cmdRun(parseConfigFlag(rest), rest, trust)
	case "health":
		return cmdHealth(parseConfigFlag(rest))
	case "kanban":
		return cmdKanban(rest)
	case "export":
		return cmdExport(parseConfigFlag(rest), rest)
	case "insights":
		return cmdInsights(parseConfigFlag(rest), rest)
	case "agents":
		return cmdAgents(rest)
	case "update":
		return cmdUpdate(rest)
	case "rollback":
		// Alias for `omega update --rollback` — same one-generation
		// swap back, health-verified.
		return cmdRollback()
	case "chat", "":
		// Explicit chat subcommand, or no subcommand: default to the TUI.
		return cmdChat(parseConfigFlag(rest), appendPrompts, trust)
	default:
		// Not a subcommand: treat as a project path. chdir there, then
		// launch the TUI so project context and tool operations resolve
		// relative to that directory.
		if err := os.Chdir(sub); err != nil {
			return fmt.Errorf("chdir %s: %w", sub, err)
		}
		return cmdChat(parseConfigFlag(rest), appendPrompts, trust)
	}
}

// splitSubcommand returns the first non-flag argument as the subcommand
// and the remaining arguments (including any leading flags) as rest.
func splitSubcommand(args []string) (string, []string) {
	for i, a := range args {
		if !strings.HasPrefix(a, "-") {
			return a, args[i+1:]
		}
	}
	return "", nil
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

// omegaHome returns the omega home directory: OMEGA_HOME env var,
// the binary's directory (release-folder installs), or ~/.omega for
// bin-dir installs (see homeForExe). This is where config, db, and
// skills live.
func omegaHome() string {
	exePath := ""
	if exe, err := os.Executable(); err == nil {
		exePath = exe
	}
	userHome, _ := os.UserHomeDir()
	return homeForExe(os.Getenv("OMEGA_HOME"), exePath, userHome)
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

// resolveHomePaths fills in home-relative defaults for DBPath and
// Skills.Dir when the config left them at their relative defaults.
// This lets omega find its db and skills from any CWD. Returns an
// error if the home directory cannot be created.
func resolveHomePaths(cfg *Config) error {
	home := omegaHome()
	if cfg.Store.DBPath == "omega.db" {
		cfg.Store.DBPath = datadir.DBPath(home, "omega.db")
	}
	if cfg.Kanban.DBPath == "kanban.db" {
		// the relative default resolves through the board registry:
		// the pointer file names the board "" means (missing = "main")
		dsn, derr := kanbanModule.PointerDSN(home)
		if derr != nil {
			return fmt.Errorf("kanban board registry: %w", derr)
		}
		cfg.Kanban.DBPath = dsn
	}
	if cfg.Skills.Dir == "skills" {
		cfg.Skills.Dir = home + "/skills"
	}
	if cfg.Memory.File == "memory.md" {
		// card t_7e2b7002: the default agent's memory files live in its
		// profile dir - MigrateDefaultAgent (run(), before any command)
		// moved the legacy home files there. Pointing the default at the
		// old home path would strand the migrated content and silently
		// start a fresh memory - the read side must follow the move.
		cfg.Memory.File = home + "/agents/default/memory.md"
	}
	if cfg.Memory.UserProfileFile == "user.md" {
		cfg.Memory.UserProfileFile = home + "/agents/default/user.md"
	}
	if cfg.Logging.File == "omega.log" {
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
		agent_loop.NewPlugin(),
		&provider.Plugin{},
		store.NewPlugin(),
		skills.NewPlugin(),
		compactor.NewPlugin(),
		memory.NewPlugin(),
		// card t_13a2af8a: mount order is declared data - the
		// notifications consumers (delegate, kanban, cron) carry
		// Requires edges naming the seam, and agent.MountAll's topoSort
		// is the only ordering. Slice order below is a stable tiebreak
		// only (it keeps prompt sections in authoring order among
		// independent plugins; it carries no dependency semantics).
		notifications.NewPlugin(),
		prompt.NewPlugin(),
		tools.NewPlugin(),
		// card t_2ecf9c51: verdict cards - verdict tool, the
		// verdicts seam, and the /verdict TUI command. Requires store
		// (the hub captures it at Mount to persist card + verdict rows);
		// placed after the tools module so the prompt section lands
		// after the tools list.
		verdict.NewPlugin(),
		mcpPlugin,
		delegate.NewPlugin(),
		web.NewPlugin(),
		httpchannel.NewPlugin(),
		tui.NewPlugin(),
		trust.NewPlugin(),
		cron.NewPlugin(),
		kanbanModule.NewPlugin(),
	}, nil
}

// buildConfigs and webBaseURL moved out in arc 2b (t_889388cc): the
// plugins declare their config section (agent.Describer's ConfigKey),
// agent.RouteConfigs builds the per-plugin map from the raw Config
// via (*Config).ConfigSection in config.go.

// newAgent wires config into a provider, agent, store, and extensions
// via the in-process plugin system. The store and plugin context are
// returned so the caller can close the store and start channels.
func newAgent(cfg Config, appendPrompts []string, trust trustFlags) (*core.Agent, core.StoreProvider, core.LoggerProvider, *agent.Context, error) {
	plugins, err := buildPlugins(cfg)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("build plugins: %w", err)
	}
	ctx := &agent.Context{
		CWD:     cwd(),
		Configs: agent.RouteConfigs(plugins, cfg),
	}
	if err := agent.MountAll(plugins, ctx); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("mount extensions: %w", err)
	}

	// The store resolves BEFORE the agent is built: the captioner
	// (vision_model knob) holds the caption sink from the start, and
	// NewFromContext copies ctx.Captioner onto every agent it creates.
	if ctx.Store != nil {
		if err := ctx.Store.Open(cfg.Store.DBPath); err != nil {
			return nil, nil, nil, nil, fmt.Errorf("open store: %w", err)
		}
	} else {
		ctx.Logger.Errorf("omega: no store extension loaded — using in-memory store (sessions will not persist)")
		s, err := store.Open(":memory:")
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("open in-memory store: %w", err)
		}
		ctx.Store = s
	}
	wireCaptioner(cfg, ctx)

	ag := core.NewFromContext(ctx, core.AgentOptions{
		MaxTurns:      cfg.MaxTurns,
		MaxToolOutput: cfg.Compaction.MaxToolOutput,
		PromptCustom:  cfg.SystemPrompt,
		PromptAppend:  appendPrompts,
		PromptContext: trustGate(ctx.Trust, cwd(), trust, false),
		CWD:           cwd(),
	})

	return ag, ctx.Store, ctx.Logger, ctx, nil
}

// wireCaptioner builds the image captioner (vision_model knob) onto the
// context: a pinned vision-model provider copy, the store as the caption
// sink, and the logger. An unset knob (or a store without caption
// support) changes nothing. Called after the store resolves, before any
// agent is built from the context - serve, TUI, and cron agents all
// inherit through NewFromContext.
func wireCaptioner(cfg Config, pctx *agent.Context) {
	if cfg.VisionModel == "" || pctx == nil {
		return
	}
	var sink agent.CaptionStore
	if cs, ok := pctx.Store.(agent.CaptionStore); ok {
		sink = cs
	}
	pctx.Captioner = provider.NewCaptioner(pctx.Provider, cfg.VisionModel, sink, pctx.Logger)
	if sink == nil && pctx.Logger != nil {
		pctx.Logger.Printf("omega: vision_model set but the store does not support captions - captions cache in-process only")
	}
}

// signalContext returns a context cancelled on SIGINT/SIGTERM.
func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

// cmdServe loads config, wires the agent, and starts all mounted
// channels (HTTP, Discord, etc.) until a signal triggers graceful
// shutdown.
func cmdServe(configPath string, appendPrompts []string, trust trustFlags) error {
	cfg, err := LoadConfig(resolveConfigPath(configPath))
	if err != nil {
		return err
	}
	if err := resolveHomePaths(&cfg); err != nil {
		return err
	}
	ai.SetHTTPTimeout(cfg.HTTPTimeout)
	ag, store, logger, pctx, err := newAgent(cfg, appendPrompts, trust)
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
	WatchConfig(configFile, func(newCfg Config) {
		hotApply(newCfg, pctx)
	})
	errCh := make(chan error, len(pctx.Channels))
	for _, ch := range pctx.Channels {
		deps := core.ChannelDeps{
			Ctx:      pctx,
			Store:    store,
			Settings: &omegaSettings{path: configFile, pctx: pctx},
			Version:  omegaVersion,
		}
		go func(c core.Channel) {
			errCh <- c.Start(ctx, deps)
		}(ch)
	}

	// Start the cron runner (if mounted) with the fully-wired agent,
	// store, memory seam and logger. Runs until shutdown, alongside
	// the channels.
	if pctx.Cron != nil {
		go func() {
			if err := pctx.Cron.Start(ctx, core.CronDeps{Ctx: pctx, Agent: ag, Store: store}); err != nil {
				fmt.Fprintf(os.Stderr, "omega: cron runner: %v\n", err)
				errCh <- err
			}
		}()
	}

	fmt.Printf("omega: serving %d channel(s) (model %s)\n", len(pctx.Channels), ag.ModelName())
	if logger != nil {
		logger.Printf("omega: serving %d channel(s), model %s", len(pctx.Channels), ag.ModelName())
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
	if pctx.Cron != nil {
		if err := pctx.Cron.Stop(); err != nil {
			fmt.Fprintf(os.Stderr, "omega: cron stop error: %v\n", err)
		}
	}
	return nil
}

// cmdRun loads config and runs the agent once with the given prompt,
// printing the final response.
func cmdRun(configPath string, args []string, trust trustFlags) error {
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
	}
	ai.SetHTTPTimeout(cfg.HTTPTimeout)
	ag, store, logger, _, err := newAgent(cfg, appendPrompts, trust)
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
		ctx = core.WithRunOrigin(ctx, core.RunOriginDelegate)
	} else {
		ctx = core.WithRunOrigin(ctx, core.RunOriginInteractive)
	}

	var userMsg core.Message
	if len(images) > 0 {
		userMsg = core.NewUserWithImages(prompt, images)
	} else {
		userMsg = core.NewUser(prompt)
	}
	messages := []core.Message{userMsg}
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

// cmdChat loads config and launches the interactive Bubble Tea TUI.
func cmdChat(configPath string, appendPrompts []string, trust trustFlags) error {
	cfg, err := LoadConfig(resolveConfigPath(configPath))
	if err != nil {
		return err
	}
	if err := resolveHomePaths(&cfg); err != nil {
		return err
	}
	ai.SetHTTPTimeout(cfg.HTTPTimeout)

	// Build the plugin context to get the store and skills.
	plugins, err := buildPlugins(cfg)
	if err != nil {
		return fmt.Errorf("build plugins: %w", err)
	}
	pctx := &agent.Context{
		CWD:     cwd(),
		Configs: agent.RouteConfigs(plugins, cfg),
	}
	if err := agent.MountAll(plugins, pctx); err != nil {
		return fmt.Errorf("mount extensions: %w", err)
	}
	if pctx.Logger != nil {
		defer pctx.Logger.Close()
	}

	// Open the store.
	if pctx.Store != nil {
		if err := pctx.Store.Open(cfg.Store.DBPath); err != nil {
			return fmt.Errorf("open store: %w", err)
		}
	} else {
		pctx.Logger.Errorf("omega: no store extension loaded — using in-memory store (sessions will not persist)")
		s, err := store.Open(":memory:")
		if err != nil {
			return fmt.Errorf("open in-memory store: %w", err)
		}
		pctx.Store = s
	}
	defer pctx.Store.Close()

	wireCaptioner(cfg, pctx)

	// Load skills.
	var skillsList []core.Skill
	if pctx.Skills != nil {
		skillsList, err = pctx.Skills.LoadSkills(cfg.Skills.Dir)
		if err != nil {
			return fmt.Errorf("load skills: %w", err)
		}
	}

	// Hot-reload non-disruptive config changes (theme, compaction,
	// notifications) when config.yaml changes on disk.
	configFile := resolveConfigPath(configPath)
	WatchConfig(configFile, func(newCfg Config) {
		hotApply(newCfg, pctx)
	})

	return runChat(cfg, pctx, skillsList, appendPrompts, trust)
}

// runChat launches the frontend (TUI) via the Frontend seam.
func runChat(cfg Config, pctx *agent.Context, skills []core.Skill, appendPrompts []string, trust trustFlags) error {
	if pctx.Frontend == nil {
		return fmt.Errorf("no frontend extension loaded — mount a frontend plugin (e.g. tui)")
	}
	return pctx.Frontend.Run(context.Background(), pctx, core.FrontendOptions{
		ModelName:     cfg.Provider.ModelName,
		ProviderType:  cfg.Provider.Type,
		Compaction:    &cfg.Compaction,
		PromptCustom:  cfg.SystemPrompt,
		PromptAppend:  appendPrompts,
		PromptContext: trustGate(pctx.Trust, cwd(), trust, true),
		Skills:        skills,
		ThemeName:     cfg.Theme,
		TrustState:    trustStatus(pctx.Trust, cwd(), trust),
		Notifications: cfg.Notifications,
		CWD:           cwd(),
		Version:       omegaVersion,
	})
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
