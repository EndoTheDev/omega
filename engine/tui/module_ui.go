package tui

// Module-owned TUI plumbing (card t_88bb7dcf): the shell side of the
// core.TUIProvider seam. The shell owns only the machinery here - the
// input loop routes, the command table breathes with the mounted
// providers, and the console translates writes into transcript
// paragraphs. Feature logic lives in the owning module.

import (
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/EndoTheDev/omega/core"
)

// shellCommands are the shell-owned slash commands - the input loop,
// layout and theme concerns that survive any module removal. Feature
// commands are NOT here: they come from ctx.TUIProviders (module
// seam), ctx.Commands (extension commands) and loaded skills.
//
// Ordered semantically: utility first, conversation control last, the
// app exit at the end. Module commands slot in after shell commands,
// sorted alphabetically so the table order is a law, not a whim.
var shellCommands = []string{"/copy", "/modules", "/extensions", "/panels", "/theme", "/variant", "/fork", "/retry", "/exit", "/help"}

// moduleTickMsg drives a module follow loop: one tick per interval
// while the loop is active.
type moduleTickMsg struct{}

// tuiConsole adapts the shell into the core.TUIConsole the module
// handlers write through. It renders exactly the way the shell's own
// command paths render today.
type tuiConsole struct{ m *model }

var _ core.TUIConsole = (*tuiConsole)(nil)

// Labeled appends the themed "[label]" paragraph + plain body and
// redraws - the shape /cron and /logs print today.
func (c tuiConsole) Labeled(label, body string) {
	c.m.addNotice("\n" + c.m.theme.Info.Render(label) + body)
	c.m.refresh()
}

// Error surfaces a status-line error.
func (c tuiConsole) Error(msg string) { c.m.err = msg }

// ClearError drops the status-line error.
func (c tuiConsole) ClearError() { c.m.err = "" }

// Style resolves a named theme style; unknown kinds resolve to the
// identity render.
func (c tuiConsole) Style(kind string) core.TUIStyle {
	if render, ok := themeStyleFunc(c.m.theme, kind); ok {
		return render
	}
	return func(s string) string { return s }
}

// CWD returns the host working directory (the /files root).
func (c tuiConsole) CWD() string { return c.m.cwd }

// Ext is the mounted plugin Context.
func (c tuiConsole) Ext() *core.Context { return c.m.extensions }

// console returns a fresh shell console for module writes. The model
// is a large value; the adapter pointer keeps handler code identical
// in shape to the receiver methods it replaced.
func (m model) console() core.TUIConsole { return tuiConsole{m: &m} }

// themeStyleFunc maps a console style name onto the shell theme. The
// names mirror the Theme fields modules render with.
func themeStyleFunc(t Theme, kind string) (func(string) string, bool) {
	switch kind {
	case "info":
		return func(s string) string { return t.Info.Render(s) }, true
	case "status":
		return func(s string) string { return t.Status.Render(s) }, true
	case "dim":
		return func(s string) string { return t.Thinking.Render(s) }, true
	case "tool":
		return func(s string) string { return t.Tool.Render(s) }, true
	case "match":
		return func(s string) string { return t.Match.Render(s) }, true
	case "error":
		return func(s string) string { return t.Error.Render(s) }, true
	default:
		return nil, false
	}
}

// moduleCommands builds the per-model dispatch table: every command a
// mounted TUI provider owns, keyed by name. A name clash between
// providers keeps the first registered plugin (buildPlugins order) -
// deterministic, and the conflict surfaces in the /extensions listing.
func moduleCommands(pctx *core.Context) map[string]core.TUIProvider {
	table := map[string]core.TUIProvider{}
	if pctx == nil {
		return table
	}
	for _, p := range pctx.TUIProviders {
		if p == nil {
			continue
		}
		for _, c := range p.TUICommands() {
			if _, dup := table[c.Name]; !dup {
				table[c.Name] = p
			}
		}
	}
	return table
}

