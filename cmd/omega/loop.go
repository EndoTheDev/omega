package main

// The CLI agent loop (card t_0744eb8e): bare `omega` is a plain
// readline-in, ANSI-out conversation against the mounted agent - the
// ollama run mental model. No alt screen, no panes, no mouse, no
// layout: the loop never owns the screen, and anything that needs one
// belongs in the webui (the regression guard is the law). Streaming is
// real (chunks render as they arrive), tool calls print as compact
// text lines, sessions persist on disk so `omega serve` + the webui
// see the same conversation.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"slices"
	"maps"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/verdict"
	"github.com/EndoTheDev/omega/llm"
)

// ANSI basics only - the loop law caps the palette here. NO_COLOR and
// a non-character-device stdout both opt out.
const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiDim    = "\x1b[2m"
	ansiRed    = "\x1b[31m"
	ansiGreen  = "\x1b[32m"
	ansiCyan   = "\x1b[36m"
	ansiYellow = "\x1b[33m"
)

// loopPersona is the loop's fixed setup-support voice (Endo ruling
// 2026-11-01: the terminal face of omega is tech support). It shapes
// HOW the agent talks - patient, terminal-plain, setup-aware - but
// never carries the knowledge itself: what each setting means and how
// providers are configured belongs to the omega-dev skill, which the
// CLI loads via the skill port (t_6c64480f), not a duplicated file.
func loopPersona() string {
	return "You are omega's terminal companion and you act as tech support. " +
		"Many people reach you while setting omega up: walk them through " +
		"providers, the config.yaml keys, and what `omega setup` and " +
		"`omega serve` do, like a patient support rep. " +
		"Reply in short plain-text lines that survive a dumb terminal: " +
		"no markdown tables, no banners, no layout - the terminal scrolls like paper. " +
		"Point at `omega setup`, config.yaml.example, and `omega serve`'s webui when they help."
}

// cliLoop is the interactive loop state.
type cliLoop struct {
	ag    *agent.Agent
	pctx  *agent.Context
	store agent.StoreProvider
	out   *bufio.Writer
	in    *bufio.Reader
	cfg   Config
	trust trustFlags
	cwd   string
	ansi  bool

	sessionID string
	history   []core.Message
	// queued lines typed while a run streamed (stdin was owned by the
	// busy reader; chat waits its turn, verdict answers go inline).
	queued []string
	sig    <-chan os.Signal
}

// cmdLoop is the loop entry (bare `omega` and the legacy `chat` alias).
// A home without config drops into the setup wizard first - the wizard
// is the gate, never an error you must decode. A broken config refuses
// pointing at the same gate.
func cmdLoop(configPath string, appendPrompts []string, trust trustFlags) error {
	cfgPath := resolveConfigPath(configPath)
	if cfgPath == "" {
		fmt.Println("omega: no config yet - running setup first")
		if err := runnableSetup(); err != nil {
			return err
		}
		s := setupSession{in: bufio.NewReader(os.Stdin), out: os.Stdout}
		if err := s.Run(); err != nil {
			return err
		}
		cfgPath = resolveConfigPath(configPath)
		if cfgPath == "" {
			return fmt.Errorf("setup wrote no config - check %s", omegaHome())
		}
	}
	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		return fmt.Errorf("%s: %w - fix the config or run `omega setup`", cfgPath, err)
	}
	if err := resolveHomePaths(&cfg); err != nil {
		return err
	}
	llm.SetHTTPTimeout(cfg.HTTPTimeout)
	if name := resolveAgentName(); name != "" {
		if err := ApplyAgentLayer(&cfg, omegaHome(), name); err != nil {
			return fmt.Errorf("agent layer %s: %w", name, err)
		}
	}
	// The loop's fixed tech-support persona rides the prompt-append
	// seam (see loopPersona) - appended after any --append-system-prompt
	// values so the operator's flags keep speaking last.
	appendPrompts = append(appendPrompts, loopPersona())

	ag, store, logger, pctx, _, _, _, _, _, err := newAgent(cfg, appendPrompts, trust)
	if err != nil {
		return err
	}
	defer store.Close()
	if logger != nil {
		defer logger.Close()
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)

	lo := &cliLoop{
		ag:    ag,
		pctx:  pctx,
		store: store,
		out:   bufio.NewWriter(os.Stdout),
		in:    bufio.NewReader(os.Stdin),
		cfg:   cfg,
		trust: trust,
		cwd:   cwd(),
		ansi:  loopColor(),
		sig:   sig,
	}
	// One-shot mode blocks waiting for late subagent results; the loop
	// is interactive and never blocks, so carry the flag. Nothing
	// consumes the channel today - it is the mode marker.
	ag.SetUserInput(make(chan string, 1))
	lo.banner()
	return lo.run()
}

