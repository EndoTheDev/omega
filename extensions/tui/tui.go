package tui

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/quick"
	chromastyles "github.com/alecthomas/chroma/v2/styles"
	"github.com/atotto/clipboard"
	"github.com/gen2brain/beeep"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/ai"
	"github.com/EndoTheDev/omega/extensions/store"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	glamouransi "github.com/charmbracelet/glamour/ansi"
	glamourstyles "github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
)

// Fixed layout: textarea starts at minTextareaHeight lines and grows up to
// maxTextareaHeight as the user types. The viewport fills the rest minus
// the status line and the autocomplete panel (when open).
const (
	minTextareaHeight = 1
	maxTextareaHeight = 8
	statusLines       = 1 // status bar only; autocomplete panel is dynamic

	// maxAutocompleteRows caps the dropup panel so a loaded skill
	// directory cannot eat the whole viewport. ponytail: fixed cap;
	// upgrade path: config knob.
	maxAutocompleteRows = 8

	// toolResultAutoThreshold is the line count at which /tools auto
	// collapses a result. ponytail: fixed constant; upgrade path: config
	// knob.
	toolResultAutoThreshold = 20
)

// Theme holds the styles used throughout the TUI. Built-in themes
// are defined below; users select via config (theme key) or the
// /theme command at runtime.
type Theme struct {
	Name       string
	User       lipgloss.Style
	Thinking   lipgloss.Style
	Tool       lipgloss.Style
	Info       lipgloss.Style
	Status     lipgloss.Style
	Match      lipgloss.Style
	Error      lipgloss.Style
	CodeBorder lipgloss.Style
}

// built-in themes.
var themes = map[string]Theme{
	"dark": {
		Name:       "dark",
		User:       lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#89b4fa")),
		Thinking:   lipgloss.NewStyle().Foreground(lipgloss.Color("#6c7086")),
		Tool:       lipgloss.NewStyle().Foreground(lipgloss.Color("#fab387")),
		Info:       lipgloss.NewStyle().Foreground(lipgloss.Color("#9399b2")),
		Status:     lipgloss.NewStyle().Foreground(lipgloss.Color("#6c7086")),
		Match:      lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#cdd6f4")),
		Error:      lipgloss.NewStyle().Foreground(lipgloss.Color("#f38ba8")),
		CodeBorder: lipgloss.NewStyle().Foreground(lipgloss.Color("#45475a")),
	},
	"light": {
		Name:       "light",
		User:       lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#1e66f5")),
		Thinking:   lipgloss.NewStyle().Foreground(lipgloss.Color("#8c8fa1")),
		Tool:       lipgloss.NewStyle().Foreground(lipgloss.Color("#fe640b")),
		Info:       lipgloss.NewStyle().Foreground(lipgloss.Color("#7c7f93")),
		Status:     lipgloss.NewStyle().Foreground(lipgloss.Color("#8c8fa1")),
		Match:      lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#4c4f69")),
		Error:      lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#d20f39")),
		CodeBorder: lipgloss.NewStyle().Foreground(lipgloss.Color("#bcc0cc")),
	},
}

// init registers Catppuccin chroma styles for syntax highlighting.
func init() {
	registerCatppuccinChroma("catppuccin-mocha", chroma.StyleEntries{
		chroma.Text:                "#a6adc8", // subtext0
		chroma.Comment:             "#6c7086", // overlay0
		chroma.CommentPreproc:      "#cba6f7", // mauve
		chroma.Keyword:             "#cba6f7", // mauve
		chroma.KeywordType:         "#eba0ac", // maroon
		chroma.KeywordNamespace:    "#b4befe", // lavender
		chroma.KeywordReserved:     "#cba6f7", // mauve
		chroma.Name:                "#bac2de", // subtext1
		chroma.NameBuiltin:         "#f38ba8", // red
		chroma.NameFunction:        "#89b4fa", // blue
		chroma.NameClass:           "#f9e2af", // yellow
		chroma.NameDecorator:       "#fab387", // peach
		chroma.NameTag:             "#f38ba8", // red
		chroma.NameAttribute:       "#89b4fa", // blue
		chroma.NameConstant:        "#fab387", // peach
		chroma.LiteralString:       "#a6e3a1", // green
		chroma.LiteralStringEscape: "#94e2d5", // teal
		chroma.LiteralNumber:       "#fab387", // peach
		chroma.Operator:            "#9399b2", // overlay2
		chroma.Punctuation:         "#9399b2", // overlay2
		chroma.GenericInserted:     "#a6e3a1", // green
		chroma.GenericDeleted:      "#f38ba8", // red
		chroma.GenericHeading:      "#89b4fa", // blue
		chroma.GenericSubheading:   "#89b4fa", // blue
		chroma.Error:               "#f38ba8",
		chroma.Background:          "bg:#1e1e2e",
	})
	registerCatppuccinChroma("catppuccin-latte", chroma.StyleEntries{
		chroma.Text:                "#6c6f85", // subtext0
		chroma.Comment:             "#8c8fa1", // overlay1
		chroma.CommentPreproc:      "#8839ef", // mauve
		chroma.Keyword:             "#8839ef", // mauve
		chroma.KeywordType:         "#e64553", // maroon
		chroma.KeywordNamespace:    "#7287fd", // lavender
		chroma.KeywordReserved:     "#8839ef", // mauve
		chroma.Name:                "#5c5f77", // subtext1
		chroma.NameBuiltin:         "#d20f39", // red
		chroma.NameFunction:        "#1e66f5", // blue
		chroma.NameClass:           "#df8e1d", // yellow
		chroma.NameDecorator:       "#fe640b", // peach
		chroma.NameTag:             "#d20f39", // red
		chroma.NameAttribute:       "#1e66f5", // blue
		chroma.NameConstant:        "#fe640b", // peach
		chroma.LiteralString:       "#40a02b", // green
		chroma.LiteralStringEscape: "#179299", // teal
		chroma.LiteralNumber:       "#fe640b", // peach
		chroma.Operator:            "#7c7f93", // overlay2
		chroma.Punctuation:         "#7c7f93", // overlay2
		chroma.GenericInserted:     "#40a02b", // green
		chroma.GenericDeleted:      "#d20f39", // red
		chroma.GenericHeading:      "#1e66f5", // blue
		chroma.GenericSubheading:   "#1e66f5", // blue
		chroma.Error:               "#d20f39",
		chroma.Background:          "bg:#eff1f5",
	})
}

// registerCatppuccinChroma registers a chroma style if not already
// registered. Safe to call multiple times (init runs once).
func registerCatppuccinChroma(name string, entries chroma.StyleEntries) {
	if _, ok := chromastyles.Registry[name]; ok {
		return
	}
	chromastyles.Register(chroma.MustNewStyle(name, entries))
}

// glamourStyleForTheme returns a glamour StyleConfig using Catppuccin
// colors for the given theme name ("dark" -> Mocha, "light" -> Latte).
func glamourStyleForTheme(themeName string) glamouransi.StyleConfig {
	base := glamourstyles.DarkStyleConfig
	if themeName == "light" {
		base = glamourstyles.LightStyleConfig
	}

	// Override key colors with Catppuccin palette.
	if themeName == "light" {
		base.Document.Color = stringPtr("#4c4f69")       // text
		base.Heading.Color = stringPtr("#1e66f5")        // blue
		base.H1.Color = stringPtr("#4c4f69")             // text
		base.H1.BackgroundColor = stringPtr("#ccd0da")   // surface0
		base.H2.Color = stringPtr("#1e66f5")             // blue
		base.H3.Color = stringPtr("#8839ef")             // mauve
		base.Code.Color = stringPtr("#ea76cb")           // pink
		base.Code.BackgroundColor = stringPtr("#ccd0da") // surface0
		base.Link.Color = stringPtr("#209fb5")           // sapphire
		base.LinkText.Color = stringPtr("#1e66f5")       // blue
		base.BlockQuote.Color = stringPtr("#8c8fa1")     // overlay1
		base.Table.Color = stringPtr("#7c7f93")          // overlay2
		base.HorizontalRule.Color = stringPtr("#bcc0cc") // surface1
	} else {
		base.Document.Color = stringPtr("#cdd6f4")       // text
		base.Heading.Color = stringPtr("#89b4fa")        // blue
		base.H1.Color = stringPtr("#cdd6f4")             // text
		base.H1.BackgroundColor = stringPtr("#313244")   // surface0
		base.H2.Color = stringPtr("#89b4fa")             // blue
		base.H3.Color = stringPtr("#cba6f7")             // mauve
		base.Code.Color = stringPtr("#f5c2e7")           // pink
		base.Code.BackgroundColor = stringPtr("#313244") // surface0
		base.Link.Color = stringPtr("#74c7ec")           // sapphire
		base.LinkText.Color = stringPtr("#89b4fa")       // blue
		base.BlockQuote.Color = stringPtr("#6c7086")     // overlay0
		base.Table.Color = stringPtr("#9399b2")          // overlay2
		base.HorizontalRule.Color = stringPtr("#585b70") // surface2
	}
	return base
}

