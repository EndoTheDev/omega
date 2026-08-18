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

	"github.com/EndoTheDev/omega/internal/agent"
	"github.com/EndoTheDev/omega/internal/harness"
	"github.com/EndoTheDev/omega/internal/ai"
	"github.com/EndoTheDev/omega/internal/gateway"
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
  --extension <path>, -e <path>   load an extension (repeatable)
  --no-extensions       disable extension loading
  --project-extensions  also load <cwd>/.omega/extensions/
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
	ext := parseExtensionArgs(rest)
	trust := parseTrustArgs(rest)
	switch sub {
	case "serve":
		return cmdServe(parseConfigFlag(rest), appendPrompts, ext, trust)
	case "run":
		return cmdRun(parseConfigFlag(rest), rest, ext, trust)
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
		return cmdChat(parseConfigFlag(rest), appendPrompts, ext, trust)
	default:
		// Not a subcommand: treat as a project path. chdir there, then
		// launch the TUI so project context and tool operations resolve
		// relative to that directory.
		if err := os.Chdir(sub); err != nil {
			return fmt.Errorf("chdir %s: %w", sub, err)
		}
		return cmdChat(parseConfigFlag(rest), appendPrompts, ext, trust)
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

// extFlags holds the extension-related CLI flags. These are CLI-only:
// they have no YAML or env equivalent.
type extFlags struct {
	explicit []string // --extension/-e paths (repeatable)
	noExt    bool     // --no-extensions
	project  bool     // --project-extensions
}

// parseExtensionArgs extracts --extension/-e, --no-extensions, and
// --project-extensions from args. Supports both "--flag value" and
// "--flag=value" forms for the value-taking flags.
func parseExtensionArgs(args []string) extFlags {
	var f extFlags
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--no-extensions":
			f.noExt = true
		case a == "--project-extensions":
			f.project = true
		case a == "--extension" || a == "-e":
			if i+1 < len(args) {
				f.explicit = append(f.explicit, args[i+1])
				i++
			}
		case strings.HasPrefix(a, "--extension="):
			f.explicit = append(f.explicit, strings.TrimPrefix(a, "--extension="))
		case strings.HasPrefix(a, "-e="):
			f.explicit = append(f.explicit, strings.TrimPrefix(a, "-e="))
		}
	}
	return f
}

// stripExtensionArgs removes --extension/-e, --no-extensions, and
// --project-extensions (and their values) from args, so the remaining
// arguments are the run prompt.
func stripExtensionArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--no-extensions", a == "--project-extensions":
			continue
		case a == "--extension" || a == "-e":
			i++ // skip the value
			continue
		case strings.HasPrefix(a, "--extension="), strings.HasPrefix(a, "-e="):
			continue
		}
		out = append(out, args[i])
	}
	return out
}

// applyExtFlags folds CLI extension flags into the config. --no-extensions
// wins over everything; otherwise --extension/-e and --project-extensions
// each force extensions on.
func applyExtFlags(cfg *gateway.Config, f extFlags) {
	if f.noExt {
		cfg.Extensions.Enabled = false
		return
	}
	if len(f.explicit) > 0 {
		cfg.Extensions.Enabled = true
		cfg.Extensions.Explicit = f.explicit
	}
	if f.project {
		cfg.Extensions.Enabled = true
		cfg.Extensions.Project = true
	}
}

// omegaHome returns the omega home directory: OMEGA_HOME env var,
// or the directory containing the omega binary, or ~/.omega/ as a
// last-resort fallback. This is where config, db, skills, and
// extensions live when omega is installed globally and invoked from
// any directory.
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
// Extensions.Dir when the config left them at their relative defaults
// and no env var overrode them. This lets omega find its db and
// extensions from any CWD. It also ensures the home directory exists
// so the SQLite store can open its file.
func resolveHomePaths(cfg *gateway.Config) {
	home := omegaHome()
	if cfg.Store.DBPath == "omega.db" {
		cfg.Store.DBPath = home + "/omega.db"
	}
	if cfg.Extensions.Dir == "extensions" {
		cfg.Extensions.Dir = home + "/extensions"
	}
	if cfg.Skills.Dir == "skills" {
		cfg.Skills.Dir = home + "/skills"
	}
	// Ensure the home directory exists so SQLite and extensions can
	// create their files. Non-fatal: if mkdir fails, the store open
	// will produce a clearer error.
	_ = os.MkdirAll(home, 0755)
}

// newAgent wires config into a provider, agent, store, and extensions.
// The store is returned so the caller can close it. The extension manager
// is returned so callers that run the TUI can close extensions on shutdown.
func newAgent(cfg gateway.Config, appendPrompts []string, trust trustFlags) (*agent.Agent, *gateway.Store, agent.ExtensionManager, error) {
	provider, err := ai.NewProvider(cfg.Provider.Type, cfg.Provider.ModelName, cfg.Provider.Host, cfg.Provider.APIKey)
	if err != nil {
		return nil, nil, nil, err
	}
	ag := agent.NewAgent(provider, agent.NewRegistry(), 0)
	ag.SetPromptBuilder(agent.DefaultPromptBuilder{
		Prompt: buildSystemPrompt(cfg, nil, appendPrompts, resolveProjectContext(cwd(), trust.approve, trust.noApprove, false)),
	})
	mgr, err := loadExtensions(cfg.Extensions, cfg.Provider.APIKey)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("load extensions: %w", err)
	}
	ag.SetExtensions(mgr)
	ag.SetCompactor(agent.DefaultCompactor{
		Provider:   provider,
		Config:     &cfg.Compaction,
		Extensions: mgr,
	})
	ag.SetMaxToolOutput(cfg.Compaction.MaxToolOutput)

	store, err := gateway.Open(cfg.Store.DBPath)
	if err != nil {
		mgr.Close()
		return nil, nil, nil, fmt.Errorf("open store: %w", err)
	}
	return ag, store, mgr, nil
}