// loopColor decides the ANSI question once.
func loopColor() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func (lo *cliLoop) banner() {
	lo.printf("%somega%s  %smodel %s%s\n\n",
		lo.s(ansiBold), lo.s(ansiReset), lo.s(ansiDim), lo.ag.ModelName(), lo.s(ansiReset))
	lo.printf("%sthe full app lives at %s - /help for command%s\n\n",
		lo.s(ansiDim), lo.webURL(), lo.s(ansiReset))
}

// webURL builds the webui URL from the resolved config.
func (lo *cliLoop) webURL() string {
	return fmt.Sprintf("http://localhost:%d/", lo.cfg.Server.Port)
}

// run is the input half. One stdin reader goroutine owns stdin for the
// whole session and feeds the `lines` channel; this loop is its only
// receiver - between runs it services the channel itself, mid-run the
// send drain does. A leading slash is a command, `"""` opens a
// multiline block, a trailing backslash continues the line. Output
// happens inside the run's event drain.
func (lo *cliLoop) run() error {
	sctx, scancel := context.WithCancel(context.Background())
	defer scancel()
	lines := lo.startStdinReader(sctx)
	for {
		lo.prompt()
		var line string
		select {
		case <-lo.sig:
			lo.printf("%s(Ctrl-C: exiting)%s\n", lo.s(ansiDim), lo.s(ansiReset))
			lo.printExitLine()
			return nil
		case l, ok := <-lines:
			if !ok {
				lo.printf("\n")
				lo.printExitLine()
				return nil
			}
			line = l
		}
		if line == "" {
			continue
		}
		for strings.HasSuffix(line, "\\") && !strings.HasSuffix(line, "\\\\") {
			nl, ok := <-lines
			if !ok {
				line = line[:len(line)-1]
				break
			}
			line = line[:len(line)-1] + " " + nl
		}
		if line == `"""` {
			line = lo.readMultiline(lines)
			if strings.TrimSpace(line) == "" {
				continue
			}
		}
		if strings.HasPrefix(line, "/") {
			stop := lo.dispatch(line)
			lo.out.Flush()
			if stop {
				lo.printExitLine()
				return nil
			}
			continue
		}
		if err := lo.send(line, lines); err != nil {
			lo.printf("%serror: %v%s\n", lo.s(ansiRed), err, lo.s(ansiReset))
			lo.out.Flush()
		}
		// lines typed (or queued) while the run streamed go now
		for len(lo.queued) > 0 {
			q := lo.queued[0]
			lo.queued = lo.queued[1:]
			lo.printf("%s%s%s\n", lo.s(ansiDim), q, lo.s(ansiReset))
			if strings.HasPrefix(q, "/") {
				if stop := lo.dispatch(q); stop {
					lo.printExitLine()
					return nil
				}
				continue
			}
			if err := lo.send(q, lines); err != nil {
				lo.printf("%serror: %v%s\n", lo.s(ansiRed), err, lo.s(ansiReset))
			}
		}
	}
}

// readMultiline collects a `"""` block until the closing line arrives
// on the same channel. EOF submits what the block holds.
func (lo *cliLoop) readMultiline(lines <-chan string) string {
	lo.printf("%s(multiline: finish with \"\"\" on its own line)%s\n", lo.s(ansiDim), lo.s(ansiReset))
	var block []string
	for {
		select {
		case <-lo.sig:
			return strings.Join(block, "\n")
		default:
		}
		l, ok := <-lines
		if !ok {
			break
		}
		if l == `"""` {
			break
		}
		block = append(block, l)
	}
	return strings.Join(block, "\n")
}

func (lo *cliLoop) prompt() {
	lo.printf("%s>%s ", lo.s(ansiBold), lo.s(ansiReset))
	lo.out.Flush()
}

func (lo *cliLoop) s(code string) string {
	if !lo.ansi {
		return ""
	}
	return code
}

