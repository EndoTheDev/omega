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
	"github.com/EndoTheDev/omega/internal/ai"
	"github.com/EndoTheDev/omega/internal/gateway"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "omega:", err)
		os.Exit(1)
	}
}

// run parses the subcommand and dispatches. The first non-flag argument
// selects the subcommand; --config is accepted before or after it.
func run(args []string) error {
	sub, rest := splitSubcommand(args)
	switch sub {
	case "serve":
		return cmdServe(parseConfigFlag(rest))
	case "run":
		return cmdRun(parseConfigFlag(rest), rest)
	case "health":
		return cmdHealth(parseConfigFlag(rest))
	case "chat":
		return cmdChat(parseConfigFlag(rest))
	case "":
		return fmt.Errorf("no subcommand; expected serve, run, health, or chat")
	default:
		return fmt.Errorf("unknown subcommand %q; expected serve, run, health, or chat", sub)
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
	// Ensure the home directory exists so SQLite and extensions can
	// create their files. Non-fatal: if mkdir fails, the store open
	// will produce a clearer error.
	_ = os.MkdirAll(home, 0755)
}

// newAgent wires config into a provider, agent, store, and extensions.
// The store is returned so the caller can close it. The extension manager
// is returned so callers that run the TUI can close extensions on shutdown.
func newAgent(cfg gateway.Config) (*agent.Agent, *gateway.Store, agent.ExtensionManager, error) {
	provider, err := ai.NewProvider(cfg.Provider.Type, cfg.Provider.ModelName, cfg.Provider.Host, cfg.Provider.APIKey)
	if err != nil {
		return nil, nil, nil, err
	}
	ag := agent.NewAgent(provider, agent.NewRegistry(), 0)
	ag.SetCompaction(&cfg.Compaction)
	ag.SetSystemPrompt(buildSystemPrompt(cfg, nil))

	mgr, err := loadExtensions(cfg.Extensions, cfg.Provider.APIKey)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("load extensions: %w", err)
	}
	ag.SetExtensions(mgr)

	store, err := gateway.Open(cfg.Store.DBPath)
	if err != nil {
		mgr.Close()
		return nil, nil, nil, fmt.Errorf("open store: %w", err)
	}
	return ag, store, mgr, nil
}

// buildSystemPrompt assembles the agent's system prompt from the
// project context (AGENTS.md in the working directory), the built-in
// tools, loaded skills, the environment, and the config's custom prompt.
func buildSystemPrompt(cfg gateway.Config, skills []agent.Skill) string {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	return agent.BuildSystemPrompt(agent.PromptOptions{
		ProjectContext: agent.LoadProjectContext(cwd),
		Tools:          agent.NewRegistry(),
		Skills:         skills,
		CWD:            cwd,
		Custom:         cfg.SystemPrompt,
	})
}

// signalContext returns a context cancelled on SIGINT/SIGTERM.
func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

// cmdServe loads config, wires the agent, and serves HTTP until a signal
// triggers graceful shutdown.
func cmdServe(configPath string) error {
	cfg, err := gateway.LoadConfig(resolveConfigPath(configPath))
	if err != nil {
		return err
	}
	resolveHomePaths(&cfg)
	ag, store, mgr, err := newAgent(cfg)
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
func cmdRun(configPath string, args []string) error {
	prompt := strings.Join(stripConfigFlag(args), " ")
	if prompt == "" {
		return fmt.Errorf("run requires a prompt argument")
	}

	cfg, err := gateway.LoadConfig(resolveConfigPath(configPath))
	if err != nil {
		return err
	}
	resolveHomePaths(&cfg)
	ag, store, mgr, err := newAgent(cfg)
	if err != nil {
		return err
	}
	defer store.Close()
	defer mgr.Close()

	ctx, stop := signalContext()
	defer stop()

	messages := []ai.Message{ai.NewUser(prompt)}
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
func cmdChat(configPath string) error {
	cfg, err := gateway.LoadConfig(resolveConfigPath(configPath))
	if err != nil {
		return err
	}
	resolveHomePaths(&cfg)
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

	skills, err := loadSkills()
	if err != nil {
		return fmt.Errorf("load skills: %w", err)
	}
	return runChat(cfg.Provider, &cfg.Compaction, buildSystemPrompt(cfg, skills), store, skills, extMgr)
}

// loadExtensions returns an extension manager configured by the user. If
// extensions are disabled it returns a no-op manager.
func loadExtensions(cfg gateway.ExtensionsConfig, apiKey string) (agent.ExtensionManager, error) {
	if !cfg.Enabled {
		return agent.NoopManager{}, nil
	}
	mgr := &agent.StdioManager{}
	if err := mgr.Load(cfg.Dir, apiKey); err != nil {
		return nil, err
	}
	return mgr, nil
}

// loadSkills reads skills from OMEGA_SKILLS_DIR, or ~/.omega/skills/.
func loadSkills() ([]agent.Skill, error) {
	dir := os.Getenv("OMEGA_SKILLS_DIR")
	if dir == "" {
		dir = omegaHome() + "/skills"
	}
	return agent.LoadSkills(dir)
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
