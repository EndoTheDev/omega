package tui

// The TUI side of the canvas contract (arc 3d): the shell consumes the
// canvas runtime in-process. Panel presence comes from the mount
// registration, not any hardcoded list (settled Q5a); the load-time
// snapshot rides Canvas.Snapshot and the deltas ride the in-process
// subscription - the same spine, no HTTP (settled Q1). Rendering is
// the minimal v1 schema - title + payload view + action list - the
// same plain rendering the webui consumes; rich kinds arrive with
// real migrations (settled Q2b). Invoking an action goes through the
// one action pipe with a module-qualified id, the identical call the
// webui's /ui/actions route makes (settled Q4b).

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// canvasFeed is the shell's in-process canvas subscription: the canvas
// runtime built over the mounted PanelProviders plus the delta channel
// its emitter writes into. Deltas carry the full panel descriptor
// (arc 3a law), so the channel alone keeps the shell live; the screen
// re-snapshots from the runtime on each View.
type canvasFeed struct {
	canvas *agent.Canvas
	events chan core.Panel
	err    error // registration wiring bug; canvas stays nil
}

// openCanvasFeed builds the canvas over the mounted providers and
// wires its delta emitter into the channel. The send blocks on a
// buffered channel: the shell's drain cmd always re-arms, so a full
// buffer means the loop is dead - a module goroutine pausing beats
// silently losing panel state.
func openCanvasFeed(pctx *core.Context) *canvasFeed {
	f := &canvasFeed{events: make(chan core.Panel, 16)}
	if pctx == nil {
		return f
	}
	canvas, err := agent.NewCanvas(pctx, func(ev core.Event) {
		if d, ok := ev.(core.ViewModelDelta); ok {
			f.events <- d.Panel
		}
	})
	if err != nil {
		f.err = err
		return f
	}
	f.canvas = canvas
	return f
}

// hasPanels reports the feed carries a usable canvas (and so deltas
// can actually arrive - the drain cmd blocks otherwise).
func (f *canvasFeed) hasPanels() bool {
	return f != nil && f.canvas != nil
}

// canvasDeltaMsg is one canvas delta delivered into the model loop.
// The panel rides for assertion; rendering re-snapshots the runtime,
// so the value itself is not stored anywhere.
type canvasDeltaMsg struct{ panel core.Panel }

// waitForCanvas returns the cmd that blocks on the next delta. The
// re-arm lives on the canvasDeltaMsg Update case.
func (m model) waitForCanvas() tea.Cmd {
	return func() tea.Msg { return canvasDeltaMsg{panel: <-m.canvasFeed.events} }
}

// canvasRow is one numbered key-binding entry: panel + action in flat
// snapshot order. The screen renders these numbered and binds the
// digit keys to the same list.
type canvasRow struct {
	panel  core.Panel
	action core.PanelAction
}

// canvasActionRows flattens the snapshot's actions in render order -
// snapshot order (mount order, panel ids sorted), actions per panel
// in declared order. Both View and Update walk this same list.
func canvasActionRows(panels []core.Panel) []canvasRow {
	var rows []canvasRow
	for _, p := range panels {
		for _, a := range p.Actions {
			rows = append(rows, canvasRow{panel: p, action: a})
		}
	}
	return rows
}

// canvasScreen is the open /panels view: every mounted canvas panel,
// title + payload view + numbered actions, live against the runtime.
// While open it takes every key (the module screen shape).
type canvasScreen struct {
	c        core.TUIConsole
	snap     func() []core.Panel              // live snapshot; nil = no canvas
	dispatch func(core.ActionCall) (any, error) // the one action pipe; nil = no canvas
	notice   string                           // empty-state line when snap is nil
	result   string                           // last invocation outcome, pinned under the header
	failed   bool                             // result is an error line
	closed   bool
	vp       viewport.Model
}

var _ core.TUIScreen = (*canvasScreen)(nil)

// openCanvasScreen opens the /panels view over the model's canvas
// feed. A missing or broken canvas renders its notice instead of
// panels - panel presence stays a property of the registration.
func openCanvasScreen(m model, c core.TUIConsole) *canvasScreen {
	s := &canvasScreen{c: c, vp: viewport.New(80, 20)}
	switch {
	case m.canvasFeed == nil:
		s.notice = "no panels mounted"
	case m.canvasFeed.err != nil:
		s.notice = "canvas unavailable: " + m.canvasFeed.err.Error()
	case m.canvasFeed.canvas != nil:
		s.snap = m.canvasFeed.canvas.Snapshot
		s.dispatch = m.canvasFeed.canvas.Dispatch
	default:
		s.notice = "no panels mounted"
	}
	return s
}

// Closed reports the view is closed (its q/esc path).
func (s *canvasScreen) Closed() bool { return s.closed }

// snapshot returns the current registration, or nil without a canvas.
func (s *canvasScreen) snapshot() []core.Panel {
	if s.snap == nil {
		return nil
	}
	return s.snap()
}