func (lo *cliLoop) printf(format string, args ...any) {
	fmt.Fprintf(lo.out, format, args...)
}

// printExitLine is the motivated-into-webui door: the session rode on,
// and the browser hosts the whole app. No bullying - the door points.
func (lo *cliLoop) printExitLine() {
	if lo.sessionID != "" {
		lo.printf("session saved: %s\n", lo.sessionID)
	}
	lo.printf("full app: %s\n", lo.webURL())
	lo.out.Flush()
}

// dispatch runs one slash command. Returns stop=true for the exit set.
func (lo *cliLoop) dispatch(line string) bool {
	name, args := splitCommand(line)
	switch name {
	case "/exit", "/quit", "/q", "/bye":
		return true
	case "/help", "/?":
		lo.printHelp()
		return false
	case "/session":
		if err := lo.commandSession(args); err != nil {
			lo.printf("%s%s%s\n", lo.s(ansiRed), err.Error(), lo.s(ansiReset))
		}
		return false
	case "/workspace":
		if err := lo.commandWorkspace(args); err != nil {
			lo.printf("%s%s%s\n", lo.s(ansiRed), err.Error(), lo.s(ansiReset))
		}
		return false
	default:
		if err := lo.commandExtension(name, args); err != nil {
			lo.printf("%s%s%s\n", lo.s(ansiRed), err.Error(), lo.s(ansiReset))
		}
		return false
	}
}

func splitCommand(line string) (string, string) {
	fields := strings.SplitN(strings.TrimRight(line, " \t"), " ", 2)
	name := fields[0]
	rest := ""
	if len(fields) == 2 {
		rest = strings.TrimSpace(fields[1])
	}
	return name, rest
}

func (lo *cliLoop) printHelp() {
	lo.printf("commands:\n")
	lo.printf("  /exit      leave (also /quit, /q)\n")
	lo.printf("  /help      this list\n")
	lo.printf("  /session [id|new]   show, switch to, or start a session on disk\n")
	lo.printf("  /workspace [dir]    show, or change the working directory\n")
	lo.printf("  \"\"\"        multiline input block (closing \"\"\" submits)\n")
	for _, c := range lo.pctx.Commands {
		lo.printf("  %-9s %s\n", c.Name, c.Description)
	}
	lo.out.Flush()
}

// commandSession shows or switches the persisted session. `new`
// starts a fresh one (created on the next send); an id loads its
// messages into this conversation.
func (lo *cliLoop) commandSession(args string) error {
	if args == "" {
		if lo.sessionID == "" {
			lo.printf("no session yet - one is created on the first message\n")
			return nil
		}
		lo.printf("session: %s\n", lo.sessionID)
		return nil
	}
	if args == "new" {
		lo.sessionID = ""
		lo.history = nil
		lo.printf("fresh session - created on the next message\n")
		return nil
	}
	msgs, err := lo.store.GetMessages(context.Background(), args)
	if err != nil {
		return fmt.Errorf("session %s: %w", args, err)
	}
	lo.sessionID = args
	lo.history = msgs
	lo.printf("session %s: %d messages loaded\n", args, len(msgs))
	return nil
}

// commandWorkspace shows or changes the loop's working directory (the
// per-run CWD the tools see). No process chdir - the agent's CWD and
// the project context re-resolve through the trust seam, so an
// untrusted project loads no AGENTS.md, the same gate startup runs.
func (lo *cliLoop) commandWorkspace(args string) error {
	if args == "" {
		lo.printf("workspace: %s\n", lo.cwd)
		return nil
	}
	if fi, err := os.Stat(args); err != nil || !fi.IsDir() {
		return fmt.Errorf("%s is not a directory", args)
	}
	abs, err := filepath.Abs(args)
	if err != nil {
		return err
	}
	lo.cwd = abs
	lo.ag.SetCWD(abs)
	lo.ag.SetPromptContext(trustGate(lo.pctx, abs, lo.trust, false))
	lo.printf("workspace: %s\n", lo.cwd)
	return nil
}

// commandExtension runs a module-owned command (provider, verdict,
// cron, logs, ...) and prints its text result. An unclaimed name is an
// error listing what exists.
func (lo *cliLoop) commandExtension(name, args string) error {
	known := false
	for _, c := range lo.pctx.Commands {
		if c.Name == name {
			known = true
			break
		}
	}
	if !known {
		return fmt.Errorf("%s is not a command - /help lists them", name)
	}
	res, err := lo.pctx.CommandHandler(context.Background(), name, args)
	if err != nil {
		return err
	}
	if res.Text != "" {
		lo.printf("%s\n", res.Text)
	}
	lo.out.Flush()
	return nil
}

