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
	"github.com/EndoTheDev/omega/gateway"
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
	// Set OMEGA_HOME early so all child processes (extensions) inherit it.
	home := omegaHome()
	os.Setenv("OMEGA_HOME", home)
	// Set OMEGA_SKILLS_DIR so the core-tools extension can read skills.
	os.Setenv("OMEGA_SKILLS_DIR", home+"/skills")
	// Set OMEGA_BIN so the core-delegate extension can spawn subagents.
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
	case "test":
		return cmdTest()
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

// setProviderEnvVars sets OMEGA_PROVIDER_* env vars so the core-provider
// extension inherits them when spawned.
func setProviderEnvVars(cfg gateway.Config) {
	os.Setenv("OMEGA_PROVIDER_TYPE", cfg.Provider.Type)
	os.Setenv("OMEGA_PROVIDER_MODEL", cfg.Provider.ModelName)
	os.Setenv("OMEGA_PROVIDER_HOST", cfg.Provider.Host)
}

// newAgent wires config into a provider, agent, store, and extensions.
// The store is returned so the caller can close it. The extension manager
// is returned so callers that run the TUI can close extensions on shutdown.
func newAgent(cfg gateway.Config, appendPrompts []string, trust trustFlags) (*agent.Agent, agent.StoreProvider, agent.ExtensionManager, error) {
	setProviderEnvVars(cfg)

	tools := map[string]agent.Tool{}
	ag := agent.NewAgent(nil, tools, cfg.MaxTurns) // provider wired after extensions
	ag.SetToolProvider(agent.DefaultToolProvider{ToolsMap: tools})
	ag.SetCWD(cwd())
	ag.SetPromptCustom(cfg.SystemPrompt)
	ag.SetPromptAppend(appendPrompts)
	ag.SetPromptContext(resolveProjectContext(cwd(), trust.approve, trust.noApprove, false))
	mgr, err := loadExtensions(cfg.Extensions, cfg.Provider.APIKey)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("load extensions: %w", err)
	}
	ag.SetExtensions(mgr)

	// Create the provider from the provider-seam extension.
	if _, ok := mgr.SeamProviders()["provider"]; !ok {
		return nil, nil, nil, fmt.Errorf("no provider extension loaded — install core-provider in extensions/")
	}
	provider := ai.ExtensionProvider{Dispatcher: mgr}
	ag.SetProvider(provider)

	// Wire the compactor. Prefer the compactor-seam extension; when
	// none is loaded, compaction is disabled and the agent surfaces a
	// friendly error on context overflow.
	if cp := mgr.CompactorProvider(cfg.Compaction); cp != nil {
		ag.SetCompactor(cp)
	} else {
		fmt.Fprintf(os.Stderr, "omega: no compactor extension loaded — context compaction disabled (install core-compactor in extensions/)\n")
	}
	ag.SetMaxToolOutput(cfg.Compaction.MaxToolOutput)

	// Validate PluginsConfig against loaded extension seams.
	seams := mgr.SeamProviders()
	if cfg.Plugins.PromptBuilder != "" && cfg.Plugins.PromptBuilder != "default" {
		if extName, ok := seams["prompt_builder"]; ok {
			if extName != cfg.Plugins.PromptBuilder {
				fmt.Fprintf(os.Stderr, "omega: warning: prompt_builder config %q does not match extension %q, using default\n", cfg.Plugins.PromptBuilder, extName)
			}
		} else {
			fmt.Fprintf(os.Stderr, "omega: warning: prompt_builder config %q but no extension provides that seam, using default\n", cfg.Plugins.PromptBuilder)
		}
	}
	if cfg.Plugins.Compactor != "" && cfg.Plugins.Compactor != "default" {
		if extName, ok := seams["compactor"]; ok {
			if extName != cfg.Plugins.Compactor {
				fmt.Fprintf(os.Stderr, "omega: warning: compactor config %q does not match extension %q\n", cfg.Plugins.Compactor, extName)
			}
		} else {
			fmt.Fprintf(os.Stderr, "omega: warning: compactor config %q but no extension provides that seam — compaction disabled\n", cfg.Plugins.Compactor)
		}
	}

	// Open the store. Prefer the store-seam extension; fall back to
	// in-memory SQLite when no store extension is loaded.
	store, err := openStore(mgr, cfg.Store.DBPath)
	if err != nil {
		mgr.Close()
		return nil, nil, nil, err
	}
	return ag, store, mgr, nil
}