// stringPtr returns a pointer to s. Used for glamour StyleConfig fields.
func stringPtr(s string) *string { return &s }
func themeNames() []string {
	names := make([]string, 0, len(themes))
	for n := range themes {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// knownCommands are the built-in slash commands, ordered semantically:
// session lifecycle, then model control, then transcript tools, then
// app commands. Skill names are appended at startup, so autocomplete
// matches both built-ins and loaded skills.
var knownCommands = []string{"/copy", "/extensions", "/theme", "/exit", "/help"}

// commandOptions maps commands with enum arguments to their valid values.
// The autocomplete offers these as second-level completions once the
// input equals the command (or the command plus a partial option).
// Commands with free-form or dynamic arguments are not listed here.
var commandOptions = map[string][]string{
	"/new":      {"--ephemeral"},
	"/thinking": {"none", "off", "on", "minimal", "low", "medium", "high", "extra high", "max", "ultra"},
	"/tools":    {"on", "off", "auto", "list"},
	"/theme":    {"dark", "light", "auto"},
	"/sessions": {"delete"},
}

// streamSegment is one ordered piece of a streaming turn. Segments are
// appended in the order the model emits them, preserving the narrative
// flow (thinking → tool → response → more thinking → ...).
type streamSegment struct {
	kind    string // "thinking", "tool", "response"
	content string
}

// model is the Bubble Tea state for the chat TUI. It owns the message
// history, the streaming buffer, and the two widgets (viewport + textarea).
type model struct {
	textarea             textarea.Model
	viewport             viewport.Model
	history              []ai.Message    // full conversation fed to the agent each turn
	transcript           string          // rendered content of completed exchanges
	segments             []streamSegment // ordered streaming segments for the current turn
	providerType         string
	modelName            string
	compaction           *agent.CompactionConfig
	promptCustom         string
	promptAppend         []string
	promptContext        string
	busy                 bool               // a run is in flight; input is ignored
	compacting           bool               // /compact is in flight; non-blocking
	err                  string             // last run error, shown in the status line
	cancel               context.CancelFunc // cancels the in-flight run; nil when idle
	events               <-chan agent.Event // run goroutine writes here; Update drains via cmd
	store                agent.StoreProvider
	sessionID            string                 // current session; "" until the first message creates one
	storeErr             string                 // store open/persistence error, shown in the status line
	promptHistory        []string               // previously submitted prompts, for Up/Down recall
	historyIndex         int                    // position in promptHistory; 0 = empty/current input
	autocompleteMatches  []string               // slash commands matching the current input
	autocompleteIndex    int                    // highlighted match; -1 = none selected
	autocompleteOffset   int                    // first visible row in the dropup window
	autocompleteSlashPos int                    // byte offset of the / triggering autocomplete, -1 = none
	screenHeight         int                    // terminal height from the last resize
	skills               []agent.Skill          // loaded skills, for autocomplete and invocation
	extensions           *agent.Context       // plugin context (provider, compactor, commands, etc.)
	commands             []string               // knownCommands + skill names, per-model copy
	showThinking         bool                   // /thinking display toggle; auto-set from thinkingLevel
	thinkingLevel        string                 // /thinking level: none, off, on, minimal, low, medium, high, extra high, max, ultra
	showToolResults      bool                   // /tools toggle; default false (collapsed)
	toolResultsAuto      bool                   // /tools auto; short results full, long ones collapsed
	queuedInput          string                 // follow-up typed while agent runs; auto-submits on done
	userInput            chan string            // mode flag for agent loop (nil = one-shot, non-nil = TUI)
	autoNamed            bool                   // true after the first auto-name attempt
	sessionLabel         string                 // model-generated title, shown in status bar
	autoNameGen          int                    // bumped on /new; stale auto-name results are dropped
	sessionList          []agent.Session        // cached from last /sessions, for /resume by #
	modelList            []string               // cached from last /models, for /model <#> selection
	ephemeral            bool                   // /new --ephemeral; nothing persisted
	theme                Theme                  // active color/style theme
	trustState           string                 // "trusted" / "untrusted" / "" (no AGENTS.md), shown in status bar
	notifications        string                 // "bell" / "desktop" / "off", fired on turn complete
	version              string                 // build version for splash display
	cwd                  string                 // working directory passed from host
	lastToolCall         string                 // last tool call name, for syntax highlighting results
	lastToolArgs         map[string]any         // last tool call args, for language detection
	lastRender           time.Time              // debounce for live glamour rendering during streaming
	lastRenderedResponse string                 // cached glamour output for debounced frames
	contextWindow        int                    // auto-discovered from provider; 0 = unknown, fall back to config
}

// streamDoneMsg signals that the run goroutine has finished.
type streamDoneMsg struct{}

// compactionResultMsg carries the result of an async /compact call.
type compactionResultMsg struct {
	messages []ai.Message
	err      error
	before   int
}

// modelsLoadedMsg carries models fetched in the background for Ctrl+P
// when modelList is empty.
type modelsLoadedMsg struct {
	models []string
	err    error
}

// modelInfoLoadedMsg carries auto-discovered model metadata (e.g.
// context window) fetched from the provider extension.
type modelInfoLoadedMsg struct {
	contextWindow int
	err           error
}

// fetchModelsCmd returns a tea.Cmd that fetches available models from
// the provider. Used by Ctrl+P when modelList is empty.
func (m model) fetchModelsCmd() tea.Cmd {
	return func() tea.Msg {
		if m.extensions == nil || m.extensions.Provider == nil {
			return modelsLoadedMsg{err: fmt.Errorf("no provider configured")}
		}
		models, err := m.extensions.Provider.ListModels()
		return modelsLoadedMsg{models: models, err: err}
	}
}

// fetchModelInfoCmd returns a tea.Cmd that queries the provider for
// metadata about the current model (context window). Non-blocking;
// on error or unknown, contextWindow stays 0 and the status bar
// falls back to compaction config.
func (m model) fetchModelInfoCmd() tea.Cmd {
	return func() tea.Msg {
		if m.extensions == nil || m.extensions.Provider == nil {
			return modelInfoLoadedMsg{err: fmt.Errorf("no provider configured")}
		}
		info, err := m.extensions.Provider.ModelInfo()
		return modelInfoLoadedMsg{contextWindow: info.ContextWindow, err: err}
	}
}

// autoNameMsg carries the result of a background auto-name call.
type autoNameMsg struct {
	sessionID string // session the title was generated for
	gen       int    // autoNameGen at spawn; stale results are dropped
	label     string
	err       error
}

// NewModel creates the TUI model from a mounted Context and frontend
// options. Called by the Frontend.Run method.
func NewModel(pctx *agent.Context, opts agent.FrontendOptions) model {
	return newChatModel(opts.ProviderType, opts.ModelName, opts.Compaction, opts.PromptCustom, opts.PromptAppend, opts.PromptContext, pctx, opts.Skills, opts.ThemeName, opts.TrustState, opts.Notifications, opts.Version, opts.CWD)
}

// storeFromContext returns the StoreProvider from a Context, or nil
// if the Context is nil.
func storeFromContext(pctx *agent.Context) agent.StoreProvider {
	if pctx == nil {
		return nil
	}
	return pctx.Store
}

func newChatModel(providerType, modelName string, compaction *agent.CompactionConfig, promptCustom string, promptAppend []string, promptContext string, pctx *agent.Context, skills []agent.Skill, themeName, trustState, notifications, version, cwd string) model {
	ta := textarea.New()
	ta.Placeholder = "message (enter to send, ctrl+j for newline, /help for commands)"
	ta.SetHeight(minTextareaHeight)
	ta.ShowLineNumbers = false
	vp := viewport.New(80, 20)
	// Build the per-model command list: clone the built-in commands,
	// append extension commands, then skill names. This avoids mutating
	// the package-level slice.
	commands := make([]string, len(knownCommands))
	copy(commands, knownCommands)
	if pctx != nil {
		for _, c := range pctx.Commands {
			commands = append(commands, c.Name)
		}
	}
	for _, s := range skills {
		commands = append(commands, "/"+s.Name)
	}
	// Resolve the theme; fall back to dark. "auto" detects the OS
	// appearance and copies the resolved theme's styles.
	t, ok := themes[themeName]
	if !ok || themeName == "auto" {
		resolved := themeName
		if themeName == "auto" {
			resolved = detectSystemTheme()
		}
		t, ok = themes[resolved]
		if !ok {
			t = themes["dark"]
		}
		if themeName == "auto" {
			t.Name = "auto"
		}
	}
	return model{
		textarea:             ta,
		viewport:             vp,
		providerType:         providerType,
		modelName:            modelName,
		compaction:           compaction,
		promptCustom:         promptCustom,
		promptAppend:         promptAppend,
		promptContext:        promptContext,
		store:                storeFromContext(pctx),
		skills:               skills,
		extensions:           pctx,
		commands:             commands,
		autocompleteIndex:    -1,
		autocompleteSlashPos: -1,
		showThinking:         true,
		thinkingLevel:        "medium",
		showToolResults:      true,
		toolResultsAuto:      true,
		theme:                t,
		trustState:           trustState,
		notifications:        notifications,
		version:              version,
		cwd:                  cwd,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.textarea.Focus(), tea.EnterAltScreen, m.titleCmd(), m.fetchModelInfoCmd())
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.textarea.SetWidth(msg.Width)
		m.screenHeight = msg.Height
		m.viewport.Width = msg.Width
		m.resizeTextarea()
		if len(m.history) > 0 && !m.busy {
			m.transcript = renderTranscript(m.history, m.viewport.Width, m.theme)
		}
		m.refresh()
		return m, m.textarea.Focus()

	case tea.KeyMsg:
		// Ctrl+C always exits, even mid-run.
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if m.busy {
			// Escape cancels the in-flight run; the agent loop observes
			// ctx.Err() and emits AgentEnd("cancelled"), which clears busy.
			if msg.String() == "esc" && m.cancel != nil {
				m.cancel()
				m.queuedInput = ""
			}
			// Enter queues the current input to auto-submit when the
			// agent finishes. Typing is still allowed while busy.
			if msg.String() == "enter" {
				queue := strings.TrimSpace(m.textarea.Value())
				if queue == "" {
					return m, nil
				}
				m.queuedInput = queue
				m.textarea.SetValue("")
				m.textarea.Placeholder = "message (queued: " + truncate(m.queuedInput, 40) + ")"
				return m, nil
			}
			// Allow typing while busy; just don't submit.
			var cmd tea.Cmd
			m.textarea, cmd = m.textarea.Update(msg)
			m.updateAutocomplete()
			return m, cmd
		}
		if msg.String() == "ctrl+j" {
			// Ctrl+J inserts a newline for multi-line input.
			var cmd tea.Cmd
			m.textarea, cmd = m.textarea.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'\n'}})
			m.updateAutocomplete()
			m.resizeTextarea()
			return m, cmd
		}
		if msg.String() == "ctrl+p" {
			// Ctrl+P cycles to the next model in modelList. If the list
			// is empty, fetch it from the provider first.
			if len(m.modelList) == 0 {
				m.transcript += "\n" + m.theme.Info.Render("[fetching models...]") + "\n"
				m.refresh()
				return m, m.fetchModelsCmd()
			}
			// Find current model index, advance to the next.
			idx := -1
			for i, name := range m.modelList {
				if name == m.modelName {
					idx = i
					break
				}
			}
			next := idx + 1
			if next >= len(m.modelList) {
				next = 0
			}
			m.modelName = m.modelList[next]
			m.transcript += "\n" + m.theme.Info.Render("[model: "+m.modelName+"]") + "\n"
			m.refresh()
			if m.extensions != nil && m.extensions.Provider != nil {
		m.extensions.Provider.SetModel(m.modelName)
	}
			return m, tea.Batch(m.titleCmd(), m.fetchModelInfoCmd())
		}
		if msg.Paste {
			// Bracketed paste (file drop, large paste): insert the
			// pasted text directly, bypassing autocomplete. The textarea
			// may not handle Paste=true KeyMsgs correctly, so we insert
			// the runes as a regular KeyRunes message.
			var cmd tea.Cmd
			m.textarea, cmd = m.textarea.Update(tea.KeyMsg{
				Type:  tea.KeyRunes,
				Runes: msg.Runes,
			})
			m.resizeTextarea()
			return m, cmd
		}
		if msg.String() == "enter" { // Enter accepts the selected match or submits
			if m.autocompleteIndex >= 0 && m.autocompleteIndex < len(m.autocompleteMatches) {
				if cmd := m.acceptMatch(); cmd != nil {
					return m, cmd
				}
			}
			return m.submit()
		}
		if msg.String() == "esc" {
			m.err = ""
			m.autocompleteMatches = nil
			m.autocompleteIndex = -1
			m.autocompleteOffset = 0
			m.resizeTextarea() // panel closed; give the rows back to the viewport
			m.refresh()
			return m, nil
		}
		// Tab completes the selected match (or accepts a single match).
		if msg.String() == "tab" {
			return m.handleTabComplete()
		}
		// PgUp/PgDn/Up/Down scroll the viewport.
		if msg.String() == "pgup" || msg.String() == "pgdown" {
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		}
		// Up/Down cycle autocomplete matches when active; otherwise recall
		// prompt history. From none selected (-1), the first press picks a
		// starting match; subsequent presses wrap.
		if msg.String() == "up" && len(m.autocompleteMatches) > 0 {
			if m.autocompleteIndex < 0 {
				m.autocompleteIndex = len(m.autocompleteMatches) - 1
			} else {
				m.autocompleteIndex--
				if m.autocompleteIndex < 0 {
					m.autocompleteIndex = len(m.autocompleteMatches) - 1
				}
			}
			m.clampAutocompleteOffset()
			return m, nil
		}
		if msg.String() == "down" && len(m.autocompleteMatches) > 0 {
			if m.autocompleteIndex < 0 {
				m.autocompleteIndex = 0
			} else {
				m.autocompleteIndex++
				if m.autocompleteIndex >= len(m.autocompleteMatches) {
					m.autocompleteIndex = 0
				}
			}
			m.clampAutocompleteOffset()
			return m, nil
		}
		// Up/Down recall prompt history when not scrolled into it. The
		// guard allows stepping through history once it's active; typing
		// (below) resets historyIndex so the next Up restarts from recent.
		if msg.String() == "up" && (m.textarea.Value() == "" || m.historyIndex > 0) {
			if m.historyIndex < len(m.promptHistory) {
				m.historyIndex++
				m.textarea.SetValue(m.promptHistory[len(m.promptHistory)-m.historyIndex])
				m.textarea.CursorEnd()
			}
			return m, nil
		}
		if msg.String() == "down" && m.historyIndex > 0 {
			m.historyIndex--
			if m.historyIndex == 0 {
				m.textarea.SetValue("")
			} else {
				m.textarea.SetValue(m.promptHistory[len(m.promptHistory)-m.historyIndex])
				m.textarea.CursorEnd()
			}
			return m, nil
		}
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		// Any other key (typing, backspace, etc.) restarts recall from recent.
		// Up/Down returned early above, so reaching here means a non-nav key.
		if m.historyIndex != 0 {
			m.historyIndex = 0
		}
		m.updateAutocomplete()
		m.resizeTextarea()
		return m, cmd

	case tea.MouseMsg:
		// Mouse wheel scrolls the viewport.
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd

	case agent.Event:
		m.handleEvent(msg)
		m.refresh()
		// Re-issue the drain command. Also issue a tick when
		// subagents are running, to refresh the status bar.
		if m.busy && m.extensions != nil && m.extensions.PendingDelegations() > 0 {
			return m, tea.Batch(m.drainEvents(), m.tickCmd())
		}
		return m, m.drainEvents()

	case tickMsg:
		// Always try to drain InjectedMessages, even if
		// PendingDelegations() is 0 — the result may have been
		// pushed and the counter decremented between ticks.
		if !m.busy && m.extensions != nil {
			ch := m.extensions.InjectedMessages
			if ch != nil {
				var combined string
				draining := true
				for draining {
					select {
					case msg, ok := <-ch:
						if ok {
							if combined != "" {
								combined += "\n\n---\n\n"
							}
							combined += msg.Text
						}
					default:
						draining = false
					}
				}
				if combined != "" {
					m.history = append(m.history, ai.NewUser(combined))
					if m.store != nil && !m.ephemeral && m.sessionID != "" {
						m.store.AppendMessage(context.Background(), m.sessionID, m.history[len(m.history)-1])
					}
					m.busy = true
					m.segments = nil
					m.err = ""
					m.lastRenderedResponse = ""
					if m.extensions != nil && m.extensions.Provider != nil {
		m.extensions.Provider.SetModel(m.modelName)
	}
					m.startRun()
					return m, tea.Batch(m.drainEvents(), m.titleCmd())
				}
			}
		}
		// Keep ticking while subagents are running (idle or busy).
		if m.extensions != nil && m.extensions.PendingDelegations() > 0 {
			return m, m.tickCmd()
		}
		return m, nil

	case streamDoneMsg:
		m.notifyTurnComplete()
		m.busy = false
		m.cancel = nil
		m.textarea.Placeholder = "message (enter to send, ctrl+j for newline, /help for commands)"
		m.refresh()
		// Auto-submit queued input from while the agent was running.
		if m.queuedInput != "" {
			m.textarea.SetValue(m.queuedInput)
			m.queuedInput = ""
			return m.submit()
		}
		// Issue a tick to drain any buffered subagent results.
		// The tick handler drains the channel regardless of
		// PendingDelegations() — covers the race where results
		// were pushed but the counter already hit 0.
		// Auto-name the session after the first exchange if it has no
		// label yet. Runs in background; result arrives as autoNameMsg.
		// Ephemeral sessions have no session to name. The title must be
		// reset on this path too (it is no longer running).
		if !m.autoNamed && !m.ephemeral && m.store != nil && m.sessionID != "" && len(m.history) >= 2 {
			return m, tea.Batch(m.tickCmd(), m.autoNameSession(), m.titleCmd())
		}
		return m, tea.Batch(m.tickCmd(), m.textarea.Focus(), m.titleCmd())

	case autoNameMsg:
		// Drop stale results: a /new (gen mismatch) or a session switch
		// (id mismatch) while the goroutine ran. A stale result must not
		// re-apply an old title or block auto-naming for the new view.
		if msg.gen != m.autoNameGen || msg.sessionID != m.sessionID {
			return m, nil
		}
		m.autoNamed = true
		if msg.err == nil && msg.label != "" {
			m.sessionLabel = msg.label
		}
		return m, nil

	case compactionResultMsg:
		m.compacting = false
		if msg.err != nil {
			m.err = "compact: " + msg.err.Error()
			m.refresh()
			return m, nil
		}
		if len(msg.messages) == msg.before {
			m.err = "nothing to compact (under budget)"
			m.refresh()
			return m, nil
		}
		m.history = msg.messages
		m.transcript += "\n" + m.theme.Info.Render("[compacted: "+fmt.Sprintf("%d messages → %d messages", msg.before, len(msg.messages))+"]") + "\n"
		m.err = ""
		m.refresh()
		return m, nil

	case modelsLoadedMsg:
		// Ctrl+P triggered a background fetch; apply the result.
		if msg.err != nil {
			m.err = msg.err.Error()
			m.refresh()
			return m, nil
		}
		m.modelList = msg.models
		if len(msg.models) > 0 {
			m.modelName = msg.models[0]
			m.persistEntry(ai.NewModelChange(m.modelName))
			m.transcript += "\n" + m.theme.Info.Render("[model: "+m.modelName+"]") + "\n"
			m.err = ""
			m.refresh()
			if m.extensions != nil && m.extensions.Provider != nil {
		m.extensions.Provider.SetModel(m.modelName)
	}
			return m, tea.Batch(m.titleCmd(), m.fetchModelInfoCmd())
		}
		m.err = "no models available"
		m.refresh()
		return m, nil

	case modelInfoLoadedMsg:
		// Auto-discovered context window from the provider. On
		// error or 0, leave the field at 0 — statusLine falls
		// back to compaction config.
		m.contextWindow = msg.contextWindow
		return m, nil

	default:
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		return m, cmd
	}
}

