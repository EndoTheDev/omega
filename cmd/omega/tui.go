package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/EndoTheDev/omega-dev/internal/agent"
	"github.com/EndoTheDev/omega-dev/internal/ai"
	"github.com/EndoTheDev/omega-dev/internal/gateway"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// Fixed layout: textarea starts at minTextareaHeight lines and grows up to
// maxTextareaHeight as the user types. The viewport fills the rest minus
// the status line.
const (
	minTextareaHeight = 1
	maxTextareaHeight = 8
	statusLines       = 1
)

// Styles for the TUI.
var (
	styleUser     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	styleThinking = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleTool     = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	styleInfo     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleStatus   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleError    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
)

// model is the Bubble Tea state for the chat TUI. It owns the message
// history, the streaming buffer, and the two widgets (viewport + textarea).
type model struct {
	textarea     textarea.Model
	viewport     viewport.Model
	history      []ai.Message // full conversation fed to the agent each turn
	transcript   string       // rendered content of completed exchanges
	buffer       string       // streaming response currently being received
	thinking     string       // streaming thinking, rendered with lipgloss
	providerType string
	modelName    string
	host         string
	apiKey       string
	compaction   *agent.CompactionConfig
	systemPrompt string
	busy         bool             // a run is in flight; input is ignored
	err          string           // last run error, shown in the status line
	cancel       context.CancelFunc // cancels the in-flight run; nil when idle
	events       <-chan agent.Event // run goroutine writes here; Update drains via cmd
	store        *gateway.Store
	sessionID    string // current session; "" until the first message creates one
	storeErr     string // store open/persistence error, shown in the status line
	promptHistory []string // previously submitted prompts, for Up/Down recall
	historyIndex   int      // position in promptHistory; 0 = empty/current input
}

// streamDoneMsg signals that the run goroutine has finished.
type streamDoneMsg struct{}

func runChat(pc gateway.ProviderConfig, compaction *agent.CompactionConfig, systemPrompt string, store *gateway.Store) error {
	m := newChatModel(pc.Type, pc.ModelName, pc.Host, pc.APIKey, compaction, systemPrompt, store)
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("chat: %w", err)
	}
	return nil
}

