package tui

// The pane shell (arc 4a, t_fb7e5998): the TUI canvas is left sidebar +
// main + right sidebar + bottom band, the same spatial layout the webui
// dock adopts (settled Q13a). The layout is native bubbletea code - no
// cross-frontend engine (Q5). Panel presence comes from the canvas
// registration through the model's canvasFeed (Q5a), rendered with the
// same plain v1 renderer the /panels screen uses (Q2b); the right pane
// is an inspector - actions still dispatch from /panels. The bottom
// band hosts the in-flight run's tool+thinking output; at AgentEnd the
// fold into the transcript stays exactly as it was. Fixed geometry v1
// (settled: resize arrives later) with a single-column fallback below
// the minimum size, so the legacy layout stays the small-terminal
// shape. Focus: tab/shift-tab cycles the ring, digits 1-4 jump, mouse
// click selects - from the chat pane digits stay typed input unless the
// textarea is empty and nothing else is underway.

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Pane ids, in ring order (tab cycles forward, shift+tab backward).
const (
	paneLeftID   = "left"
	paneMainID   = "main"
	paneRightID  = "right"
	paneBottomID = "bottom"
)

var paneRing = []string{paneLeftID, paneMainID, paneRightID, paneBottomID}

// paneDigits maps the jump keys onto the ring - the same spatial order.
var paneDigits = map[string]string{"1": paneLeftID, "2": paneMainID, "3": paneRightID, "4": paneBottomID}

// Fixed v1 geometry (settled Q: fixed; resize arrives later). The run
// band takes runPaneRows body rows plus its header. Below minPaneWidth
// or minPaneHeight the shell keeps the single-column legacy layout -
// sidebars need a terminal that can host them.
const (
	leftPaneWidth  = 24
	rightPaneWidth = 28
	runPaneRows    = 7
	minPaneWidth   = 100
	minPaneHeight  = 22
)

// paneRect is one pane's region in terminal coordinates.
type paneRect struct {
	x, y, w, h int
}

// contains reports whether the point (col, row) falls inside the rect.
func (r paneRect) contains(col, row int) bool {
	return col >= r.x && col < r.x+r.w && row >= r.y && row < r.y+r.h
}

// paneGeometry is the fixed v1 composition. Row budget, top to bottom:
// the three columns (colH rows), one separator row, the run band
// (header + runPaneRows), one separator row, the status line.
type paneGeometry struct {
	active bool
	colH   int
	left   paneRect
	main   paneRect
	right  paneRect
	bottom paneRect
}

// paneGeometryFor computes the layout for a terminal size. Inactive
// below the minimum size or when the main column would be unusably
// narrow - the caller falls back to the legacy single-column view.
func paneGeometryFor(width, height int) paneGeometry {
	if width < minPaneWidth || height < minPaneHeight {
		return paneGeometry{}
	}
	mainW := width - leftPaneWidth - rightPaneWidth
	if mainW < 30 {
		return paneGeometry{}
	}
	colH := height - (runPaneRows + 1) - 2 // run band + the two separator rows
	if colH < 6 {
		return paneGeometry{}
	}
	return paneGeometry{
		active: true,
		colH:   colH,
		left:   paneRect{x: 0, y: 0, w: leftPaneWidth, h: colH},
		main:   paneRect{x: leftPaneWidth, y: 0, w: mainW, h: colH},
		right:  paneRect{x: width - rightPaneWidth, y: 0, w: rightPaneWidth, h: colH},
		bottom: paneRect{x: 0, y: colH, w: width, h: runPaneRows + 1},
	}
}

// geometry is this model's layout, computed from the last known
// terminal size. A model that never saw a WindowSizeMsg (tests, and the
// splash frame before the first resize) stays on the legacy layout
// because termWidth is zero.
func (m model) geometry() paneGeometry {
	return paneGeometryFor(m.termWidth, m.screenHeight)
}

// focusedPane is the model's focus, normalized - the zero value means
// the chat pane (models built by hand set no focus).
func (m model) focusedPane() string {
	if m.focus == "" {
		return paneMainID
	}
	return m.focus
}

// nextPane / prevPane walk the ring one step, wrapping at the ends.
func nextPane(cur string) string {
	for i, id := range paneRing {
		if id == cur {
			return paneRing[(i+1)%len(paneRing)]
		}
	}
	return paneRing[0]
}

func prevPane(cur string) string {
	for i, id := range paneRing {
		if id == cur {
			return paneRing[(i+len(paneRing)-1)%len(paneRing)]
		}
	}
	return paneRing[0]
}

// jumpPane moves the focus. It carries no scroll or input side effects:
// the chat textarea keeps its value, sidebar scroll offsets stay put.
func (m model) jumpPane(id string) tea.Model {
	m.focus = id
	return m
}