// submit sends the current input as a user message and starts a run.
func (m model) submit() (tea.Model, tea.Cmd) {
	input := strings.TrimSpace(m.textarea.Value())
	if input == "" {
		return m, nil
	}
	m.textarea.SetValue("")
	m.promptHistory = append(m.promptHistory, input)
	m.historyIndex = 0

	// Echo the input as a user message in the transcript.
	m.transcript += "\n" + m.theme.User.Render("> "+wordWrap(input, m.viewport.Width)) + "\n"

	// Slash commands run locally and never hit the agent.
	if strings.HasPrefix(input, "/") {
		return m.handleCommand(input)
	}

	// Inline skill invocation: "/name" tokens inside a normal message
	// inject the matching skill's content as a system message. The user
	// text is left unchanged so the model sees both the reference and
	// the skill. Unknown tokens (URLs, paths) are ignored.
	m = m.invokeInlineSkills(input)

	// Extract @file references: load images as base64, inline text files.
	prompt, images, imgErr := extractImages(input, m.store, m.skills)
	if imgErr != nil {
		m.transcript += "\n" + m.theme.Error.Render("[image error: "+imgErr.Error()+"]") + "\n"
	}
	if len(images) > 0 {
		m.transcript += "\n" + m.theme.Info.Render(fmt.Sprintf("[loaded %d image(s)]", len(images))) + "\n"
		m.history = append(m.history, ai.NewUserWithImages(prompt, images))
	} else {
		m.history = append(m.history, ai.NewUser(prompt))
	}
	m.busy = true
	m.segments = nil
	m.err = ""
	m.lastRenderedResponse = ""

	// Persist the user message; auto-create a session on the first one.
	// Ephemeral sessions skip the store entirely.
	if m.store != nil && !m.ephemeral {
		if m.sessionID == "" {
			id, err := agent.NewSessionID()
			if err != nil {
				m.storeErr = "session id: " + err.Error()
				m.busy = false
				m.refresh()
				return m, nil
			}
			if err := m.store.CreateSession(context.Background(), id, "", ""); err != nil {
				m.storeErr = "create session: " + err.Error()
				m.busy = false
				m.refresh()
				return m, nil
			}
			m.sessionID = id
			m.storeErr = ""
		}
		if err := m.store.AppendMessage(context.Background(), m.sessionID, m.history[len(m.history)-1]); err != nil {
			m.storeErr = "save message: " + err.Error()
		} else {
			m.storeErr = ""
		}
	}

	// Capture the current provider settings; /model and /provider apply next turn.
	// Update the provider's model name at runtime.
	if m.extensions != nil && m.extensions.Provider != nil {
		m.extensions.Provider.SetModel(m.modelName)
	}
	m.startRun()
	return m, tea.Batch(m.drainEvents(), m.titleCmd())
}