func newChatModel(providerType, modelName, host, apiKey string, compaction *agent.CompactionConfig, systemPrompt string, store *gateway.Store) model {
	ta := textarea.New()
	ta.Placeholder = "message (enter to send, ctrl+j for newline, /help for commands)"
	ta.SetHeight(minTextareaHeight)
	ta.ShowLineNumbers = false
	vp := viewport.New(80, 20)
	return model{
		textarea:     ta,
		viewport:     vp,
		providerType: providerType,
		modelName:    modelName,
		host:         host,
		apiKey:       apiKey,
		compaction:   compaction,
		systemPrompt: systemPrompt,
		store:        store,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.textarea.Focus(), tea.EnterAltScreen)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.textarea.SetWidth(msg.Width)
		m.resizeTextarea()
		vpHeight := msg.Height - m.textarea.Height() - statusLines
		if vpHeight < 1 {
			vpHeight = 1
		}
		m.viewport.Width = msg.Width
		m.viewport.Height = vpHeight
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
			}
			return m, nil
		}
		if msg.String() == "ctrl+j" {
			// Ctrl+J inserts a newline for multi-line input.
			var cmd tea.Cmd
			m.textarea, cmd = m.textarea.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'\n'}})
			m.resizeTextarea()
			return m, cmd
		}
		if msg.String() == "enter" { // Enter submits
			return m.submit()
		}
		if msg.String() == "esc" {
			m.err = ""
			m.refresh()
			return m, nil
		}
		// PgUp/PgDn/Up/Down scroll the viewport.
		if msg.String() == "pgup" || msg.String() == "pgdown" {
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
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
		// Re-issue the drain command so the next event is delivered.
		return m, m.drainEvents()

	case streamDoneMsg:
		m.busy = false
		m.cancel = nil
		m.refresh()
		return m, m.textarea.Focus()

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

	// Slash commands run locally and never hit the agent.
	if strings.HasPrefix(input, "/") {
		return m.handleCommand(input)
	}

	m.textarea.Blur()
	m.transcript += "\n" + styleUser.Render("> "+input) + "\n"
	m.history = append(m.history, ai.NewUser(input))
	m.busy = true
	m.buffer = ""
	m.thinking = ""

	// Persist the user message; auto-create a session on the first one.
	if m.store != nil {
		if m.sessionID == "" {
			id, err := newSessionID()
			if err != nil {
				m.storeErr = "session id: " + err.Error()
				m.busy = false
				m.refresh()
				return m, nil
			}
			if err := m.store.CreateSession(context.Background(), id); err != nil {
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
	providerType, modelName, host, apiKey := m.providerType, m.modelName, m.host, m.apiKey
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	provider, err := ai.NewProvider(providerType, modelName, host, apiKey)
	if err != nil {
		m.err = err.Error()
		m.busy = false
		m.refresh()
		return m, nil
	}
	ag := agent.NewAgent(provider, agent.NewRegistry(), 0)
	ag.SetCompaction(m.compaction)
	ag.SetSystemPrompt(m.systemPrompt)

	// The goroutine writes events to the channel; Update drains it via
	// drainEvents. The channel is a reference type, so it survives the
	// value copy Bubble Tea makes of the model. A fresh channel per run:
	// it is closed when the run ends, so reusing one across runs would
	// panic on the second write.
	m.events = ag.Run(ctx, m.history, nil)
	return m, m.drainEvents()
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

// handleEvent folds one agent event into the streaming buffer or transcript.
func (m *model) handleEvent(event agent.Event) {
	switch e := event.(type) {
	case agent.StreamEvent:
		switch chunk := e.Event.(type) {
		case ai.ResponseChunk:
			m.buffer += chunk.Content
		case ai.ThinkingChunk:
			m.thinking += chunk.Content
		case ai.ToolCallEvent:
			m.buffer += "\n" + styleTool.Render("[tool: "+chunk.ToolCall.Name+"]") + "\n"
			if len(chunk.ToolCall.Arguments) > 0 {
				for k, v := range chunk.ToolCall.Arguments {
					m.buffer += fmt.Sprintf("  %s: %v\n", k, v)
				}
			}
		case ai.StreamEnd:
			if chunk.Error != "" {
				m.buffer += "\n" + styleError.Render("error: "+chunk.Error) + "\n"
			}
		}
	case agent.AgentEnd:
		if e.Error != "" {
			m.err = e.Error
		}
		// Prepend thinking (lipgloss-styled) to the transcript, then
		// render the response through glamour for markdown styling.
		if m.thinking != "" {
			m.transcript += "\n" + styleThinking.Render("[thinking]") + "\n"
			m.transcript += styleThinking.Render(m.thinking) + "\n"
		}
		response := ai.NewAssistant(strings.TrimSuffix(m.buffer, "\n"))
		m.transcript += "\n" + renderAssistant(response.Content, m.viewport.Width) + "\n"
		m.history = append(m.history, response)
		m.buffer = ""
		m.thinking = ""
		// Persist the assistant response.
		if m.store != nil && m.sessionID != "" {
			if err := m.store.AppendMessage(context.Background(), m.sessionID, response); err != nil {
				m.storeErr = "save response: " + err.Error()
			} else {
				m.storeErr = ""
			}
		}
	}
}

// handleCommand executes a slash command and returns a follow-up command.
func (m model) handleCommand(input string) (tea.Model, tea.Cmd) {
	fields := strings.Fields(input)
	switch fields[0] {
	case "/exit":
		return m, tea.Quit
	case "/clear":
		// Clear in-memory history but keep the session; messages already
		// persisted stay in the store.
		m.history = nil
		m.transcript = ""
		m.buffer = ""
		m.thinking = ""
		m.err = ""
		m.refresh()
		return m, nil
	case "/sessions":
		return m.handleSessions()
	case "/resume":
		return m.handleResume(fields)
	case "/help":
		m.transcript += renderHelp()
		m.refresh()
		return m, nil
	case "/model":
		if len(fields) < 2 {
			m.err = "usage: /model <name>"
			return m, nil
		}
		m.modelName = fields[1]
		m.transcript += "\n" + styleInfo.Render("[model set to "+m.modelName+"]") + "\n"
		m.refresh()
		return m, nil
	case "/provider":
		if len(fields) < 2 {
			m.err = "usage: /provider <ollama|openai|anthropic>"
			return m, nil
		}
		name := fields[1]
		if _, err := ai.NewProvider(name, m.modelName, m.host, m.apiKey); err != nil {
			m.err = err.Error()
			m.refresh()
			return m, nil
		}
		m.providerType = name
		m.transcript += "\n" + styleInfo.Render("[provider set to "+m.providerType+"]") + "\n"
		m.refresh()
		return m, nil
	default:
		m.err = "unknown command: " + fields[0]
		return m, nil
	}
}

// handleSessions lists all sessions from the store.
func (m model) handleSessions() (tea.Model, tea.Cmd) {
	if m.store == nil {
		m.err = "no store available"
		return m, nil
	}
	sessions, err := m.store.ListSessions(context.Background())
	if err != nil {
		m.storeErr = "list sessions: " + err.Error()
		return m, nil
	}
	m.storeErr = ""
	if len(sessions) == 0 {
		m.transcript += "\n" + styleInfo.Render("[no sessions yet]") + "\n"
		m.refresh()
		return m, nil
	}
	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString(styleInfo.Render("[sessions]"))
	sb.WriteString("\n")
	for _, s := range sessions {
		count, _ := m.store.CountMessages(context.Background(), s.ID)
		fmt.Fprintf(&sb, "  %s  %d messages\n", s.ID, count)
	}
	m.transcript += sb.String()
	m.refresh()
	return m, nil
}

// handleResume loads a session from the store and displays its transcript.
func (m model) handleResume(fields []string) (tea.Model, tea.Cmd) {
	if m.store == nil {
		m.err = "no store available"
		return m, nil
	}
	if len(fields) < 2 {
		m.err = "usage: /resume <session-id>"
		return m, nil
	}
	id := fields[1]
	if _, err := m.store.GetSession(context.Background(), id); err != nil {
		m.storeErr = "resume: " + err.Error()
		return m, nil
	}
	messages, err := m.store.GetMessages(context.Background(), id)
	if err != nil {
		m.storeErr = "resume: " + err.Error()
		return m, nil
	}
	m.sessionID = id
	m.history = messages
	m.transcript = renderTranscript(messages, m.viewport.Width)
	m.buffer = ""
	m.thinking = ""
	m.err = ""
	m.storeErr = ""
	m.refresh()
	return m, nil
}

// newSessionID generates a random 8-character hex session identifier.
func newSessionID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// resizeTextarea adjusts the textarea height based on its current content,
// clamped between minTextareaHeight and maxTextareaHeight. It also resizes
// the viewport to fill the remaining space.
func (m *model) resizeTextarea() {
	lines := strings.Count(m.textarea.Value(), "\n") + 1
	if lines < minTextareaHeight {
		lines = minTextareaHeight
	}
	if lines > maxTextareaHeight {
		lines = maxTextareaHeight
	}
	m.textarea.SetHeight(lines)
	vpHeight := m.viewport.Height + m.textarea.Height() - lines
	if vpHeight < 1 {
		vpHeight = 1
	}
	m.viewport.Height = vpHeight
}

// renderTranscript renders a message history as the TUI transcript.
// Assistant messages are routed through renderAssistant so resumed
// sessions render markdown identically to the live AgentEnd path.
func renderTranscript(messages []ai.Message, width int) string {
	var sb strings.Builder
	for _, msg := range messages {
		switch m := msg.(type) {
		case ai.User:
			sb.WriteString("\n")
			sb.WriteString(styleUser.Render("> " + m.Content))
			sb.WriteString("\n")
		case ai.Assistant:
			sb.WriteString("\n")
			sb.WriteString(renderAssistant(m.Content, width))
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// refresh re-renders the viewport content from the transcript and buffer.
// The buffer is not wrapped during streaming — wrapping is deferred to
// AgentEnd so the UI thread stays responsive. The viewport handles long
// lines natively via horizontal scrolling.
func (m *model) refresh() {
	content := m.transcript + m.thinking + m.buffer
	m.viewport.SetContent(content)
	m.viewport.GotoBottom()
}

// renderAssistant renders markdown content through glamour to styled
// terminal output. Falls back to raw text if rendering fails.
func renderAssistant(content string, width int) string {
	if width <= 0 {
		width = 80
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
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

// View renders the full screen: viewport on top, textarea below, status last.
func (m model) View() string {
	var sb strings.Builder
	sb.WriteString(m.viewport.View())
	sb.WriteString("\n")
	sb.WriteString(m.textarea.View())
	sb.WriteString("\n")
	sb.WriteString(styleStatus.Render(m.statusLine()))
	return sb.String()
}

// statusLine returns the bottom status bar text.
func (m model) statusLine() string {
	state := "idle"
	if m.busy {
		state = "running"
	}
	sess := m.sessionID
	if sess == "" {
		sess = "none"
	} else if len(sess) > 8 {
		sess = sess[:8]
	}
	provider := m.providerType
	if provider == "" {
		provider = "ollama"
	}
	line := fmt.Sprintf("omega | %s/%s | sess: %s | %s | enter to send | /help", provider, m.modelName, sess, state)
	if m.err != "" {
		line += " | " + styleError.Render("error: "+m.err)
	}
	if m.storeErr != "" {
		line += " | " + styleError.Render("store: "+m.storeErr)
	}
	return line
}

// renderHelp returns the /help text.
func renderHelp() string {
	return "\n" + styleInfo.Render(`[omega chat]
  type a message and press enter to send
  ctrl+j inserts a newline (multi-line input)

  /exit          quit
  /clear         clear the conversation (keeps the session)
  /sessions      list saved sessions
  /resume <id>   resume a session
  /model <name>  switch the model
  /provider <n>  switch provider (ollama, openai, anthropic)
  /help          show this help
`) + "\n"
}
