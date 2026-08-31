// Command omega is the single binary entry point for the omega agent.
// It ties together the serve, run, and health subcommands.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/ai"
	"github.com/EndoTheDev/omega/extensions/agent_loop"
	"github.com/EndoTheDev/omega/extensions/compactor"
	"github.com/EndoTheDev/omega/extensions/delegate"
	httpchannel "github.com/EndoTheDev/omega/extensions/http_channel"
	"github.com/EndoTheDev/omega/extensions/logging"
	"github.com/EndoTheDev/omega/extensions/mcp"
	"github.com/EndoTheDev/omega/extensions/memory"
	"github.com/EndoTheDev/omega/extensions/prompt"
	"github.com/EndoTheDev/omega/extensions/provider"
	"github.com/EndoTheDev/omega/extensions/skills"
	"github.com/EndoTheDev/omega/extensions/store"
	"github.com/EndoTheDev/omega/extensions/tools"
	"github.com/EndoTheDev/omega/extensions/trust"
	"github.com/EndoTheDev/omega/extensions/tui"
	"github.com/EndoTheDev/omega/extensions/web"
)

func main() {
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
  omega update              check for and install the latest release
  omega health          check the server at the configured port

Flags:
  --config <path>       config file (default: <home>/config.yaml)
  --append-system-prompt <text>   append to system prompt (repeatable)
  --approve             trust the current project's AGENTS.md
  --no-approve          skip the current project's AGENTS.md
  --version, -v         print version
  --help, -h            show this help
`

// run parses the subcommand and dispatches. The first non-flag argument
// selects the subcommand; --config is accepted before or after it. With
// no argument, the TUI starts. A non-subcommand argument is treated as a
// project path: omega chdirs there and starts the TUI.
func run(args []string) error {
	// Set OMEGA_HOME early so all child processes inherit it.
	home := omegaHome()
	os.Setenv("OMEGA_HOME", home)
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
	case "export":
		return cmdExport(parseConfigFlag(rest), rest)
	case "insights":
		return cmdInsights(parseConfigFlag(rest), rest)
	case "update":
		return cmdUpdate()
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
// or the directory containing the omega binary, or ~/.omega/ as a
// last-resort fallback. This is where config, db, and skills live.
func omegaHome() string {
	if dir := os.Getenv("OMEGA_HOME"); dir != "" {
		return dir
	}
	if exe, err := os.Executable(); err == nil {
		return filepath.Dir(exe)
	}
	// Fallback: ~/.omega/ if the binary path is unresolvable.
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return home + "/.omega"
}

// resolveConfigPath returns the --config value, or <home>/config.yaml
// when it exists, or "" to skip YAML entirely.
func resolveConfigPath(flagPath string) string {
	if flagPath != "" {
		return flagPath
	}
	homePath := omegaHome() + "/config.yaml"
	if _, err := os.Stat(homePath); err == nil {
		return homePath
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
		cfg.Store.DBPath = home + "/omega.db"
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
		prompt.NewPlugin(),
		tools.NewPlugin(),
		mcpPlugin,
		delegate.NewPlugin(),
		web.NewPlugin(),
		httpchannel.NewPlugin(),
		tui.NewPlugin(),
		trust.NewPlugin(),
	}, nil
}

// buildConfigs routes Config sub-sections into per-extension
// typed Config structs. Each extension reads its own config via
// ctx.Configs["<name>"].(<ext>.Config) — no external import needed.
func buildConfigs(cfg Config) map[string]any {
	return map[string]any{
		"store":        store.Config{DBPath: cfg.Store.DBPath},
		"provider":     provider.Config{Type: cfg.Provider.Type, ModelName: cfg.Provider.ModelName, Host: cfg.Provider.Host, APIKey: cfg.Provider.APIKey},
		"http_channel": httpchannel.Config{Port: cfg.Server.Port},
		"skills":       skills.Config{Dir: cfg.Skills.Dir},
		"memory":       memory.Config{Enabled: cfg.Memory.Enabled, UserProfileEnabled: cfg.Memory.UserProfileEnabled, CharLimit: cfg.Memory.CharLimit, UserProfileCharLimit: cfg.Memory.UserProfileCharLimit, File: cfg.Memory.File, UserProfileFile: cfg.Memory.UserProfileFile},
		"logging":      logging.Config{Enabled: cfg.Logging.Enabled, File: cfg.Logging.File},
		"compactor":    cfg.Compaction,
		"web":          web.Config{APIKey: cfg.Provider.APIKey},
		"trust":        trust.Config{Home: omegaHome()},
	}
}

// newAgent wires config into a provider, agent, store, and extensions
// via the in-process plugin system. The store and plugin context are
// returned so the caller can close the store and start channels.
func newAgent(cfg Config, appendPrompts []string, trust trustFlags) (*agent.Agent, agent.StoreProvider, agent.LoggerProvider, *agent.Context, error) {
	ctx := &agent.Context{
		CWD:     cwd(),
		Configs: buildConfigs(cfg),
	}
	plugins, err := buildPlugins(cfg)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("build plugins: %w", err)
	}
	if err := agent.MountAll(plugins, ctx); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("mount extensions: %w", err)
	}

	ag := agent.NewFromContext(ctx, agent.AgentOptions{
		MaxTurns:      cfg.MaxTurns,
		MaxToolOutput: cfg.Compaction.MaxToolOutput,
		PromptCustom:  cfg.SystemPrompt,
		PromptAppend:  appendPrompts,
		PromptContext: ctx.Trust.ResolveContext(cwd(), trust.approve, trust.noApprove, false),
		CWD:           cwd(),
	})

	// Open the store.
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

	return ag, ctx.Store, ctx.Logger, ctx, nil
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
	errCh := make(chan error, len(pctx.Channels))
	for _, ch := range pctx.Channels {
		deps := agent.ChannelDeps{Ctx: pctx, Store: store}
		go func(c agent.Channel) {
			errCh <- c.Start(ctx, deps)
		}(ch)
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

	var userMsg ai.Message
	if len(images) > 0 {
		userMsg = ai.NewUserWithImages(prompt, images)
	} else {
		userMsg = ai.NewUser(prompt)
	}
	messages := []ai.Message{userMsg}
	var response strings.Builder
	for event := range ag.Run(ctx, messages, nil) {
		switch e := event.(type) {
		case agent.StreamEvent:
			if chunk, ok := e.Event.(ai.ResponseChunk); ok {
				response.WriteString(chunk.Content)
			}
		case agent.AgentEnd:
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
	pctx := &agent.Context{
		CWD:     cwd(),
		Configs: buildConfigs(cfg),
	}
	plugins, err := buildPlugins(cfg)
	if err != nil {
		return fmt.Errorf("build plugins: %w", err)
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

	// Load skills.
	var skillsList []agent.Skill
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
		ai.SetHTTPTimeout(newCfg.HTTPTimeout)
	})

	return runChat(cfg, pctx, skillsList, appendPrompts, trust)
}

// runChat launches the frontend (TUI) via the Frontend seam.
func runChat(cfg Config, pctx *agent.Context, skills []agent.Skill, appendPrompts []string, trust trustFlags) error {
	if pctx.Frontend == nil {
		return fmt.Errorf("no frontend extension loaded — mount a frontend plugin (e.g. tui)")
	}
	return pctx.Frontend.Run(context.Background(), pctx, agent.FrontendOptions{
		ModelName:     cfg.Provider.ModelName,
		ProviderType:  cfg.Provider.Type,
		Compaction:    &cfg.Compaction,
		PromptCustom:  cfg.SystemPrompt,
		PromptAppend:  appendPrompts,
		PromptContext: pctx.Trust.ResolveContext(cwd(), trust.approve, trust.noApprove, true),
		Skills:        skills,
		ThemeName:     cfg.Theme,
		TrustState:    pctx.Trust.State(cwd(), trust.approve, trust.noApprove),
		Notifications: cfg.Notifications,
		CWD:           cwd(),
		Version:       omegaVersion,
	})
}

// omegaVersion is set via ldflags at build time:
//	go build -ldflags "-X main.omegaVersion=v0.1.0"
var omegaVersion = "dev"

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