// startRun creates a new Agent, configures it, and starts the event
// channel. Shared by submit() and the tick handler for injected
// subagent results.
func (m *model) startRun() {
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	var provider ai.Provider
	if m.extensions != nil {
		provider = m.extensions.Provider
	}
	if provider != nil {
		provider.SetThinkingLevel(m.thinkingLevel)
	}
	var ag *agent.Agent
	if m.extensions != nil {
		ag = agent.NewFromContext(m.extensions, agent.AgentOptions{
			PromptCustom:  m.promptCustom,
			PromptAppend:  m.promptAppend,
			PromptContext: m.promptContext,
			CWD:           m.cwd,
		})
	} else {
		ag = agent.NewAgent(provider, nil, 0)
		ag.SetCWD(m.cwd)
		ag.SetPromptCustom(m.promptCustom)
		ag.SetPromptAppend(m.promptAppend)
		ag.SetPromptContext(m.promptContext)
	}
	if m.compaction != nil {
		ag.SetMaxToolOutput(m.compaction.MaxToolOutput)
	}
	// Pass a non-nil channel as a mode flag so the agent loop
	// knows this is TUI mode (don't block on subagent results).
	m.userInput = make(chan string, 1)
	ag.SetUserInput(m.userInput)
	m.events = ag.Run(ctx, m.history, nil)
}

// drainEvents returns a command that reads one event from the channel and
// delivers it to Update. It is re-issued after each event so the stream
// keeps flowing. A nil event (channel closed) signals the run is done.
func (m model) drainEvents() tea.Cmd {
	return func() tea.Msg {
		event, ok := <-m.events
		if !ok {
			return streamDoneMsg{}
		}
		return event
	}
}

// tickMsg triggers a status bar refresh while the agent is busy.
type tickMsg struct{}

// tickCmd returns a command that fires after 250ms, triggering a
// re-render of the status bar (subagent count, etc.).
func (m model) tickCmd() tea.Cmd {
	return tea.Tick(250*time.Millisecond, func(time.Time) tea.Msg {
		return tickMsg{}
	})
}

// handleEvent folds one agent event into the streaming segments.
// Segments are appended in the order the model emits them, preserving
// the narrative flow (thinking → tool → response → more thinking → ...).
func (m *model) handleEvent(event agent.Event) {
	switch e := event.(type) {
	case agent.StreamEvent:
		switch chunk := e.Event.(type) {
		case ai.ResponseChunk:
			m.appendSegment("response", chunk.Content)
		case ai.ThinkingChunk:
			m.appendSegment("thinking", chunk.Content)
		case ai.ToolCallEvent:
			m.lastToolCall = chunk.ToolCall.Name
			m.lastToolArgs = chunk.ToolCall.Arguments
			var sb strings.Builder
			sb.WriteString("\n")
			sb.WriteString(m.theme.Tool.Render("[tool: " + chunk.ToolCall.Name + "]"))
			sb.WriteString("\n")
			if len(chunk.ToolCall.Arguments) > 0 {
				for k, v := range chunk.ToolCall.Arguments {
					sb.WriteString(wordWrap(fmt.Sprintf("  %s: %v", k, v), m.viewport.Width))
					sb.WriteString("\n")
				}
				sb.WriteString("\n")
			}
			m.segments = append(m.segments, streamSegment{kind: "tool", content: sb.String()})
		case ai.StreamEnd:
			if chunk.Error != "" {
				m.appendSegment("response", "\n"+m.theme.Error.Render("error: "+chunk.Error)+"\n")
			}
		}
	case agent.AgentEnd:
		if e.Error != "" {
			m.err = e.Error
		}
		// Fold segments into the transcript in order. Thinking and tool
		// segments are lipgloss-styled; response segments go through
		// glamour for markdown rendering.
		var responseBuf strings.Builder
		for _, seg := range m.segments {
			switch seg.kind {
			case "thinking":
				if m.showThinking {
					m.transcript += "\n" + m.theme.Thinking.Render("[thinking]") + "\n"
					m.transcript += m.theme.Thinking.Render(wordWrap(seg.content, m.viewport.Width)) + "\n"
				}
			case "tool":
				m.transcript += seg.content
			case "tool_result":
				m.transcript += "\n" + m.theme.Tool.Render(seg.content) + "\n"
			case "tool_result_highlighted":
				m.transcript += "\n" + seg.content + "\n"
			case "response":
				responseBuf.WriteString(seg.content)
			}
		}
		if responseBuf.Len() > 0 {
			response := e.Message
			if response.Content == "" {
				response.Content = strings.TrimSuffix(responseBuf.String(), "\n")
			}
			m.transcript += "\n" + renderMarkdown(response.Content, m.viewport.Width, m.theme) + "\n"
		}
		m.segments = nil
	case agent.ToolResultEvent:
		m.history = append(m.history, e.Message)
		if m.store != nil && m.sessionID != "" {
			if err := m.store.AppendMessage(context.Background(), m.sessionID, e.Message); err != nil {
				m.storeErr = "save tool result: " + err.Error()
			} else {
				m.storeErr = ""
			}
		}
		// Append as a segment so it renders in order with thinking
		// and tool calls at AgentEnd, not out of sequence.
		lines := strings.Count(e.Message.Content, "\n") + 1
		// Determine language for syntax highlighting from the last tool call.
		lang := ""
		if !e.Message.IsError {
			lang = langForTool(m.lastToolCall, m.lastToolArgs)
		}
		switch {
		case m.showToolResults && !m.toolResultsAuto:
			content := e.Message.Content
			if lang != "" {
				content = highlightCode(content, lang, m.theme.Name)
				m.appendSegment("tool_result_highlighted", wordWrap(content, m.viewport.Width))
			} else {
				m.appendSegment("tool_result", wordWrap(content, m.viewport.Width))
			}
		case m.toolResultsAuto && lines <= toolResultAutoThreshold:
			content := e.Message.Content
			if lang != "" {
				content = highlightCode(content, lang, m.theme.Name)
				m.appendSegment("tool_result_highlighted", wordWrap(content, m.viewport.Width))
			} else {
				m.appendSegment("tool_result", wordWrap(content, m.viewport.Width))
			}
		default:
			m.appendSegment("tool_result", fmt.Sprintf("[tool result: %d lines]", lines))
		}
	case agent.AssistantMessageEvent:
		m.history = append(m.history, e.Message)
		if m.store != nil && m.sessionID != "" {
			if err := m.store.AppendMessage(context.Background(), m.sessionID, e.Message); err != nil {
				m.storeErr = "save assistant: " + err.Error()
			} else {
				m.storeErr = ""
			}
		}
	}
}

// appendSegment appends content to the last segment if it has the same
// kind, or creates a new segment. This keeps consecutive chunks of the
// same type (e.g. multiple ResponseChunks) in one segment.
func (m *model) appendSegment(kind, content string) {
	if len(m.segments) > 0 && m.segments[len(m.segments)-1].kind == kind {
		m.segments[len(m.segments)-1].content += content
	} else {
		m.segments = append(m.segments, streamSegment{kind: kind, content: content})
	}
}

// handleCommand executes a slash command and returns a follow-up command.
func (m model) handleCommand(input string) (tea.Model, tea.Cmd) {
	fields := strings.Fields(input)
	switch fields[0] {
	case "/exit":
		return m, tea.Quit
	case "/new":
		return m.handleExtensionCommand("/new", strings.Join(fields[1:], " "))
	case "/sessions", "/resume", "/branch", "/label", "/tree":
		if m.ephemeral {
			m.err = "no sessions in ephemeral mode"
			return m, nil
		}
		switch fields[0] {
		case "/sessions":
			if len(fields) > 1 && fields[1] == "delete" {
				return m.handleSessionDelete(fields[2:])
			}
			return m.handleExtensionCommand("/sessions", strings.TrimSpace(strings.TrimPrefix(input, "/sessions")))
		case "/resume":
			return m.handleExtensionCommand("/resume", strings.Join(fields[1:], " "))
		case "/branch":
			// If the user passes an explicit parent ID, use it.
			// Otherwise pass the current session ID.
			rest := strings.Join(fields[1:], " ")
			if rest != "" {
				return m.handleExtensionCommand("/branch", rest)
			}
			return m.handleExtensionCommand("/branch", m.sessionID)
		case "/label":
			return m.handleExtensionCommand("/label", m.sessionID+" "+strings.Join(fields[1:], " "))
		case "/tree":
			return m.handleExtensionCommand("/tree", "")
		}
		return m, nil // unreachable; all cases return
	case "/copy":
		return m.handleCopy()
	case "/export":
		if m.ephemeral {
			m.err = "no sessions in ephemeral mode"
			return m, nil
		}
		return m.handleExtensionCommand("/export", m.sessionID+" "+strings.Join(fields[1:], " "))
	case "/insights":
		if m.ephemeral {
			m.err = "no sessions in ephemeral mode"
			return m, nil
		}
		return m.handleExtensionCommand("/insights", strings.Join(fields[1:], " "))
	case "/search":
		if m.ephemeral {
			m.err = "no sessions in ephemeral mode"
			return m, nil
		}
		return m.handleExtensionCommand("/search", strings.Join(fields[1:], " "))
	case "/thinking":
		return m.handleExtensionCommand("/thinking", strings.Join(fields[1:], " "))
	case "/tools":
		return m.handleExtensionCommand("/tools", strings.Join(fields[1:], " "))
	case "/extensions":
		return m.handleExtensions()
	case "/theme":
		return m.handleTheme(fields[1:])
	case "/help":
		m.transcript += m.renderHelp()
		m.refresh()
		return m, nil
	case "/model":
		if len(fields) < 2 {
			m.err = "usage: /model <#|name>"
			return m, nil
		}
		arg := fields[1]
		// If numeric and cache is populated, select by line number.
		if n, err := strconv.Atoi(arg); err == nil && len(m.modelList) > 0 {
			if n < 1 || n > len(m.modelList) {
				m.err = fmt.Sprintf("model number %d out of range (1-%d)", n, len(m.modelList))
				m.refresh()
				return m, nil
			}
			arg = m.modelList[n-1]
		}
		// Validate against cached model list if available.
		if len(m.modelList) > 0 {
			found := false
			for _, name := range m.modelList {
				if name == arg {
					found = true
					break
				}
			}
			if !found {
				m.err = fmt.Sprintf("model %q not found. Use /models to list available models.", arg)
				m.refresh()
				return m, nil
			}
		}
		// Route to the provider extension command.
		return m.handleExtensionCommand("/model", arg)
	default:
		// Check if the command matches a loaded extension command.
		cmd := fields[0]
		if m.extensions != nil {
			for _, c := range m.extensions.Commands {
				if c.Name == cmd {
					return m.handleExtensionCommand(c.Name, strings.TrimSpace(strings.TrimPrefix(input, cmd)))
				}
			}
		}
		// Check if the command matches a loaded skill.
		for _, s := range m.skills {
			if "/"+s.Name == cmd {
				m.transcript += "\n" + m.theme.Info.Render("[skill: "+s.Name+"]") + "\n"
				m.history = append(m.history, ai.NewSystem(s.Content))
				m.refresh()
				return m, nil
			}
		}
		m.err = "unknown command: " + fields[0]
		return m, nil
	}
}

