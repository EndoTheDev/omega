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
	"github.com/EndoTheDev/omega/extensions/mcp"
	"github.com/EndoTheDev/omega/extensions/prompt"
	"github.com/EndoTheDev/omega/extensions/provider"
	"github.com/EndoTheDev/omega/extensions/skills"
	"github.com/EndoTheDev/omega/extensions/store"
	"github.com/EndoTheDev/omega/extensions/tools"
	"github.com/EndoTheDev/omega/extensions/web"
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
	case "test":
		return cmdTest()
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
// This lets omega find its db and skills from any CWD.
func resolveHomePaths(cfg *gateway.Config) {
	home := omegaHome()
	if cfg.Store.DBPath == "omega.db" {
		cfg.Store.DBPath = home + "/omega.db"
	}
	if cfg.Skills.Dir == "skills" {
		cfg.Skills.Dir = home + "/skills"
	}
	// Ensure the home directory exists so SQLite can create its file.
	_ = os.MkdirAll(home, 0755)
}

// buildPlugins creates the list of in-process extensions from config.
// Extensions are compiled into omega — config controls their settings,
// not whether they're loaded. Each plugin reads its config from
// ctx.Config during Mount.
func buildPlugins(cfg gateway.Config) ([]agent.Plugin, error) {
	mcpPlugin, err := mcp.NewPluginFromEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "omega: mcp bridge: %v\n", err)
		mcpPlugin = mcp.NewPlugin(nil)
	}
	return []agent.Plugin{
		agent_loop.NewPlugin(),
		&provider.Plugin{},
		store.NewPlugin(),
		skills.NewPlugin(),
		compactor.NewPlugin(),
		prompt.NewPlugin(cfg.Skills.Dir),
		tools.NewPlugin(),
		mcpPlugin,
		delegate.NewPlugin(),
		web.NewPlugin(),
	}, nil
}

// newAgent wires config into a provider, agent, store, and extensions
// via the in-process plugin system. The store is returned so the caller
// can close it.
func newAgent(cfg gateway.Config, appendPrompts []string, trust trustFlags) (*agent.Agent, agent.StoreProvider, error) {
	ctx := &agent.Context{
		CWD:    cwd(),
		Config: cfg,
	}
	plugins, err := buildPlugins(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("build plugins: %w", err)
	}
	if err := agent.MountAll(plugins, ctx); err != nil {
		return nil, nil, fmt.Errorf("mount extensions: %w", err)
	}

	tools := map[string]agent.Tool{}
	ag := agent.NewAgent(ctx.Provider, tools, cfg.MaxTurns)
	ag.SetToolProvider(agent.DefaultToolProvider{ToolsMap: tools})
	ag.SetToolProviders(ctx.ToolProviders)
	ag.SetCWD(cwd())
	ag.SetPromptCustom(cfg.SystemPrompt)
	ag.SetPromptAppend(appendPrompts)
	ag.SetPromptContext(resolveProjectContext(cwd(), trust.approve, trust.noApprove, false))
	ag.SetPromptBuilder(ctx.PromptBuilder)
	ag.SetExtensionInfos(ctx.Infos)
	ag.SetMaxToolOutput(cfg.Compaction.MaxToolOutput)

	if ctx.Compactor != nil {
		ag.SetCompactionProvider(ctx.Compactor)
	}
	if ctx.InjectedMessages != nil {
		ag.SetInjectedMessages(ctx.InjectedMessages)
	}
	if ctx.PendingDelegations != nil {
		ag.SetPendingDelegations(ctx.PendingDelegations)
	}

	// Open the store.
	if ctx.Store != nil {
		if err := ctx.Store.Open(cfg.Store.DBPath); err != nil {
			return nil, nil, fmt.Errorf("open store: %w", err)
		}
	} else {
		fmt.Fprintf(os.Stderr, "omega: no store extension loaded — using in-memory store (sessions will not persist)\n")
		s, err := gateway.Open(":memory:")
		if err != nil {
			return nil, nil, fmt.Errorf("open in-memory store: %w", err)
		}
		ctx.Store = s
	}

	return ag, ctx.Store, nil
}

// signalContext returns a context cancelled on SIGINT/SIGTERM.
func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

// cmdServe loads config, wires the agent, and serves HTTP until a signal
// triggers graceful shutdown.
func cmdServe(configPath string, appendPrompts []string, trust trustFlags) error {
	cfg, err := gateway.LoadConfig(resolveConfigPath(configPath))
	if err != nil {
		return err
	}
	resolveHomePaths(&cfg)
	ai.SetHTTPTimeout(cfg.HTTPTimeout)
	ag, store, err := newAgent(cfg, appendPrompts, trust)
	if err != nil {
		return err
	}
	defer store.Close()

	ctx, stop := signalContext()
	defer stop()

	srv := gateway.NewServer(ag, nil, store)
	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	fmt.Printf("omega: serving on %s (model %s)\n", addr, ag.ModelName())
	return srv.Serve(ctx, addr)
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

	cfg, err := gateway.LoadConfig(resolveConfigPath(configPath))
	if err != nil {
		return err
	}
	resolveHomePaths(&cfg)
	ai.SetHTTPTimeout(cfg.HTTPTimeout)
	ag, store, err := newAgent(cfg, appendPrompts, trust)
	if err != nil {
		return err
	}
	defer store.Close()

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
	cfg, err := gateway.LoadConfig(resolveConfigPath(configPath))
	if err != nil {
		return err
	}
	resolveHomePaths(&cfg)
	ai.SetHTTPTimeout(cfg.HTTPTimeout)

	// Build the plugin context to get the store and skills.
	pctx := &agent.Context{
		CWD:    cwd(),
		Config: cfg,
	}
	plugins, err := buildPlugins(cfg)
	if err != nil {
		return fmt.Errorf("build plugins: %w", err)
	}
	if err := agent.MountAll(plugins, pctx); err != nil {
		return fmt.Errorf("mount extensions: %w", err)
	}

	// Open the store.
	if pctx.Store != nil {
		if err := pctx.Store.Open(cfg.Store.DBPath); err != nil {
			return fmt.Errorf("open store: %w", err)
		}
	} else {
		fmt.Fprintf(os.Stderr, "omega: no store extension loaded — using in-memory store (sessions will not persist)\n")
		s, err := gateway.Open(":memory:")
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
	gateway.WatchConfig(configFile, func(newCfg gateway.Config) {
		ai.SetHTTPTimeout(newCfg.HTTPTimeout)
	})

	return runChat(cfg.Provider, &cfg.Compaction, cfg.SystemPrompt, appendPrompts, resolveProjectContext(cwd(), trust.approve, trust.noApprove, true), pctx, skillsList, cfg.Theme, trustState(cwd(), trust.approve, trust.noApprove), cfg.Notifications)
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
	ag.SetLoopProvider(agent_loop.Loop{})

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