// scrollViewport applies one scroll step to a viewport value.
func scrollViewport(vp viewport.Model, dir int, half bool) viewport.Model {
	switch {
	case dir < 0 && half:
		vp.HalfPageUp()
	case dir < 0:
		vp.LineUp(1)
	case half:
		vp.HalfPageDown()
	default:
		vp.LineDown(1)
	}
	return vp
}

// scrollPane scrolls the pane named by id. The chat pane routes through
// its viewport value like the legacy pgup/pgdn path does.
func (m model) scrollPane(id string, dir int, half bool) (tea.Model, tea.Cmd) {
	switch id {
	case paneLeftID:
		m.leftVP = scrollViewport(m.leftVP, dir, half)
	case paneRightID:
		m.rightVP = scrollViewport(m.rightVP, dir, half)
	case paneBottomID:
		m.bottomVP = scrollViewport(m.bottomVP, dir, half)
	case paneMainID:
		m.viewport = scrollViewport(m.viewport, dir, half)
	}
	return m, nil
}

// routePaneKey answers one key while a sidebar or the run band holds
// the focus. Digits jump, tab/shift-tab cycle, scrolls scroll, esc and
// enter hand the keys back to the chat pane. Everything else - runes
// included - is ignored: a focused sidebar never ghosts input into the
// chat textarea.
func (m model) routePaneKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "tab":
		return m.jumpPane(nextPane(m.focus)), nil
	case "shift+tab":
		return m.jumpPane(prevPane(m.focus)), nil
	case "esc", "enter":
		return m.jumpPane(paneMainID), m.textarea.Focus()
	case "up", "k":
		return m.scrollPane(m.focus, -1, false)
	case "down", "j":
		return m.scrollPane(m.focus, 1, false)
	case "pgup", "ctrl+u":
		return m.scrollPane(m.focus, -1, true)
	case "pgdown", "ctrl+d":
		return m.scrollPane(m.focus, 1, true)
	default:
		if id, ok := paneDigits[key]; ok {
			return m.jumpPane(id), nil
		}
		return m, nil
	}
}

// routeMouse handles one mouse event while panes are active: a press
// selects the pane under the pointer (the chat pane takes the textarea
// focus back), a wheel step scrolls that pane. The legacy handler owns
// the event when panes are inactive.
func (m model) routeMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	geo := m.geometry()
	if !geo.active {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}
	if msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown {
		id := geo.paneAt(msg.X, msg.Y)
		if id == "" {
			id = paneMainID
		}
		dir := 1
		if msg.Button == tea.MouseButtonWheelUp {
			dir = -1
		}
		return m.scrollPane(id, dir, false)
	}
	if msg.Type == tea.MouseLeft || (msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft) {
		switch id := geo.paneAt(msg.X, msg.Y); id {
		case paneLeftID, paneRightID, paneBottomID:
			return m.jumpPane(id), nil
		case paneMainID:
			return m.jumpPane(paneMainID), m.textarea.Focus()
		}
		return m, nil
	}
	return m, nil
}

// paneAt names the pane under the pointer, or "" for the gutter rows
// (status line and separators).
func (g paneGeometry) paneAt(col, row int) string {
	switch {
	case g.left.contains(col, row):
		return paneLeftID
	case g.main.contains(col, row):
		return paneMainID
	case g.right.contains(col, row):
		return paneRightID
	case g.bottom.contains(col, row):
		return paneBottomID
	}
	return ""
}

// paneHeader renders one pane's title row: a focus marker plus the
// label, match-highlighted when the pane holds the focus.
func (m model) paneHeader(id, label string) string {
	marker := "  "
	style := m.theme.Info
	if m.focusedPane() == id {
		marker = "▸ "
		style = m.theme.Match
	}
	return style.Render(marker + label)
}

// paneColumn renders one sidebar or the run band: header row plus a
// viewport of the body, exactly h rows tall and w columns wide. The
// viewport value is the caller's copy - scroll offsets persist on the
// model, content is re-set every frame the way the full-screen views
// re-snapshot.
func (m model) paneColumn(id, label string, r paneRect, vp viewport.Model, body string) string {
	vp.Width = r.w
	vp.Height = max(1, r.h-1)
	vp.SetContent(body)
	lines := []string{m.paneHeader(id, label)}
	lines = append(lines, vp.View())
	return strings.Join(lines, "\n")
}