// inlineSkillPattern matches "/name" tokens inside a normal message.
// Restricted to [a-z0-9-] so URLs (//, dots) and file paths (/d/...) do
// not match skill names by accident.
var inlineSkillPattern = regexp.MustCompile(`/([a-z0-9-]+)`)

// invokeInlineSkills scans a non-command message for "/name" tokens that
// match a loaded skill and injects each skill's content as a system
// message. The user text is left unchanged. Unknown tokens are ignored.
func (m model) invokeInlineSkills(input string) model {
	if len(m.skills) == 0 {
		return m
	}
	for _, match := range inlineSkillPattern.FindAllStringSubmatch(input, -1) {
		name := match[1]
		for _, s := range m.skills {
			if s.Name == name {
				m.history = append(m.history, ai.NewSystem(s.Content))
				m.transcript += "\n" + m.theme.Info.Render("[skill: "+s.Name+"]") + "\n"
				break
			}
		}
	}
	return m
}

// handleTabComplete accepts the currently selected autocomplete match (a
// single match is already auto-selected by updateAutocomplete), and leaves
// the input unchanged otherwise.
func (m model) handleTabComplete() (tea.Model, tea.Cmd) {
	if len(m.autocompleteMatches) == 0 {
		return m, nil
	}
	m.acceptMatch()
	m.refresh()
	return m, nil
}

// findSlashToken returns the byte offset of the last / in val that is
// either at position 0 or preceded by a space. This is the slash that
// triggers autocomplete. Returns -1 when no qualifying slash exists.
func findSlashToken(val string) int {
	for i := len(val) - 1; i >= 0; i-- {
		if val[i] != '/' {
			continue
		}
		if i == 0 || val[i-1] == ' ' {
			return i
		}
	}
	return -1
}

// updateAutocomplete recomputes the live slash-command matches from the
// current input. It runs after every keystroke. The autocomplete triggers
// when a / appears at the start of the input or after a space, so skills
// can be autocompleted mid-sentence (e.g. "go ahead and /lear...").
// Two levels: a bare command matches against knownCommands; a command
// with enum arguments (see commandOptions) matches against its options.
func (m *model) updateAutocomplete() {
	val := m.textarea.Value()
	slashPos := findSlashToken(val)
	if slashPos < 0 {
		m.autocompleteMatches = nil
		m.autocompleteIndex = -1
		m.autocompleteOffset = 0
		m.autocompleteSlashPos = -1
		m.resizeTextarea() // panel closed; give the rows back to the viewport
		return
	}
	m.autocompleteSlashPos = slashPos
	// Extract the partial command after the slash.
	partial := val[slashPos:]
	matches := m.autocompleteMatches[:0]
	// Split at the first space: cmd is the command part.
	cmd, _, _ := strings.Cut(partial, " ")
	if options, ok := commandOptions[cmd]; ok {
		// Second level: match options by prefix. Full strings so
		// acceptMatch replaces the whole token unchanged.
		for _, opt := range options {
			full := cmd + " " + opt
			if strings.HasPrefix(full, partial) {
				matches = append(matches, full)
			}
		}
	} else {
		for _, c := range m.commands {
			if strings.HasPrefix(c, partial) {
				matches = append(matches, c)
			}
		}
	}
	m.autocompleteMatches = matches
	// Keep the highlight in range. A single match is auto-selected so
	// Enter/Tab accept it immediately without an explicit arrow.
	if len(matches) == 1 {
		m.autocompleteIndex = 0
	} else if m.autocompleteIndex >= len(matches) {
		m.autocompleteIndex = len(matches) - 1
	}
	// Keep the window in range when the match list shrinks.
	if m.autocompleteOffset > len(matches)-1 {
		m.autocompleteOffset = len(matches) - 1
	}
	if m.autocompleteOffset < 0 {
		m.autocompleteOffset = 0
	}
	// The panel height changed; re-layout so the viewport breathes.
	m.resizeTextarea()
}

// acceptMatch accepts the selected match into the textarea. It splices
// the completion at the slash position, preserving any text before it.
// It returns a command (never nil) when it actually changed the input;
// nil when the token already equals the match, so Enter falls through
// to submit.
func (m *model) acceptMatch() tea.Cmd {
	if m.autocompleteIndex < 0 || m.autocompleteIndex >= len(m.autocompleteMatches) {
		return nil
	}
	completion := m.autocompleteMatches[m.autocompleteIndex]
	m.autocompleteMatches = nil
	m.autocompleteIndex = -1
	val := m.textarea.Value()
	slashPos := m.autocompleteSlashPos
	if slashPos < 0 {
		slashPos = 0
	}
	// Build the new value: everything before the slash + the completion.
	newVal := val[:slashPos] + completion
	// Check if the current token already equals the completion.
	if newVal == val {
		return nil
	}
	m.textarea.SetValue(newVal)
	m.textarea.CursorEnd()
	m.err = ""
	m.resizeTextarea()
	m.refresh()
	return func() tea.Msg { return m.textarea.Focus() }
}

// handleSessionDelete deletes a session (by #, id, or label) from the
// store. Messages and child branches cascade. Deleting the current
// session resets the in-memory state like /new.
func (m model) handleSessionDelete(args []string) (tea.Model, tea.Cmd) {
	if m.store == nil {
		m.err = "no store available"
		return m, nil
	}
	if len(args) == 0 {
		m.err = "usage: /sessions delete <#|id|label>"
		return m, nil
	}
	// Populate the resolve cache if empty (extension handler doesn't
	// populate TUI state).
	if len(m.sessionList) == 0 {
		m.sessionList, _ = m.store.ListSessions(context.Background())
	}
	id := m.resolveSession(args[0])
	if id == "" {
		m.err = "session not found: " + args[0]
		return m, nil
	}
	name := store.SessionDisplayName(m.labelOf(id), id)
	if err := m.store.DeleteSession(context.Background(), id); err != nil {
		m.storeErr = "delete: " + err.Error()
		return m, nil
	}
	m.storeErr = ""
	// Drop the deleted session from the resolve cache so # lookups
	// stay accurate.
	for i, s := range m.sessionList {
		if s.ID == id {
			m.sessionList = append(m.sessionList[:i], m.sessionList[i+1:]...)
			break
		}
	}
	// If the deleted session was the active one, or the active session
	// was a branch that cascaded away with it, reset like /new so no
	// dead session id survives in the status bar or store writes.
	if id == m.sessionID {
		m.resetSession()
	} else if m.sessionID != "" {
		if _, err := m.store.GetSession(context.Background(), m.sessionID); err != nil {
			m.resetSession()
		}
	}
	m.transcript += "\n" + m.theme.Info.Render("[deleted: "+name+"]") + "\n"
	m.refresh()
	return m, nil
}

// resetSession clears the active session state, like /new.
func (m *model) resetSession() {
	m.sessionID = ""
	m.sessionLabel = ""
	m.autoNamed = false
	m.history = nil
	m.transcript = ""
	m.segments = nil
}

// persistEntry appends a non-conversation entry (model change, thinking
// level change) to the session store. No-op for ephemeral sessions or
// when no store is available.
func (m *model) persistEntry(msg ai.Message) {
	if m.store == nil || m.sessionID == "" || m.ephemeral {
		return
	}
	_ = m.store.AppendMessage(context.Background(), m.sessionID, msg)
}

// handleExtensions lists loaded extensions with name, tool count,
// command count, and status.
func (m model) handleExtensions() (tea.Model, tea.Cmd) {
	infos := m.extensions.Infos
	if len(infos) == 0 {
		m.transcript += "\n" + m.theme.Info.Render("[no extensions loaded]") + "\n"
		m.refresh()
		return m, nil
	}

	nameWidth := 12
	for _, info := range infos {
		if len(info.Name) > nameWidth {
			nameWidth = len(info.Name)
		}
	}

	var sb strings.Builder
	sb.WriteString("\n")
	header := fmt.Sprintf("%-*s  %5s  %8s  %s", nameWidth, "NAME", "TOOLS", "COMMANDS", "SEAMS")
	sb.WriteString(m.theme.Info.Render(header))
	sb.WriteString("\n")
	for _, info := range infos {
		seams := strings.Join(info.Seams, ", ")
		fmt.Fprintf(&sb, "%-*s  %5d  %8d  %s\n", nameWidth, info.Name, info.Tools, info.Commands, seams)
	}
	m.transcript += sb.String()
	m.refresh()
	return m, nil
}

