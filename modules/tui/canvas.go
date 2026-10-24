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
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	xansi "github.com/charmbracelet/x/ansi"
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

// The TUI toast (arc 4b): a panel of kind "toast" is a finished-thing
// notice, not a persistent panel surface. The payload row is decoded
// through its wire shape (the shell holds no module types - layer law)
// and renders one status-bar line for the toast lifetime.

// toastLifetime is how long a toast rides the status bar. failed
// verdicts stay longer - the same asymmetry the webui's stack keeps.
const tuiToastLifetime = 6 * time.Second
const tuiToastFailedLifetime = 12 * time.Second

// toastExpiredMsg clears the transient toast line.
type toastExpiredMsg struct{}

// toastLine renders one toast panel's payload as a status-bar line.
// The bool reports a failed verdict (the failed lifetime is longer).
// Empty payload or a toast-shaped payload that cannot decode returns ""
// and the delta is treated like any other panel delta.
func toastLine(p core.Panel) (string, bool) {
	if p.Kind != "toast" || p.Payload == nil {
		return "", false
	}
	b, err := json.Marshal(p.Payload)
	if err != nil {
		return "", false
	}
	var r struct {
		Title   string `json:"title"`
		Verdict string `json:"verdict"`
	}
	if err := json.Unmarshal(b, &r); err != nil || r.Title == "" {
		return "", false
	}
	return r.Title, r.Verdict == "failed"
}

// toastExpireCmd arms the lifetime tick; the re-arm lives on the
// canvasDeltaMsg case (a newer toast resets the clock by re-arming).
func (m model) toastExpireCmd(failed bool) tea.Cmd {
	lifetime := tuiToastLifetime
	if failed {
		lifetime = tuiToastFailedLifetime
	}
	return tea.Tick(lifetime, func(time.Time) tea.Msg { return toastExpiredMsg{} })
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
// title + body view + numbered actions, live against the runtime.
// While open it takes every key (the module screen shape). The 'd'
// key flips the raw JSON view - the debug shape behind a keybinding
// (t_c5b98609 fix 1), rows stay the default body.
type canvasScreen struct {
	c        core.TUIConsole
	snap     func() []core.Panel                // live snapshot; nil = no canvas
	dispatch func(core.ActionCall) (any, error) // the one action pipe; nil = no canvas
	notice   string                             // empty-state line when snap is nil
	result   string                             // last invocation outcome, pinned under the header
	failed   bool                               // result is an error line
	raw      bool                               // 'd': the JSON dump view
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
	case "d":
		s.raw = !s.raw
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
	if s.raw {
		body = renderPanelsDebug(width, panels)
	}
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
	sb.WriteString(s.c.Style("status")("1-9 invoke · j/k scroll · d raw · q/esc close"))
	return sb.String()
}

// The list wire shape (t_c5b98609, extended t_985670d3): list-kind
// panels decode their payload through the generic row vocabulary - the
// shell holds no module types (layer law), the JSON wire shape is the
// shared seam, the same decode the toast line rides. The vocabulary
// carries both id dialects the wire has produced (arc 4b numbers, arc 5
// "t_xxx" strings) and a status field beside the verdict, so a row
// renders whatever it declared.
type wireRowAction struct {
	Label string `json:"label"`
}

// wireID is a row id that decodes as a JSON number or string - a row
// is rendered through whichever dialect it carries.
type wireID struct {
	num int64
	str string
}

func (w *wireID) UnmarshalJSON(b []byte) error {
	var n int64
	if err := json.Unmarshal(b, &n); err == nil {
		w.num, w.str = n, ""
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		w.num, w.str = 0, s
		return nil
	}
	return fmt.Errorf("id: number or string required, got %s", b)
}

// set reports the id carries anything renderable.
func (w wireID) set() bool { return w.num != 0 || w.str != "" }

// idLine renders the id the way its dialect reads: a bare int gains
// the # marker; a string id (arc 5 "t_xxx") rides verbatim.
func (w wireID) idLine() string {
	if w.num != 0 {
		return fmt.Sprintf(" #%d", w.num)
	}
	if w.str != "" {
		return " " + w.str
	}
	return ""
}

type wireRow struct {
	ID        wireID         `json:"id"`
	Kind      string         `json:"kind"`
	SourceID  string         `json:"source_id"`
	Title     string         `json:"title"`
	Status    string         `json:"status"`
	Verdict   string         `json:"verdict"`
	CreatedAt string         `json:"created_at"`
	Read      *bool          `json:"read"`
	RowAction *wireRowAction `json:"action,omitempty"`
}

// rowRead is the read state driving the row marker. A declared read
// field wins (arc 4b always sends one); a payload with no read field
// derives it from the status - settled rows read, live rows unread
// (the t_985670d3 fallback: the wire vocabulary already ties bindings
// to unsettled rows, the marker follows the same line).
func (r wireRow) rowRead() bool {
	if r.Read != nil {
		return *r.Read
	}
	return r.Status == "done" || r.Status == "archived"
}

// renderable reports a decoded row can produce a real line. A payload
// that decodes but shares no vocabulary field with the wire (the
// kanban board switcher: name/current/active, no title, no id, no
// status) is not a list body - the JSON dump stays its default view.
func renderable(r wireRow) bool {
	return r.Title != "" || r.SourceID != "" || r.ID.set() || r.Verdict != ""
}

type wireListPayload struct {
	Unread int       `json:"unread"`
	Total  int       `json:"total"`
	Rows   []wireRow `json:"rows"`
}

// wireListView is a decoded list payload: the rows plus the badge
// counts the header may show. ok reports the payload decoded into
// renderable rows.
type wireListView struct {
	Rows   []wireRow
	Unread int
	Total  int
	OK     bool
}

// decodeListRows decodes a list-kind payload through the wire shape.
// OK reports the panel is a list: its body becomes one line per row
// instead of the JSON dump. Struct payloads and JSON maps decode
// alike (the in-process spine carries the module's typed payload, the
// same field tags the wire carries). A payload that decodes to rows
// with no renderable field falls back to the dump, not to empty lines.
func decodeListRows(kind string, payload any) wireListView {
	if kind != "list" || payload == nil {
		return wireListView{}
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return wireListView{}
	}
	var lp wireListPayload
	if err := json.Unmarshal(b, &lp); err != nil {
		return wireListView{}
	}
	for _, r := range lp.Rows {
		if renderable(r) {
			return wireListView{Rows: lp.Rows, Unread: lp.Unread, Total: lp.Total, OK: true}
		}
	}
	return wireListView{}
}

// decodeToastRow decodes a toast-kind payload: one wire row, the same
// shape as a list row. The bool reports the panel is a toast.
func decodeToastRow(kind string, payload any) (wireRow, bool) {
	if kind != "toast" || payload == nil {
		return wireRow{}, false
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return wireRow{}, false
	}
	var r wireRow
	if err := json.Unmarshal(b, &r); err != nil || r.Title == "" {
		return wireRow{}, false
	}
	return r, true
}

// truncateLine clips one line to a display width with an ellipsis -
// no pane content may run past the pane edge (t_c5b98609 fix 2).
func truncateLine(s string, width int) string {
	if width <= 0 {
		return s
	}
	return xansi.Truncate(s, width, "…")
}

// truncateLines clips every line of a multi-line body to width.
func truncateLines(s string, width int) string {
	if width <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = truncateLine(l, width)
	}
	return strings.Join(lines, "\n")
}