// Update consumes one shell message while the view is open.
func (s *canvasScreen) Update(msg any, c core.TUIConsole) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return
	}
	switch key.String() {
	case "q", "esc":
		s.closed = true
	case "up", "k":
		s.vp.LineUp(1)
	case "down", "j":
		s.vp.LineDown(1)
	case "pgup", "ctrl+u":
		s.vp.HalfPageUp()
	case "pgdown", "ctrl+d":
		s.vp.HalfPageDown()
	case "g", "home":
		s.vp.GotoTop()
	case "G", "end":
		s.vp.GotoBottom()
	default:
		s.invokeAction(key.String(), c)
	}
}

// invokeAction dispatches the digit-numbered action through the one
// action pipe - module plus action id, no args (v1 bindings carry no
// arguments). Route failures land in the result line and the status
// line; a handler's error surfaces verbatim.
func (s *canvasScreen) invokeAction(digits string, c core.TUIConsole) {
	if s.dispatch == nil {
		s.result = "no canvas mounted"
		s.failed = true
		return
	}
	n := 0
	for _, r := range digits {
		if r < '0' || r > '9' || len(digits) != 1 {
			return
		}
		n = int(r - '0')
	}
	rows := canvasActionRows(s.snapshot())
	if n < 1 || n > len(rows) {
		if len(digits) == 1 {
			c.Error(fmt.Sprintf("/panels: no bound action %d", n))
		}
		return
	}
	row := rows[n-1]
	res, err := s.dispatch(core.ActionCall{Module: row.action.Module, Action: row.action.ID})
	if err != nil {
		s.result = fmt.Sprintf("%s.%s: %v", row.action.Module, row.action.ID, err)
		s.failed = true
		c.Error("/panels: " + s.result)
		return
	}
	s.result = fmt.Sprintf("%s.%s: ok", row.action.Module, row.action.ID)
	if body := payloadView(res); body != "" {
		s.result += " - " + body
	}
	s.failed = false
	c.ClearError()
}

// canvasScreenHeight is rows spent outside the body viewport: header,
// trailing newline and the hint row (the shell's status line rides
// moduleScreenView). The result line adds one when present.
func resultRows(s *canvasScreen) int {
	if s.result == "" {
		return 0
	}
	return 1
}

func canvasScreenHeight(s *canvasScreen) int {
	return 3 + resultRows(s)
}

// View paints the full-screen panels view: header, optional result
// line, the panels body, and a key hint footer.
func (s *canvasScreen) View(width, height int) string {
	panels := s.snapshot()
	rows := canvasActionRows(panels)
	body := renderCanvasPanels(width, panels)
	if s.snap != nil && len(panels) == 0 {
		body = "(no panels mounted)"
	} else if s.snap == nil {
		body = "(" + s.notice + ")"
	}
	if height < 1 {
		height = 1
	}
	s.vp.Width = width
	s.vp.Height = max(1, height-canvasScreenHeight(s))
	s.vp.SetContent(body)

	var sb strings.Builder
	sb.WriteString(s.c.Style("info")(fmt.Sprintf("/panels  [%d actions]", len(rows))))
	if s.result != "" {
		sb.WriteString("\n")
		styled := s.c.Style("status")
		if s.failed {
			styled = s.c.Style("error")
		}
		sb.WriteString(styled(s.result))
	}
	sb.WriteString("\n")
	sb.WriteString(s.vp.View())
	sb.WriteString("\n")
	sb.WriteString(s.c.Style("status")("1-9 invoke · j/k scroll · q/esc close"))
	return sb.String()
}

// renderCanvasPanels renders the body: per panel a title, the payload
// view (plain v1: string payloads as text, everything else as JSON),
// and the numbered actions with module-qualified ids.
func renderCanvasPanels(width int, panels []core.Panel) string {
	if width <= 0 {
		width = 80
	}
	var sb strings.Builder
	n := 0
	for _, p := range panels {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		head := p.Title
		if head == "" {
			head = p.ID
		}
		sb.WriteString(head)
		if p.Kind != "" {
			sb.WriteString("  [" + p.Kind + "]")
		}
		sb.WriteString("\n")
		if body := payloadView(p.Payload); body != "" {
			sb.WriteString(body)
			sb.WriteString("\n")
		}
		for _, a := range p.Actions {
			n++
			sb.WriteString(fmt.Sprintf("[%d] %s", n, a.Label))
			sb.WriteString("\n")
			sb.WriteString("    " + a.Module + "." + a.ID)
			sb.WriteString("\n")
		}
	}
	if n == 0 && len(panels) == 0 {
		return "(no panels mounted)"
	}
	return sb.String()
}

// payloadView renders one payload or action result as plain text: a
// string rides verbatim, anything else renders as indented JSON.
func payloadView(payload any) string {
	switch v := payload.(type) {
	case nil:
		return ""
	case string:
		return v
	}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", payload)
	}
	return string(b)
}

// handlePanels opens the /panels screen - the shell-owned view over
// the canvas (panel presence is registration-driven, Q5a; the shell
// owns the canvas machinery the way it owns /extensions listing).
func (m model) handlePanels() (tea.Model, tea.Cmd) {
	out := m
	// Opening a full-screen view hands the keys over - the follow-loop
	// stop law a running module tick obeys too.
	out.tick = nil
	out.screen = openCanvasScreen(out, tuiConsole{m: &out})
	return out, nil
}