// openStore opens the session store. Prefers the store-seam extension;
// falls back to in-memory SQLite (no persistence) when no store
// extension is loaded.
func openStore(mgr agent.ExtensionManager, dbPath string) (agent.StoreProvider, error) {
	if sp := mgr.StoreProvider(); sp != nil {
		if err := sp.Open(dbPath); err != nil {
			return nil, fmt.Errorf("open store extension: %w", err)
		}
		return sp, nil
	}
	fmt.Fprintf(os.Stderr, "omega: no store extension loaded — using in-memory store (sessions will not persist)\n")
	s, err := gateway.Open(":memory:")
	if err != nil {
		return nil, fmt.Errorf("open in-memory store: %w", err)
	}
	return s, nil
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
	setProviderEnvVars(cfg)

	extMgr, err := loadExtensions(cfg.Extensions, cfg.Provider.APIKey)
	if err != nil {
		return fmt.Errorf("load extensions: %w", err)
	}
	defer extMgr.Close()

	// Open the store. Prefer the store-seam extension; fall back to
	// in-memory SQLite when no store extension is loaded.
	store, err := openStore(extMgr, cfg.Store.DBPath)
	if err != nil {
		return err
	}
	defer store.Close()

	skills, err := loadSkills(extMgr, cfg)
	if err != nil {
		return fmt.Errorf("load skills: %w", err)
	}

	// Hot-reload non-disruptive config changes (theme, compaction,
	// notifications) when config.yaml changes on disk.
	configFile := resolveConfigPath(configPath)
	gateway.WatchConfig(configFile, func(newCfg gateway.Config) {
		ai.SetHTTPTimeout(newCfg.HTTPTimeout)
	})

	return runChat(cfg.Provider, &cfg.Compaction, cfg.SystemPrompt, appendPrompts, resolveProjectContext(cwd(), trust.approve, trust.noApprove, true), store, skills, extMgr, cfg.Theme, trustState(cwd(), trust.approve, trust.noApprove), cfg.Notifications)
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

// loadSkills reads skills from the skills-seam extension. Returns empty
// when no skills extension is loaded.
func loadSkills(mgr agent.ExtensionManager, cfg gateway.Config) ([]agent.Skill, error) {
	if sp := mgr.SkillsProvider(); sp != nil {
		return sp.LoadSkills(cfg.Skills.Dir)
	}
	return nil, nil
}

// cmdTest runs a smoke test through the full agent pipeline using a
// fake provider. Verifies event ordering and tool execution.
func cmdTest() error {
	provider := ai.NewFakeProviderScripts("test",
		// Turn 1: response chunk + tool call.
		[]ai.StreamEvent{
			ai.ResponseChunk{Type: "response_chunk", Content: "Let me check"},
			ai.ToolCallEvent{Type: "tool_call", ToolCall: ai.ToolCall{ID: "t1", Name: "echo", Arguments: map[string]any{"msg": "hello"}}},
			ai.StreamEnd{Type: "stream_end", FinishReason: "tool_calls"},
		},
		// Turn 2: final response.
		[]ai.StreamEvent{
			ai.ResponseChunk{Type: "response_chunk", Content: "Done: hello"},
			ai.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		},
	)

	tools := map[string]agent.Tool{
		"echo": {
			Description: "Echo back the message",
			Parameters:  map[string]any{"type": "object", "properties": map[string]any{"msg": map[string]any{"type": "string"}}},
			Run: func(ctx context.Context, args map[string]any) (string, error) {
				msg, _ := args["msg"].(string)
				return msg, nil
			},
		},
	}

	ag := agent.NewAgent(provider, tools, 10)
	ag.SetToolProvider(agent.DefaultToolProvider{ToolsMap: tools})

	ch := ag.Run(context.Background(), []ai.Message{ai.NewUser("test")}, tools)

	var types []string
	for e := range ch {
		switch v := e.(type) {
		case agent.AgentStart:
			types = append(types, "agent_start")
		case agent.TurnStart:
			types = append(types, "turn_start")
		case agent.TurnEnd:
			types = append(types, "turn_end")
		case agent.AgentEnd:
			types = append(types, "agent_end")
			if v.FinishReason != "stop" {
				return fmt.Errorf("finish_reason = %s, want stop", v.FinishReason)
			}
		case agent.StreamEvent:
			switch v.Event.(type) {
			case ai.ResponseChunk:
				types = append(types, "response_chunk")
			case ai.ToolCallEvent:
				types = append(types, "tool_call")
			case ai.StreamEnd:
				types = append(types, "stream_end")
			}
		case agent.ToolResultEvent:
			types = append(types, "tool_result")
		}
	}

	// Verify we got the key events.
	want := []string{"agent_start", "turn_start", "response_chunk", "tool_call", "stream_end", "tool_result", "turn_end", "turn_start", "response_chunk", "stream_end", "turn_end", "agent_end"}
	if len(types) != len(want) {
		return fmt.Errorf("event count = %d, want %d (got %v)", len(types), len(want), types)
	}
	for i, w := range want {
		if types[i] != w {
			return fmt.Errorf("event %d = %s, want %s (full: %v)", i, types[i], w, types)
		}
	}

	fmt.Println("OK: agent pipeline smoke test passed")
	fmt.Printf("  events: %v\n", types)
	return nil
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