// ensureSession creates the on-disk session before the first send.
func (lo *cliLoop) ensureSession() error {
	if lo.sessionID != "" {
		return nil
	}
	id, err := agent.NewSessionID()
	if err != nil {
		return err
	}
	if err := lo.store.CreateSession(context.Background(), id, "", ""); err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	lo.sessionID = id
	return nil
}

// send runs one user message through the loop, streaming the reply.
// While the run streams, lines typed mid-run queue for replay (or
// answer a pending verdict card) - the session reader is the feed.
func (lo *cliLoop) send(text string, lines <-chan string) error {
	if err := lo.ensureSession(); err != nil {
		return err
	}
	userMsg := core.NewUser(text)
	lo.history = append(lo.history, userMsg)

	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runCtx = agent.WithSessionID(runCtx, lo.sessionID)
	// interactive origin: the verdict tool may pause here, and the
	// busy reader answers it while the run blocks - the loop's one
	// mid-run interaction, matching the old busy-Enter path.
	runCtx = core.WithRunOrigin(runCtx, "interactive")
	runCtx = agent.WithTurnSink(runCtx, agent.NewStoreTurnSink(lo.store, lo.sessionID, ""))
	runCtx = agent.WithPersistInput(runCtx, []core.Message{userMsg})

	render := &loopRenderer{lo: lo}
	events := lo.ag.Run(runCtx, lo.history, nil)

	drain := true
	for drain {
		select {
		case ev, ok := <-events:
			if !ok {
				drain = false
				break
			}
			render.event(ev)
		case line, ok := <-lines:
			if !ok {
				lines = nil
				break
			}
			if !lo.answerPendingVerdict(line) {
				lo.queued = append(lo.queued, line)
				lo.printf("%s(queued: %s)%s\n", lo.s(ansiDim), line, lo.s(ansiReset))
			}
		case <-lo.sig:
			cancel()
			lo.printf("%s(cancelled)%s\n", lo.s(ansiDim), lo.s(ansiReset))
		}
	}
	cancel()
	// non-blocking drain of the reader's buffered leftovers: a verdict
	// card can outlive its blocking call, chat waits in the queue.
	for {
		select {
		case l, ok := <-lines:
			if !ok {
				lines = nil
				break
			}
			if !lo.answerPendingVerdict(l) {
				lo.queued = append(lo.queued, l)
			}
			continue
		default:
		}
		break
	}
	lo.out.Flush()

	// the sink persisted the turn; the disk is the truth
	msgs, err := lo.store.GetMessages(context.Background(), lo.sessionID)
	if err == nil {
		lo.history = msgs
	}
	return nil
}

// startStdinReader owns stdin for the whole session - exactly one
// reader goroutine, so no second goroutine ever races the bufio. Lines
// feed the channel; EOF closes it and the loop exits with the door.
func (lo *cliLoop) startStdinReader(ctx context.Context) chan string {
	lines := make(chan string, 16)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			line, rerr := lo.in.ReadString('\n')
			if rerr != nil {
				close(lines)
				return
			}
			line = strings.TrimRight(line, "\n")
			if line == "" {
				continue
			}
			select {
			case <-ctx.Done():
				return
			case lines <- line:
			}
		}
	}()
	return lines
}

// verdictSink is the CLI loop's own need face of the verdicts capsule
// (part 3, the seam constitution): the capsule's answerSink type is the
// capsule's business - the loop names what IT calls and resolves the
// service by key from the universal registry. The key is data; the
// owner capsule never knows a loop exists.
type verdictSink interface {
	Pending(sessionID string) []core.QuestionPrompt
	Answer(sessionID, cardID string, v core.QuestionAnswer) (core.QuestionAnswer, error)
}