// handleTheme switches the active theme or lists available themes.
func (m model) handleTheme(args []string) (tea.Model, tea.Cmd) {
	if len(args) == 0 {
		// List available themes with the current one marked.
		// "auto" is a virtual option (not in the themes map) so
		// it is appended manually.
		var sb strings.Builder
		sb.WriteString("\n")
		all := append(themeNames(), "auto")
		for _, name := range all {
			marker := "  "
			if name == m.theme.Name {
				marker = "* "
			}
			fmt.Fprintf(&sb, "%s%s\n", marker, name)
		}
		m.transcript += sb.String()
		m.refresh()
		return m, nil
	}
	name := args[0]
	if name == "auto" {
		resolved := detectSystemTheme()
		t, ok := themes[resolved]
		if !ok {
			t = themes["dark"]
		}
		t.Name = "auto"
		m.theme = t
		if len(m.history) > 0 {
			m.transcript = renderTranscript(m.history, m.viewport.Width, m.theme)
			// Re-add the user's echo line: slash commands are not in
			// m.history, so renderTranscript omits them.
			m.transcript += "\n" + m.theme.User.Render("> /theme "+strings.Join(args, " ")) + "\n"
		}
		m.transcript += "\n" + m.theme.Info.Render("[theme: auto ("+resolved+")]") + "\n"
		m.refresh()
		return m, nil
	}
	t, ok := themes[name]
	if !ok {
		m.err = "unknown theme: " + name
		m.refresh()
		return m, nil
	}
	m.theme = t
	if len(m.history) > 0 {
		m.transcript = renderTranscript(m.history, m.viewport.Width, m.theme)
		m.transcript += "\n" + m.theme.User.Render("> /theme "+strings.Join(args, " ")) + "\n"
	}
	m.transcript += "\n" + m.theme.Info.Render("[theme: "+name+"]") + "\n"
	m.refresh()
	return m, nil
}

// handleExtensionCommand runs an extension-provided slash command.
func (m model) handleExtensionCommand(name, args string) (tea.Model, tea.Cmd) {
	if m.extensions == nil || m.extensions.CommandHandler == nil {
		m.err = "no command handler configured"
		return m, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := m.extensions.CommandHandler(ctx, name, args)
	if err != nil {
		m.err = err.Error()
		return m, nil
	}
	if result.Text != "" {
		m.transcript += "\n" + m.theme.Info.Render(result.Text) + "\n"
	}
	m.refresh()
	// Process TUI actions declared by the extension.
	var cmds []tea.Cmd
	for _, action := range result.Actions {
		switch action.Type {
		case "set_model":
			m.modelName = action.Value
			m.persistEntry(ai.NewModelChange(m.modelName))
		case "set_model_list":
			m.modelList = action.List
		case "set_tool_display":
			switch action.Value {
			case "expanded":
				m.showToolResults = true
				m.toolResultsAuto = false
			case "collapsed":
				m.showToolResults = false
				m.toolResultsAuto = false
			case "auto":
				m.showToolResults = true
				m.toolResultsAuto = true
			}
		case "set_thinking":
			m.thinkingLevel = action.Value
			m.showThinking = ai.ThinkingEnabled(m.thinkingLevel)
			m.persistEntry(ai.NewThinkingLevelChange(m.thinkingLevel))
		case "new_session":
			m.history = nil
			m.transcript = ""
			m.segments = nil
			m.err = ""
			m.sessionLabel = ""
			m.autoNamed = false
			m.autoNameGen++
			m.sessionID = ""
			m.ephemeral = action.Value == "--ephemeral"
			m.refresh()
		case "load_session":
			id := action.Value
			if m.store == nil {
				m.err = "no store available"
				return m, nil
			}
			messages, err := m.store.GetMessages(context.Background(), id)
			if err != nil {
				m.err = "resume: " + err.Error()
				return m, nil
			}
			m.sessionID = id
			m.history = messages
			m.transcript = renderTranscript(messages, m.viewport.Width, m.theme)
			m.segments = nil
			m.err = ""
			m.storeErr = ""
			for _, msg := range messages {
				switch v := msg.(type) {
				case ai.ModelChange:
					m.modelName = v.Model
				case ai.ThinkingLevelChange:
					m.thinkingLevel = v.Level
					m.showThinking = ai.ThinkingEnabled(v.Level)
				}
			}
			if sess, err := m.store.GetSession(context.Background(), id); err == nil && sess.Label != "" {
				m.sessionLabel = sess.Label
				m.autoNamed = true
			}
			m.refresh()
		case "branch_session":
			id := action.Value
			if m.store == nil {
				m.err = "no store available"
				return m, nil
			}
			messages, err := m.store.GetAncestorMessages(context.Background(), id)
			if err != nil {
				m.err = "branch: " + err.Error()
				return m, nil
			}
			if m.compaction != nil && len(messages) > m.compaction.KeepFirst+m.compaction.KeepLast+5 {
				messages = summarizeForBranch(messages, m.compaction.KeepFirst, m.compaction.KeepLast)
			}
			m.sessionID = id
			m.history = messages
			m.transcript = renderTranscript(messages, m.viewport.Width, m.theme)
			m.segments = nil
			m.err = ""
			m.refresh()
		case "set_label":
			m.sessionLabel = action.Value
			m.refresh()
		case "refresh_title":
			cmds = append(cmds, m.titleCmd())
		case "fetch_model_info":
			cmds = append(cmds, m.fetchModelInfoCmd())
		case "run_compact":
			return m.handleCompact()
		}
	}
	if len(cmds) > 0 {
		return m, tea.Batch(cmds...)
	}
	return m, nil
}

// labelOf returns the stored label for a session, or "" when it has
// none or cannot be read.
func (m model) labelOf(id string) string {
	sess, err := m.store.GetSession(context.Background(), id)
	if err != nil {
		return ""
	}
	return sess.Label
}

// resolveSession resolves a /resume argument to a session ID.
// Tries: line number (#3 or 3) from the cached session list,
// exact session ID match, then case-insensitive label prefix match.
func (m model) resolveSession(arg string) string {
	// Strip leading # for line numbers.
	numStr := strings.TrimPrefix(arg, "#")
	if n, err := strconv.Atoi(numStr); err == nil && n >= 1 && n <= len(m.sessionList) {
		return m.sessionList[n-1].ID
	}
	// Try exact ID match.
	for _, s := range m.sessionList {
		if s.ID == arg {
			return s.ID
		}
	}
	// Try case-insensitive label prefix match.
	lower := strings.ToLower(arg)
	for _, s := range m.sessionList {
		if s.Label != "" && strings.HasPrefix(strings.ToLower(s.Label), lower) {
			return s.ID
		}
	}
	// Fallback: try the store directly (for IDs not in the cached list).
	if _, err := m.store.GetSession(context.Background(), arg); err == nil {
		return arg
	}
	return ""
}

// handleCopy copies the last message in the transcript to the system
// clipboard. Works on user, assistant, and tool result messages.
func (m model) handleCopy() (tea.Model, tea.Cmd) {
	if len(m.history) == 0 {
		m.err = "nothing to copy"
		return m, nil
	}
	last := m.history[len(m.history)-1]
	var text string
	switch msg := last.(type) {
	case ai.User:
		text = msg.Content
	case ai.Assistant:
		text = msg.Content
	case ai.ToolResult:
		text = msg.Content
	}
	if text == "" {
		m.err = "nothing to copy"
		return m, nil
	}
	if err := clipboard.WriteAll(text); err != nil {
		m.err = "copy failed: " + err.Error()
	}
	return m, nil
}

// handleCompact triggers manual compaction of the conversation history
// via the compactor extension. Runs asynchronously so the TUI stays
// responsive while the LLM summarizes.
// summarizeForBranch trims a long message history for a branch by
// keeping the first keepFirst messages, a synthetic summary placeholder,
// and the last keepLast messages.
func summarizeForBranch(messages []ai.Message, keepFirst, keepLast int) []ai.Message {
	if keepFirst+keepLast >= len(messages) {
		return messages
	}
	head := messages[:keepFirst]
	tail := messages[len(messages)-keepLast:]
	summary := ai.NewSystem(fmt.Sprintf("[branch summary: %d messages omitted from parent session]", len(messages)-keepFirst-keepLast))
	return append(append(head, summary), tail...)
}

func (m model) handleCompact() (tea.Model, tea.Cmd) {
	if m.compacting {
		m.err = "already compacting..."
		return m, nil
	}
	if len(m.history) < 3 {
		m.err = "not enough history to compact"
		return m, nil
	}
	if m.compaction == nil {
		m.err = "compaction not configured"
		return m, nil
	}
	cp := m.extensions.Compactor
	if cp == nil {
		m.err = "no compactor extension loaded"
		return m, nil
	}
	m.compacting = true
	m.err = ""
	history := m.history
	before := len(history)
	return m, func() tea.Msg {
		compacted, err := cp.Compact(context.Background(), history)
		return compactionResultMsg{messages: compacted, err: err, before: before}
	}
}

// resizeTextarea adjusts the textarea height based on its current content,
// clamped between minTextareaHeight and maxTextareaHeight. It also resizes
// the viewport to fill the remaining space minus the status bar and the
// autocomplete panel (when open).
func (m *model) resizeTextarea() {
	lines := strings.Count(m.textarea.Value(), "\n") + 1
	if lines < minTextareaHeight {
		lines = minTextareaHeight
	}
	if lines > maxTextareaHeight {
		lines = maxTextareaHeight
	}
	m.textarea.SetHeight(lines)
	vpHeight := m.screenHeight - m.textarea.Height() - statusLines - m.autocompleteHeight()
	if vpHeight < 1 {
		vpHeight = 1
	}
	m.viewport.Height = vpHeight
}

// renderTranscript renders a message history as the TUI transcript.
// Assistant messages render thinking, tool calls, and content in order
// (matching the live AgentEnd path); tool results render full; compacted
// summaries render dimmed. Other system messages (skills, injected
// prompts) are not persisted and never appear here.
func renderTranscript(messages []ai.Message, width int, t Theme) string {
	var sb strings.Builder
	// Track the last assistant's tool calls so we can infer the
	// language for syntax highlighting tool results by ToolCallID.
	toolCallsByID := make(map[string]ai.ToolCall)
	for _, msg := range messages {
		switch m := msg.(type) {
		case ai.User:
			sb.WriteString("\n")
			sb.WriteString(t.User.Render("> " + wordWrap(m.Content, width)))
			sb.WriteString("\n")
		case ai.Assistant:
			// Register tool calls for matching with subsequent results.
			for _, call := range m.ToolCalls {
				toolCallsByID[call.ID] = call
			}
			sb.WriteString("\n")
			if m.Thinking != nil && *m.Thinking != "" {
				sb.WriteString(t.Thinking.Render("[thinking]"))
				sb.WriteString("\n")
				sb.WriteString(t.Thinking.Render(wordWrap(*m.Thinking, width)))
				sb.WriteString("\n")
			}
			for _, call := range m.ToolCalls {
				sb.WriteString(t.Tool.Render("[tool: " + call.Name + "]"))
				sb.WriteString("\n")
				if len(call.Arguments) > 0 {
					for k, v := range call.Arguments {
						sb.WriteString(wordWrap(fmt.Sprintf("  %s: %v", k, v), width))
						sb.WriteString("\n")
					}
					sb.WriteString("\n")
				}
			}
			if m.Content != "" {
				sb.WriteString(renderMarkdown(m.Content, width, t))
				sb.WriteString("\n")
			}
		case ai.ToolResult:
			sb.WriteString("\n")
			if m.IsError {
				sb.WriteString(t.Error.Render("[tool result: error]"))
			} else {
				sb.WriteString(t.Tool.Render("[tool result]"))
			}
			sb.WriteString("\n")
			content := m.Content
			hasHighlight := false
			if !m.IsError {
				if call, ok := toolCallsByID[m.ToolCallID]; ok {
					lang := langForTool(call.Name, call.Arguments)
					if lang != "" {
						content = highlightCode(content, lang, t.Name)
						hasHighlight = true
					}
				}
			}
			if hasHighlight {
				// Don't wrap highlighted content in tool color;
				// let chroma's colors stand on their own.
				sb.WriteString(wordWrap(content, width))
			} else {
				sb.WriteString(t.Tool.Render(wordWrap(content, width)))
			}
			sb.WriteString("\n")
		case ai.System:
			// Only compaction summaries are persisted; render them so
			// the user sees the history was summarized.
			if strings.HasPrefix(m.Content, "[compacted:") {
				sb.WriteString("\n")
				sb.WriteString(t.Info.Render(m.Content))
				sb.WriteString("\n")
			}
		}
	}
	return sb.String()
}

// refresh re-renders the viewport content from the transcript and segments.
// Response segments render through glamour (markdown + syntax highlighting)
// during streaming, debounced to 80ms to prevent CPU thrashing. The final
// AgentEnd render is always a clean full glamour pass.
func (m *model) refresh() {
	var sb strings.Builder
	sb.WriteString(m.transcript)
	for _, seg := range m.segments {
		switch seg.kind {
		case "thinking":
			sb.WriteString("\n")
			sb.WriteString(m.theme.Thinking.Render("[thinking]"))
			sb.WriteString("\n")
			sb.WriteString(m.theme.Thinking.Render(wordWrap(seg.content, m.viewport.Width)))
			sb.WriteString("\n")
		case "tool":
			sb.WriteString(seg.content)
		case "tool_result":
			sb.WriteString("\n")
			sb.WriteString(m.theme.Tool.Render(seg.content))
			sb.WriteString("\n")
		case "tool_result_highlighted":
			sb.WriteString("\n")
			sb.WriteString(seg.content)
			sb.WriteString("\n")
		case "response":
			if m.busy && time.Since(m.lastRender) < 80*time.Millisecond {
				// Debounce: show the last glamour-rendered output
				// instead of raw text to avoid flicker.
				if m.lastRenderedResponse != "" {
					sb.WriteString(m.lastRenderedResponse)
				} else {
					sb.WriteString(seg.content)
				}
			} else {
				m.lastRender = time.Now()
				m.lastRenderedResponse = renderMarkdown(seg.content, m.viewport.Width, m.theme)
				sb.WriteString(m.lastRenderedResponse)
			}
		}
	}
	m.viewport.SetContent(sb.String())
	m.viewport.GotoBottom()
}

// extLexers maps file extensions to chroma lexer names for syntax
// highlighting in tool results.
var extLexers = map[string]string{
	".go":         "go",
	".py":         "python",
	".js":         "javascript",
	".jsx":        "javascript",
	".ts":         "typescript",
	".tsx":        "typescript",
	".rs":         "rust",
	".java":       "java",
	".c":          "c",
	".cpp":        "cpp",
	".cc":         "cpp",
	".h":          "c",
	".hpp":        "cpp",
	".sh":         "bash",
	".bash":       "bash",
	".zsh":        "bash",
	".yaml":       "yaml",
	".yml":        "yaml",
	".json":       "json",
	".xml":        "xml",
	".html":       "html",
	".css":        "css",
	".scss":       "scss",
	".sql":        "sql",
	".md":         "markdown",
	".toml":       "toml",
	".rb":         "ruby",
	".php":        "php",
	".swift":      "swift",
	".kt":         "kotlin",
	".lua":        "lua",
	".dart":       "dart",
	".dockerfile": "dockerfile",
}

// langForTool determines the chroma lexer name for a tool result based
// on the tool name and its arguments. Returns "" if no language can be
// determined (plain text fallback).
func langForTool(toolName string, args map[string]any) string {
	switch toolName {
	case "files.read":
		if path, ok := args["path"].(string); ok {
			ext := strings.ToLower(filepath.Ext(path))
			if lang, ok := extLexers[ext]; ok {
				return lang
			}
			// Special-case filenames without extensions.
			name := strings.ToLower(filepath.Base(path))
			if name == "dockerfile" || name == "containerfile" {
				return "dockerfile"
			}
			if name == "makefile" || name == "gnumakefile" {
				return "makefile"
			}
		}
	case "shell.run", "shell", "bash":
		return "bash"
	}
	return ""
}

// highlightCode applies chroma syntax highlighting to content using
// Catppuccin chroma styles (catppuccin-mocha for dark, catppuccin-latte
// for light). Returns the original content unchanged if highlighting
// fails or lang is empty.
func highlightCode(content, lang, themeName string) string {
	if lang == "" {
		return content
	}
	style := "catppuccin-mocha"
	if themeName == "light" {
		style = "catppuccin-latte"
	}
	var buf bytes.Buffer
	if err := quick.Highlight(&buf, content, lang, "terminal256", style); err != nil {
		return content
	}
	return strings.TrimRight(buf.String(), "\n")
}

// ansiRegex strips ANSI SGR escape codes from lipgloss-styled text
// before glamour processes it, so color codes don't render as literal
// characters in the transcript.
var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// renderAssistant renders markdown content through glamour to styled
// terminal output. ANSI escape codes from lipgloss are stripped first
// so glamour doesn't render them as literal text. Falls back to raw
// text if rendering fails. The themeName selects glamour's color style
// ("dark" or "light").
func renderAssistant(content string, width int, themeName string) string {
	content = ansiRegex.ReplaceAllString(content, "")
	if width <= 0 {
		width = 80
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(glamourStyleForTheme(themeName)),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return content
	}
	out, err := r.Render(content)
	if err != nil {
		return content
	}
	return strings.TrimRight(out, "\n")
}

// codeBlockRegex matches fenced code blocks: ```lang\ncode\n```.
// Captures the language (group 1) and code content (group 2).
var codeBlockRegex = regexp.MustCompile("(?s)```([a-zA-Z0-9+]*)\n(.*?)```")

// renderMarkdown renders markdown with bordered code blocks. Prose
// sections go through glamour; fenced code blocks are syntax-highlighted
// with chroma and wrapped in a lipgloss border with a language header.
// Replaces renderAssistant for the TUI transcript rendering path.
func renderMarkdown(content string, width int, t Theme) string {
	content = ansiRegex.ReplaceAllString(content, "")
	if width <= 0 {
		width = 80
	}

	// If no fenced code blocks, use plain glamour (fast path).
	if !codeBlockRegex.MatchString(content) {
		return renderAssistant(content, width, t.Name)
	}

	var result strings.Builder
	lastEnd := 0
	for _, match := range codeBlockRegex.FindAllStringSubmatchIndex(content, -1) {
		// Render prose before this code block through glamour.
		if match[0] > lastEnd {
			prose := strings.TrimSpace(content[lastEnd:match[0]])
			if prose != "" {
				result.WriteString(renderAssistant(prose, width, t.Name))
				result.WriteString("\n")
			}
		}
		lang := content[match[2]:match[3]]
		code := content[match[4]:match[5]]
		result.WriteString(renderCodeBlock(code, lang, width, t))
		result.WriteString("\n")
		lastEnd = match[1]
	}
	// Render trailing prose.
	if lastEnd < len(content) {
		prose := strings.TrimSpace(content[lastEnd:])
		if prose != "" {
			result.WriteString(renderAssistant(prose, width, t.Name))
		}
	}
	return strings.TrimRight(result.String(), "\n")
}

// renderCodeBlock highlights code with chroma and wraps it in a
// lipgloss bordered box with a language header label.
func renderCodeBlock(code, lang string, width int, t Theme) string {
	// Highlight the code with Catppuccin chroma style.
	highlighted := highlightCode(code, lang, t.Name)

	// Build the border box.
	boxWidth := width - 2 // account for border chars
	if boxWidth < 10 {
		boxWidth = 10
	}

	// Header: language label (or "text" if empty), styled with theme.Info.
	header := lang
	if header == "" {
		header = "text"
	}
	headerStyled := t.Info.Bold(true).Render(header)

	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.CodeBorder.GetForeground()).
		Width(boxWidth).
		Padding(0, 1)

	// Combine header + code.
	body := headerStyled + "\n" + strings.TrimRight(highlighted, "\n")
	return style.Render(body)
}