// listRowLine renders one list row as a single line: the read-state
// marker, the title, the record id (number or string dialect), the
// kind or status field, and the row's binding hint when it is unread
// (t_c5b98609 fix 1, t_985670d3: key fields only, whatever decoded).
func listRowLine(r wireRow) string {
	read := r.rowRead()
	marker := "○ " // read
	if !read {
		marker = "● "
	}
	title := r.Title
	if title == "" {
		title = r.SourceID
	}
	line := marker + title
	line += r.ID.idLine()
	if r.Kind != "" {
		line += " · " + r.Kind
	}
	field := r.Status
	if field == "" {
		field = r.Verdict
	}
	if field != "" {
		line += " · " + field
	}
	if !read && r.RowAction != nil && r.RowAction.Label != "" {
		line += " · " + r.RowAction.Label
	}
	return line
}

// renderCanvasPanels renders the body: per panel a title, the body
// view - list-kind panels as readable rows (t_c5b98609 fix 1),
// everything else as the plain payload view - and the numbered
// actions with module-qualified ids. Every line truncates to width.
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
		view := decodeListRows(p.Kind, p.Payload)
		toast, isToast := decodeToastRow(p.Kind, p.Payload)
		if view.OK && view.Unread > 0 {
			head += fmt.Sprintf("  · %d unread", view.Unread)
		} else if view.OK && view.Total > 0 {
			head += fmt.Sprintf("  · %d total", view.Total)
		} else if p.Kind != "" {
			head += "  [" + p.Kind + "]"
		}
		sb.WriteString(truncateLine(head, width))
		sb.WriteString("\n")
		switch {
		case view.OK:
			if len(view.Rows) == 0 {
				sb.WriteString("(empty)")
				sb.WriteString("\n")
			}
			for _, r := range view.Rows {
				sb.WriteString(truncateLine(listRowLine(r), width))
				sb.WriteString("\n")
			}
		case isToast:
			sb.WriteString(truncateLine(listRowLine(toast), width))
			sb.WriteString("\n")
		default:
			if body := payloadView(p.Payload); body != "" {
				sb.WriteString(truncateLines(body, width))
				sb.WriteString("\n")
			}
		}
		for _, a := range p.Actions {
			n++
			sb.WriteString(truncateLine(fmt.Sprintf("[%d] %s", n, a.Label), width))
			sb.WriteString("\n")
			sb.WriteString(truncateLine("    "+a.Module+"."+a.ID, width))
			sb.WriteString("\n")
		}
	}
	if n == 0 && len(panels) == 0 {
		return "(no panels mounted)"
	}
	return strings.TrimRight(sb.String(), "\n")
}

// renderPanelsDebug is the raw view behind the 'd' key: every payload
// as indented JSON - a debug shape behind a keybinding, never the
// default body (t_c5b98609 fix 1).
func renderPanelsDebug(width int, panels []core.Panel) string {
	if len(panels) == 0 {
		return "(no panels mounted)"
	}
	var sb strings.Builder
	for _, p := range panels {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		head := p.Title
		if p.Kind != "" {
			head += "  [" + p.Kind + "]"
		}
		sb.WriteString(truncateLine(head, width))
		sb.WriteString("\n")
		if body := payloadView(p.Payload); body != "" {
			sb.WriteString(truncateLines(body, width))
			sb.WriteString("\n")
		}
	}
	return strings.TrimRight(sb.String(), "\n")
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