// moduleTable walks the mounted TUI providers and returns their
// commands, sorted alphabetically by name (the order is a law: shell
// commands keep theirs, module commands get one that survives re-runs).
func moduleTable(pctx *core.Context) []core.TUICommand {
	var cmds []core.TUICommand
	if pctx == nil {
		return nil
	}
	for _, p := range pctx.TUIProviders {
		if p == nil {
			continue
		}
		cmds = append(cmds, p.TUICommands()...)
	}
	sort.Slice(cmds, func(i, j int) bool { return cmds[i].Name < cmds[j].Name })
	return cmds
}

// moduleOptions maps every command that declares first-argument
// completions, from mounted providers and extension commands alike.
func moduleOptions(pctx *core.Context) map[string][]string {
	opts := map[string][]string{}
	if pctx == nil {
		return opts
	}
	for _, p := range pctx.TUIProviders {
		if p == nil {
			continue
		}
		for _, c := range p.TUICommands() {
			if len(c.Args) > 0 {
				opts[c.Name] = c.Args
			}
		}
	}
	for _, c := range pctx.Commands {
		if len(c.Args) > 0 {
			opts[c.Name] = c.Args
		}
	}
	return opts
}

// handleModuleCommand dispatches to the TUI provider that registered
// the command, then applies the returned shell instructions (screen
// to focus, follow loop to start). Unknown names land on the shell's
// normal unknown-command path. The console writes through the same
// copy this function returns - a provider's Labeled/Error output
// lands on the model the shell keeps.
func (m model) handleModuleCommand(name, args string) (tea.Model, tea.Cmd) {
	p, ok := m.moduleCommands[name]
	if !ok {
		m.err = "unknown command: " + name
		return m, nil
	}
	out := m
	c := tuiConsole{m: &out}
	res := p.HandleTUICommand(name, args, c)
	if res.Overlay != nil {
		// Opening a full-screen view is the terminal shape of changing
		// modes: a running follow loop hands the keys back (the old
		// handleFiles stopped /logs -f the same way).
		out.tick = nil
		out.screen = res.Overlay
	}
	if res.Tick != nil {
		out.tick = res.Tick
	}
	return out, nil
}

// handleModuleTick runs one follow-loop step: ask the provider for new
// lines, append them through the console (the print redraws), and
// reschedule while the provider says keep going. The step writes
// through the same copy this function returns.
func (m model) handleModuleTick() (tea.Model, tea.Cmd) {
	if m.tick == nil {
		return m, nil
	}
	interval := m.tick.Interval
	if interval <= 0 {
		interval = time.Second
	}
	out := m
	c := tuiConsole{m: &out}
	lines, keep := out.tick.Step(c)
	if lines != "" {
		c.Labeled("", lines)
	}
	if !keep {
		out.tick = nil
		return out, nil
	}
	return out, out.tickSchedule(interval)
}

// tickSchedule returns the command for the next follow-loop tick.
func (m model) tickSchedule(interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(time.Time) tea.Msg { return moduleTickMsg{} })
}

// routeModuleScreen hands a message to the open full-screen module
// view and reports whether it consumed the message - the screen owns
// every key while it is open. A screen that reports Closed folds
// back to the transcript.
func (m model) routeModuleScreen(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	if m.screen == nil {
		return m, nil, false
	}
	out := m
	m.screen.Update(msg, tuiConsole{m: &out})
	if out.screen.Closed() {
		out.screen = nil
		out.refresh()
	}
	return out, nil, true
}

// moduleScreenView renders the open module screen full-paragraph: body
// then status line, exactly the /files layout it replaces.
func (m model) moduleScreenView() string {
	var sb strings.Builder
	// The height budget mirrors the old files screen: the shell leaves
	// the status line to itself and passes the raw screen height.
	sb.WriteString(m.screen.View(m.viewport.Width, m.screenHeight))
	sb.WriteString("\n")
	sb.WriteString(m.theme.Status.Render(m.statusLine()))
	return sb.String()
}