// buildSystemPrompt assembles the agent's system prompt from the
// resolved project context, the built-in tools, loaded skills, the
// environment, the config's custom prompt, and any
// --append-system-prompt values.
func buildSystemPrompt(cfg gateway.Config, skills []agent.Skill, appendPrompts []string, projectContext string) string {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	return harness.BuildSystemPrompt(harness.PromptOptions{
		ProjectContext: projectContext,
		Skills:         skills,
		CWD:            cwd,
		Custom:         cfg.SystemPrompt,
		Append:         appendPrompts,
	})
}

// signalContext returns a context cancelled on SIGINT/SIGTERM.
func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

// cmdServe loads config, wires the agent, and serves HTTP until a signal
// triggers graceful shutdown.
func cmdServe(configPath string, appendPrompts []string, ext extFlags, trust trustFlags) error {
	cfg, err := gateway.LoadConfig(resolveConfigPath(configPath))
	if err != nil {
		return err
	}
	applyExtFlags(&cfg, ext)
	resolveHomePaths(&cfg)
	ai.SetHTTPTimeout(cfg.HTTPTimeout)
	ag, store, mgr, err := newAgent(cfg, appendPrompts, trust)
	if err != nil {
		return err
	}
	defer store.Close()
	defer mgr.Close()

	ctx, stop := signalContext()
	defer stop()

	srv := gateway.NewServer(ag, nil, store)
	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	fmt.Printf("omega: serving on %s (model %s)\n", addr, ag.ModelName())
	return srv.Serve(ctx, addr)
}

// cmdRun loads config and runs the agent once with the given prompt,
// printing the final response.
func cmdRun(configPath string, args []string, ext extFlags, trust trustFlags) error {
	appendPrompts := parseAppendPrompts(args)
	args = stripAppendPrompts(stripExtensionArgs(stripTrustArgs(stripConfigFlag(args))))
	prompt, images, err := parseFileArgs(args)
	if err != nil {
		return err
	}
	if prompt == "" && len(images) == 0 {
		return fmt.Errorf("run requires a prompt argument")
	}

	cfg, err := gateway.LoadConfig(resolveConfigPath(configPath))
	if err != nil {
		return err
	}
	applyExtFlags(&cfg, ext)
	resolveHomePaths(&cfg)
	ai.SetHTTPTimeout(cfg.HTTPTimeout)
	ag, store, mgr, err := newAgent(cfg, appendPrompts, trust)
	if err != nil {
		return err
	}
	defer store.Close()
	defer mgr.Close()

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
func cmdChat(configPath string, appendPrompts []string, ext extFlags, trust trustFlags) error {
	cfg, err := gateway.LoadConfig(resolveConfigPath(configPath))
	if err != nil {
		return err
	}
	applyExtFlags(&cfg, ext)
	resolveHomePaths(&cfg)
	ai.SetHTTPTimeout(cfg.HTTPTimeout)
	// Open the session store so the TUI can persist conversations across
	// runs. cmdChat owns the store and closes it on every exit path
	// (/exit, Ctrl+C, or an error in p.Run).
	store, err := gateway.Open(cfg.Store.DBPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer store.Close()

	extMgr, err := loadExtensions(cfg.Extensions, cfg.Provider.APIKey)
	if err != nil {
		return fmt.Errorf("load extensions: %w", err)
	}
	defer extMgr.Close()

	skills, err := loadSkills(cfg)
	if err != nil {
		return fmt.Errorf("load skills: %w", err)
	}
	return runChat(cfg.Provider, &cfg.Compaction, buildSystemPrompt(cfg, skills, appendPrompts, resolveProjectContext(cwd(), trust.approve, trust.noApprove, true)), store, skills, extMgr, cfg.Theme, trustState(cwd(), trust.approve, trust.noApprove), cfg.Notifications)
}

// loadExtensions returns an extension manager configured by the user. If
// extensions are disabled it returns a no-op manager. When enabled, it
// loads the main dir, the project dir (when --project-extensions was
// passed), and any explicit --extension/-e paths.
func loadExtensions(cfg gateway.ExtensionsConfig, apiKey string) (agent.ExtensionManager, error) {
	if !cfg.Enabled {
		return agent.NoopManager{}, nil
	}
	mgr := &agent.StdioManager{}

	dirs := []string{cfg.Dir}
	if cfg.Project {
		cwd, err := os.Getwd()
		if err != nil {
			cwd = "."
		}
		dirs = append(dirs, filepath.Join(cwd, ".omega", "extensions"))
	}
	for _, d := range dirs {
		if err := mgr.Load(d, apiKey); err != nil {
			return nil, err
		}
	}
	for _, p := range cfg.Explicit {
		if err := mgr.LoadFile(p, apiKey); err != nil {
			// Non-fatal: log and skip. One bad explicit path does not
			// kill the manager.
			fmt.Fprintf(os.Stderr, "omega: extension %s: %v\n", p, err)
		}
	}
	return mgr, nil
}

// loadSkills reads skills from the configured skills directory.
func loadSkills(cfg gateway.Config) ([]agent.Skill, error) {
	return harness.LoadSkills(cfg.Skills.Dir)
}

// cmdHealth checks whether the server is reachable at the configured port.
func cmdHealth(configPath string) error {
	cfg, err := gateway.LoadConfig(resolveConfigPath(configPath))
	if err != nil {
		return err
	}
	url := fmt.Sprintf("http://localhost:%d/health", cfg.Server.Port)
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