// View renders the full screen: viewport on top, status bar, then the
// autocomplete dropup panel (when open), textarea at the bottom. When
// the conversation is empty (fresh start or /new), a startup splash
// replaces the viewport.
func (m model) View() string {
	var sb strings.Builder
	if m.transcript == "" && len(m.segments) == 0 && len(m.history) == 0 {
		sb.WriteString(m.splashView())
	} else {
		sb.WriteString(m.viewport.View())
	}
	sb.WriteString("\n")
	sb.WriteString(m.theme.Status.Render(m.statusLine()))
	sb.WriteString("\n")
	if panel := m.autocompletePanel(); panel != "" {
		sb.WriteString(panel)
		sb.WriteString("\n")
	}
	sb.WriteString(m.textarea.View())
	return sb.String()
}

// splashView renders the startup splash: the ASCII omega logo on the
// left and version/model/tools/hints on the right, side by side. Shown
// when the conversation is empty; scrolls away on first message or
// command.
func (m model) splashView() string {
	provider := m.providerType
	if provider == "" {
		provider = "ollama"
	}
	toolCount := 0
	if m.extensions != nil {
		for _, ext := range m.extensions.Infos {
			toolCount += len(ext.ToolList)
		}
	}
	skillCount := len(m.skills)
	logo := []string{
		`   #"""#  `,
		`  #     # `,
		`  #     # `,
		`  m#   #m `,
	}
	info := []string{
		"omega " + m.version,
		provider + "/" + m.modelName,
		fmt.Sprintf("%d tools | %d skills", toolCount, skillCount),
		"/help for commands - enter to start",
	}
	var lines []string
	lines = append(lines, "") // blank line at the top
	for i := 0; i < len(logo); i++ {
		lines = append(lines, m.theme.Info.Render(fmt.Sprintf("%-6s  %s", logo[i], info[i])))
	}
	// Pad with blank lines to fill the viewport height so the
	// status bar and textarea stay at the bottom of the terminal.
	splashHeight := m.viewport.Height
	if splashHeight <= 0 {
		splashHeight = 20
	}
	for len(lines) < splashHeight {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

// windowTitle builds the terminal title: state + model.
func windowTitle(state, modelName string) string {
	return fmt.Sprintf("Ω | %s | %s", state, modelName)
}

// titleCmd returns a Bubble Tea command that sets the window title to
// the current state and model.
func (m model) titleCmd() tea.Cmd {
	state := "idle"
	if m.busy {
		state = "running"
	} else if m.compacting {
		state = "compacting"
	}
	return tea.SetWindowTitle(windowTitle(state, m.modelName))
}

// notifyTurnComplete fires the configured notification when a turn ends.
// "bell" prints the terminal bell (\x07), "desktop" sends an OS
// notification via beeep, "off" does nothing. Desktop notifications
// run in a goroutine so a slow notification API never blocks the TUI.
func (m model) notifyTurnComplete() {
	switch m.notifications {
	case "desktop":
		go func() {
			_ = beeep.Notify("omega", "Turn complete", "")
		}()
	case "off":
		// no notification
	default: // "bell" or unset
		fmt.Print("\x07")
	}
}

// statusLine returns the bottom status bar text.
func (m model) statusLine() string {
	state := "idle"
	if m.busy {
		state = "running"
	} else if m.compacting {
		state = "compacting"
	}
	sess := m.sessionID
	if m.ephemeral {
		sess = "ephemeral"
	} else if sess == "" {
		sess = "none"
	}
	if m.sessionLabel != "" {
		sess = m.sessionLabel
	}
	provider := m.providerType
	if provider == "" {
		provider = "ollama"
	}
	tokens := agent.EstimateTokens(m.history)
	// Context window priority: auto-discovered from provider > config > default.
	window := agent.DefaultContextWindow
	if m.compaction != nil && m.compaction.ContextWindow > 0 {
		window = m.compaction.ContextWindow
	}
	if m.contextWindow > 0 {
		window = m.contextWindow
	}
	line := fmt.Sprintf("Ω | %s | %s/%s", state, provider, m.modelName)
	if m.thinkingLevel != "none" && m.thinkingLevel != "" {
		line += " | thinking: " + m.thinkingLevel
	}
	line += fmt.Sprintf(" | tokens: %d/%d | %s", tokens, window, sess)
	// Show subagent count if any are running.
	if m.extensions != nil && m.extensions.PendingDelegations != nil {
		if pending := m.extensions.PendingDelegations(); pending > 0 {
			line += fmt.Sprintf(" | %s", m.theme.Info.Render(fmt.Sprintf("%d subagent", pending)))
		}
	}
	switch m.trustState {
	case "trusted":
		line += " | " + m.theme.Info.Render("trusted")
	case "untrusted":
		line += " | " + m.theme.Error.Render("untrusted")
	}
	if m.err != "" {
		line += " | " + m.theme.Error.Render("error: "+m.err)
	}
	if m.storeErr != "" {
		line += " | " + m.theme.Error.Render("store: "+m.storeErr)
	}
	return line
}

// autoNameSession returns a command that calls the provider in a
// background goroutine to generate a short title for the session.
// The result is delivered as an autoNameMsg.
func (m model) autoNameSession() tea.Cmd {
	if len(m.history) < 2 {
		return nil
	}
	firstUser := ""
	firstAssistant := ""
	for _, msg := range m.history {
		switch msg := msg.(type) {
		case ai.User:
			if firstUser == "" {
				firstUser = msg.Content
			}
		case ai.Assistant:
			if firstAssistant == "" {
				firstAssistant = msg.Content
			}
		}
	}
	if firstUser == "" || firstAssistant == "" {
		return nil
	}
	// Truncate to avoid blowing up the title prompt.
	if len(firstUser) > 200 {
		firstUser = firstUser[:200]
	}
	if len(firstAssistant) > 200 {
		firstAssistant = firstAssistant[:200]
	}
	prompt := fmt.Sprintf("Generate a short title (3-5 words) for this conversation. Reply with only the title, no quotes or punctuation.\n\nUser: %s\nAssistant: %s", firstUser, firstAssistant)

	sessionID := m.sessionID
	gen := m.autoNameGen
	store := m.store
	extensions := m.extensions

	return func() tea.Msg {
		provider := extensions.Provider
		messages := []ai.Message{
			ai.NewUser(prompt),
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		events := provider.Stream(ctx, messages, nil)
		var title strings.Builder
		for event := range events {
			if chunk, ok := event.(ai.ResponseChunk); ok {
				title.WriteString(chunk.Content)
			}
		}
		label := strings.TrimSpace(title.String())
		if label == "" {
			return autoNameMsg{sessionID: sessionID, gen: gen, err: fmt.Errorf("empty title")}
		}
		if len(label) > 80 {
			label = label[:80]
		}
		if err := store.UpdateSession(context.Background(), sessionID, label); err != nil {
			return autoNameMsg{sessionID: sessionID, gen: gen, err: err}
		}
		return autoNameMsg{sessionID: sessionID, gen: gen, label: label}
	}
}

// clampAutocompleteOffset keeps the selected row inside the dropup
// window, scrolling the window as the selection moves past its edges.
func (m *model) clampAutocompleteOffset() {
	if m.autocompleteIndex < m.autocompleteOffset {
		m.autocompleteOffset = m.autocompleteIndex
	}
	if m.autocompleteIndex >= m.autocompleteOffset+maxAutocompleteRows {
		m.autocompleteOffset = m.autocompleteIndex - maxAutocompleteRows + 1
	}
	if m.autocompleteOffset < 0 {
		m.autocompleteOffset = 0
	}
}

// autocompleteHeight returns the height the dropup panel occupies:
// 0 when there are no matches, otherwise the visible row count plus the
// border, capped at maxAutocompleteRows. When the list is truncated, the
// "..." row adds one more line.
func (m model) autocompleteHeight() int {
	if len(m.autocompleteMatches) == 0 {
		return 0
	}
	rows := len(m.autocompleteMatches)
	if rows > maxAutocompleteRows {
		rows = maxAutocompleteRows
	}
	height := rows + 2 // +2 for the border
	if len(m.autocompleteMatches) > maxAutocompleteRows {
		height++ // the "..." row
	}
	return height
}

// autocompletePanel renders the slash-command matches as a vertical
// dropup list with the selected match highlighted, or an empty string
// when there are no matches. It sits between the status bar and the
// textarea. The visible window follows autocompleteOffset so the
// selection stays on screen as it cycles.
func (m model) autocompletePanel() string {
	if len(m.autocompleteMatches) == 0 {
		return ""
	}
	var lines []string
	end := m.autocompleteOffset + maxAutocompleteRows
	if end > len(m.autocompleteMatches) {
		end = len(m.autocompleteMatches)
	}
	for i := m.autocompleteOffset; i < end; i++ {
		if i == m.autocompleteIndex {
			lines = append(lines, m.theme.Match.Render(m.autocompleteMatches[i]))
		} else {
			lines = append(lines, m.autocompleteMatches[i])
		}
	}
	if end < len(m.autocompleteMatches) {
		lines = append(lines, "...")
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.theme.CodeBorder.GetForeground()).
		Width(m.viewport.Width - 2).
		Render(strings.Join(lines, "\n"))
}

// truncate shortens s to maxLen characters, appending "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// wordWrap wraps s at width columns by splitting on spaces. Long words
// exceeding width are not broken. ANSI-aware: escape sequences are
// preserved in the output but excluded from width calculations.
func wordWrap(s string, width int) string {
	if width <= 0 {
		return s
	}
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		col := 0
		first := true
		for _, word := range strings.Fields(line) {
			visible := len(ansiRegex.ReplaceAllString(word, ""))
			if !first && col+1+visible > width {
				b.WriteString("\n")
				col = 0
				first = true
			}
			if !first {
				b.WriteString(" ")
				col++
			}
			b.WriteString(word)
			col += visible
			first = false
		}
		b.WriteString("\n")
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// renderHelp returns the /help text. Commands are laid out as a
// two-column table with widths computed from the data, matching the
// /sessions and /tree tables. Extension-provided commands are
// appended after built-in commands.
func (m model) renderHelp() string {
	builtin := [][2]string{
		{"/exit", "quit"},
		{"/new [--ephemeral]", "start a new conversation (--ephemeral: nothing persisted)"},
		{"/sessions", "list saved sessions"},
		{"/resume <#|id|label>", "resume a session (line # from /sessions, id, or label)"},
		{"/branch [id]", "branch a new session from the current (or given) one"},
		{"/label [text]", "set a label on the current session (no text clears it)"},
		{"/tree", "show the session tree"},
		{"/models", "list available models from the current provider"},
		{"/copy", "copy the last message to clipboard"},
		{"/export [path]", "export session messages to JSONL (default: <session_id>.jsonl)"},
		{"/search <query>", "search session messages (full-text)"},
		{"/insights [days]", "show cross-session usage analytics (default: 30 days)"},
		{"/thinking [level]", "set thinking level (none, off, on, minimal, low, medium, high, extra high, max, ultra; no arg cycles)"},
		{"/tools [on|off|auto|list]", "tool results: expanded / collapsed / auto, or list all tools"},
		{"/extensions", "list loaded extensions"},
		{"/theme [name]", "switch theme (dark, light, auto; no arg lists all)"},
		{"/help", "show this help"},
	}
	rows := builtin
	if m.extensions != nil {
		for _, c := range m.extensions.Commands {
			rows = append(rows, [2]string{c.Name, c.Description})
		}
	}
	maxCmd := len("COMMAND")
	for _, r := range rows {
		if len(r[0]) > maxCmd {
			maxCmd = len(r[0])
		}
	}
	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString(m.theme.Info.Render("[omega chat]"))
	sb.WriteString("\n")
	sb.WriteString("  type a message and press enter to send\n")
	sb.WriteString("  ctrl+j inserts a newline (multi-line input)\n")
	sb.WriteString("\n")
	header := fmt.Sprintf("  %-*s  %s", maxCmd, "COMMAND", "DESCRIPTION")
	sb.WriteString(m.theme.Info.Render(header))
	sb.WriteString("\n")
	for _, r := range rows {
		fmt.Fprintf(&sb, "  %-*s  %s\n", maxCmd, r[0], r[1])
	}
	return sb.String()
}