// answerPendingVerdict hands one busy-time line to the verdict module's
// inline answering. The named return is load-bearing for the purity
// law (scripts/nm-purity.sh): the scrubber deletes the module wire as
// one line and never deletes a host function, so the loop must still
// compile and answer false with the module gone - the plain `return
// verdict.AnswerInline(...)` shape strands a missing return.
func (lo *cliLoop) answerPendingVerdict(line string) (handled bool) {
	if lo.pctx == nil {
		return false
	}
	vp, ok := lo.pctx.Services().Resolve("verdicts").(verdictSink)
	if !ok {
		return false
	}
	handled = verdict.AnswerInline(vp, lo.sessionID, line, lo.printf)
	return handled
}

// loopRenderer turns the run's event stream into plain ANSI text:
// thinking dim, reply plain, tool calls compact cyan lines - no
// redraws, no cursor games, the terminal scrolls like paper.
type loopRenderer struct {
	lo        *cliLoop
	inText    bool
	inThink   bool
	toolNames map[string]string // tool_call_id -> tool name
}

func (r *loopRenderer) open(kind string) {
	if kind == "think" && !r.inThink {
		r.lo.printf("%s", r.lo.s(ansiDim))
		r.inThink = true
		r.inText = false
	}
}

func (r *loopRenderer) closeBlock() {
	if r.inThink {
		r.lo.printf("%s\n", r.lo.s(ansiReset))
		r.inThink = false
	}
	if r.inText {
		r.lo.printf("\n")
		r.inText = false
	}
}

func (r *loopRenderer) event(ev core.Event) {
	switch e := ev.(type) {
	case core.AgentStart:
		// quiet: the streaming output is the news
	case core.StreamEvent:
		switch chunk := e.Event.(type) {
		case core.ThinkingChunk:
			r.open("think")
			r.lo.printf("%s", chunk.Content)
		case core.ResponseChunk:
			if r.inThink {
				r.lo.printf("%s\n", r.lo.s(ansiReset))
				r.inThink = false
			}
			r.inText = true
			r.lo.printf("%s", chunk.Content)
		case core.StreamEnd:
			if chunk.Error != "" {
				r.closeBlock()
				r.lo.printf("%s%s%s\n", r.lo.s(ansiRed), chunk.Error, r.lo.s(ansiReset))
			}
		case core.ToolCallEvent:
			r.closeBlock()
			tc := chunk.ToolCall
			if r.toolNames == nil {
				r.toolNames = map[string]string{}
			}
			r.toolNames[tc.ID] = tc.Name
			r.lo.printf("%s> %s%s%s %s%s%s\n",
				r.lo.s(ansiCyan), r.lo.s(ansiCyan), tc.Name, r.lo.s(ansiReset),
				r.lo.s(ansiDim), compactToolArgs(tc.Arguments), r.lo.s(ansiReset))
		}
	case core.ToolResultEvent:
		tail := "ok"
		color := r.lo.s(ansiGreen)
		if e.Message.IsError {
			tail = "error"
			color = r.lo.s(ansiRed)
		}
		name := r.toolNames[e.Message.ToolCallID]
		r.lo.printf("%s< %s%s%s (%d chars, %s)%s\n",
			r.lo.s(ansiDim), r.lo.s(ansiDim), name, r.lo.s(ansiReset),
			len(e.Message.Content), color+tail+r.lo.s(ansiReset)+r.lo.s(ansiDim), r.lo.s(ansiReset))
	case core.AgentEnd:
		r.closeBlock()
		if e.Error != "" {
			r.lo.printf("%serror: %s%s\n", r.lo.s(ansiRed), e.Error, r.lo.s(ansiReset))
		}
	}
	r.lo.out.Flush()
}

// compactToolArgs renders tool arguments as one short line: keys and
// short values only, so a tool call stays one readable line no matter
// how fat its payload.
func compactToolArgs(args map[string]any) string {
	if len(args) == 0 {
		return ""
	}
	var b strings.Builder
	first := true
	for _, k := range sortedKeys(args) {
		v := args[k]
		vs := ""
		switch t := v.(type) {
		case string:
			vs = t
		default:
			j, _ := json.Marshal(t)
			vs = string(j)
		}
		vs = strings.ReplaceAll(vs, "\n", " ")
		if len(vs) > 60 {
			vs = vs[:57] + "..."
		}
		if !first {
			b.WriteString(" ")
		}
		first = false
		if vs == "" {
			b.WriteString(k)
		} else {
			fmt.Fprintf(&b, "%s=%s", k, vs)
		}
	}
	return b.String()
}

func sortedKeys(m map[string]any) []string {
	return slices.Sorted(maps.Keys(m))
}