// leftPaneContent builds the sessions/module list: the live session
// block, the session labels cached by the last /sessions, and the
// mounted module commands. Nothing here queries the store - the list
// refreshes when /sessions runs.
func (m model) leftPaneContent(w int) string {
	var sb strings.Builder
	sess := m.sessionID
	if m.ephemeral {
		sess = "ephemeral"
	} else if sess == "" {
		sess = "none"
	}
	if m.sessionLabel != "" {
		sess = m.sessionLabel
	}
	state := "idle"
	if m.busy {
		state = "running"
	} else if m.compacting {
		state = "compacting"
	}
	sb.WriteString("session: " + truncate(sess, w-9) + "\n")
	sb.WriteString("state:   " + state + "\n")
	sb.WriteString("model:   " + truncate(m.providerType+"/"+m.modelName, w-9) + "\n")
	if len(m.sessionList) > 0 {
		sb.WriteString("\nsessions\n")
		for _, s := range m.sessionList {
			sb.WriteString("  " + truncate(s.ID, w-4) + "\n")
		}
	}
	sb.WriteString("\nmodules\n")
	mods := moduleTable(m.extensions)
	if len(mods) == 0 {
		sb.WriteString("  (none mounted)\n")
	}
	for _, c := range mods {
		sb.WriteString("  " + truncate(c.Name, w-4) + "\n")
	}
	return sb.String()
}

// rightPaneContent builds the inspector body from the canvas
// registration (panel presence is registration-driven, Q5a). Same
// plain v1 rendering as the /panels screen; the numbered action rows
// are a view there too - dispatch stays on /panels in v1.
func (m model) rightPaneContent(w int) string {
	feed := m.canvasFeed
	switch {
	case feed == nil || !feed.hasPanels():
		return "(no panels mounted)"
	case feed.err != nil:
		return "canvas unavailable: " + feed.err.Error()
	}
	snap := feed.canvas.Snapshot()
	if len(snap) == 0 {
		return "(no panels mounted)"
	}
	return renderCanvasPanels(w, snap)
}

// bottomPaneBody builds the run band's body: the in-flight turn's
// tool+thinking output while the agent runs, a dim idle line when it
// does not. Completed folds land in the transcript as before - the
// band never re-renders them.
func (m model) bottomPaneBody() string {
	if !m.busy || len(m.segments) == 0 {
		return m.theme.Thinking.Render("(no active run)")
	}
	var sb strings.Builder
	for _, seg := range m.segments {
		switch seg.kind {
		case "thinking":
			sb.WriteString(m.theme.Thinking.Render("[thinking]") + "\n")
			sb.WriteString(m.theme.Thinking.Render(wordWrap(seg.content, m.termWidth)) + "\n")
		case "tool":
			sb.WriteString(seg.content)
		case "tool_result":
			sb.WriteString("\n" + m.theme.Tool.Render(seg.content) + "\n")
		case "tool_result_highlighted":
			sb.WriteString("\n" + seg.content + "\n")
		}
	}
	if sb.Len() == 0 {
		return m.theme.Thinking.Render("(no active run)")
	}
	return strings.TrimRight(sb.String(), "\n")
}

// paneView composes the full pane shell: the three columns, the run
// band, the status line. The chat column keeps the main viewport,
// autocomplete panel and textarea - the exact vertical rhythm the
// legacy layout had, wrapped in its column.
func (m model) paneView(geo paneGeometry) string {
	taH := m.textarea.Height()
	if taH < 1 {
		taH = 1
	}
	acH := m.autocompleteHeight()
	sep := 0
	if acH > 0 {
		sep = 1
	}
	vh := max(3, geo.colH-1-acH-sep-taH)

	m.viewport.Width = geo.main.w
	m.viewport.Height = vh
	if m.transcript == "" && len(m.segments) == 0 && len(m.history) == 0 {
		// Empty conversation: the splash IS the chat content. Rendered
		// against this frame's height; not persisted past the frame.
		m.viewport.SetContent(m.splashView())
	}

	mainLines := []string{m.paneHeader(paneMainID, "chat")}
	mainLines = append(mainLines, m.viewport.View())
	if panel := m.autocompletePanel(); panel != "" {
		mainLines = append(mainLines, panel)
	}
	mainLines = append(mainLines, m.textarea.View())
	mainCol := strings.Join(mainLines, "\n")

	leftCol := m.paneColumn(paneLeftID, "sessions", geo.left, m.leftVP, m.leftPaneContent(geo.left.w))
	rightCol := m.paneColumn(paneRightID, "panels", geo.right, m.rightVP, m.rightPaneContent(geo.right.w))
	bottom := m.paneColumn(paneBottomID, "run", geo.bottom, m.bottomVP, m.bottomPaneBody())

	var sb strings.Builder
	sb.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, leftCol, mainCol, rightCol))
	sb.WriteString("\n")
	sb.WriteString(bottom)
	sb.WriteString("\n")
	sb.WriteString(m.theme.Status.Render(m.statusLine()))
	return sb.String()
}
