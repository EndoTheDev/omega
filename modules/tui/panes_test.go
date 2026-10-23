package tui

// Tests for the pane shell (arc 4a): the fixed left/main/right/bottom
// geography, the focus ring over a fake spine, the registration-driven
// panel pane, the run band's in-flight tool output, and the mouse
// routing. The legacy single-column fallback stays covered by the
// existing tui_test.go suites (they run at sub-minimum sizes).

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/EndoTheDev/omega/core"
)

// paneTestModel builds a shell sized into the pane layout with an
// optional panel context - the same construction the host runs.
func paneTestModel(t *testing.T, pctx *core.Context) model {
	t.Helper()
	m := NewModel(pctx, core.FrontendOptions{})
	outSize, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 42})
	return outSize.(model)
}

// paneSizedGeometry is the layout the 120x42 shell computes.
func paneSizedGeometry(t *testing.T, m model) paneGeometry {
	t.Helper()
	geo := m.geometry()
	if !geo.active {
		t.Fatal("120x42 fell below the pane minimum")
	}
	return geo
}

// asModel types the Update result back to the concrete model.
func asModel(t *testing.T, mo tea.Model) model {
	t.Helper()
	out, ok := mo.(model)
	if !ok {
		t.Fatalf("Update returned %T, want model", mo)
	}
	return out
}

// TestPaneGeometry covers the fixed v1 budget and the minimum-size
// fallback: sidebars left/right of the main column, run band below,
// column rows absorbing the remainder.
func TestPaneGeometry(t *testing.T) {
	geo := paneGeometryFor(120, 42)
	if !geo.active {
		t.Fatal("120x42 inactive")
	}
	if geo.colH != 42-runPaneRows-3 {
		t.Fatalf("colH = %d, want %d", geo.colH, 42-runPaneRows-3)
	}
	if geo.left != (paneRect{x: 0, y: 0, w: leftPaneWidth, h: geo.colH}) {
		t.Fatalf("left = %+v", geo.left)
	}
	if geo.main.w != 120-leftPaneWidth-rightPaneWidth || geo.main.x != leftPaneWidth {
		t.Fatalf("main = %+v", geo.main)
	}
	if geo.right.x != 120-rightPaneWidth || geo.right.w != rightPaneWidth {
		t.Fatalf("right = %+v", geo.right)
	}
	if geo.bottom != (paneRect{x: 0, y: geo.colH, w: 120, h: runPaneRows + 1}) {
		t.Fatalf("bottom = %+v", geo.bottom)
	}
	if paneGeometryFor(80, 24).active {
		t.Fatal("80x24 must stay on the legacy layout")
	}
	if paneGeometryFor(120, 20).active {
		t.Fatal("20 rows must stay on the legacy layout")
	}
	if paneGeometryFor(120, 42).paneAt(40, geo.colH+runPaneRows+2) != "" {
		t.Fatal("status rows must hit no pane")
	}
}

// TestPaneViewComposition covers the settled geography in the rendered
// output: all four pane headers present, the canvas panel title in the
// right pane's rows, the splashed chat in the main column, the run
// band idle line in the bottom rows.
func TestPaneViewComposition(t *testing.T) {
	m := paneTestModel(t, &core.Context{PanelProviders: []core.PanelProvider{kanbanFake()}})
	geo := paneSizedGeometry(t, m)
	view := ansiStrip(m.View())

	for _, want := range []string{"sessions", "chat", "panels", "run"} {
		if !strings.Contains(view, want) {
			t.Fatalf("pane header %q missing from view", want)
		}
	}
	if !strings.Contains(view, "Kanban board") {
		t.Fatal("registered panel title missing from the panels pane")
	}
	if strings.Contains(view, "(no panels mounted)") {
		t.Fatal("registration present but the pane rendered its notice")
	}
	if !strings.Contains(view, "enter to start") {
		t.Fatal("splash is not the chat pane content on an empty conversation")
	}
	// Region check: the run band's rows sit below the columns.
	lines := strings.Split(view, "\n")
	if idx := findLineIndex(lines, "(no active run)"); idx >= 0 && idx < geo.colH {
		t.Fatalf("run band content at row %d, want >= %d", idx, geo.colH)
	}
}

// findLineIndex returns the first row containing substr (stripped), or
// -1.
func findLineIndex(lines []string, substr string) int {
	for i, l := range lines {
		if strings.Contains(ansiStrip(l), substr) {
			return i
		}
	}
	return -1
}

// TestPaneFocusRing covers the focus grammar: tab walks left->main->right
// ->bottom, shift+tab walks back, digits jump, esc returns to chat.
func TestPaneFocusRing(t *testing.T) {
	m := paneTestModel(t, nil)
	if got := m.focusedPane(); got != paneMainID {
		t.Fatalf("default focus = %q, want main", got)
	}

	// From chat, tab hands over to the next ring pane; a second tab
	// walks on; shift+tab walks back.
	mo, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = asModel(t, mo)
	if m.focusedPane() != paneRightID {
		t.Fatalf("tab from main = %q, want right", m.focusedPane())
	}
	mo, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = asModel(t, mo)
	if m.focusedPane() != paneBottomID {
		t.Fatalf("tab from right = %q, want bottom", m.focusedPane())
	}
	mo, _ = m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = asModel(t, mo)
	if m.focusedPane() != paneRightID {
		t.Fatalf("shift+tab from bottom = %q, want right", m.focusedPane())
	}

	// Digits jump from a focused sidebar; the ring is spatial.
	mo, _ = m.Update(runeKey("1"))
	m = asModel(t, mo)
	if m.focusedPane() != paneLeftID {
		t.Fatalf("digit 1 = %q, want left", m.focusedPane())
	}
	mo, _ = m.Update(runeKey("4"))
	m = asModel(t, mo)
	if m.focusedPane() != paneBottomID {
		t.Fatalf("digit 4 = %q, want bottom", m.focusedPane())
	}

	// esc returns the keys to the chat pane.
	mo, _ = m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m = asModel(t, mo)
	if m.focusedPane() != paneMainID {
		t.Fatalf("esc = %q, want main", m.focusedPane())
	}
}

// TestPaneChatKeysStayChat covers the guards that keep typing typing:
// content in the textarea routes digits into the input, a busy run
// keeps the digits for the queued answer, and a tab mid-completion
// still completes instead of cycling.
func TestPaneChatKeysStayChat(t *testing.T) {
	m := paneTestModel(t, nil)

	// Textarea content: digits are typed input.
	m.textarea.SetValue("hi")
	mo, _ := m.Update(runeKey("4"))
	m = asModel(t, mo)
	if m.focusedPane() != paneMainID {
		t.Fatalf("digit with content jumped to %q", m.focusedPane())
	}
	if m.textarea.Value() != "hi4" {
		t.Fatalf("digit lost from input: %q", m.textarea.Value())
	}

	// Busy run: digits queue the answer (verdict cards), no jump.
	m2 := paneTestModel(t, nil)
	m2.busy = true
	mo, _ = m2.Update(runeKey("2"))
	m2 = asModel(t, mo)
	if m2.focusedPane() != paneMainID {
		t.Fatalf("busy digit jumped to %q", m2.focusedPane())
	}
	if m2.textarea.Value() != "2" {
		t.Fatalf("busy digit not typed: %q", m2.textarea.Value())
	}

	// Autocomplete up: tab completes rather than cycling.
	m3 := paneTestModel(t, nil)
	m3.autocompleteMatches = []string{"/theme"}
	m3.autocompleteIndex = 0
	mo, _ = m3.Update(tea.KeyMsg{Type: tea.KeyTab})
	m3 = asModel(t, mo)
	if m3.focusedPane() != paneMainID {
		t.Fatalf("tab mid-completion cycled to %q", m3.focusedPane())
	}
	if m3.textarea.Value() != "/theme" {
		t.Fatalf("tab did not complete: %q", m3.textarea.Value())
	}
}

// TestPaneSidebarKeyRouting covers the focused-non-chat grammar:
// scrolls move that pane's viewport, runes ghost nowhere, esc returns
// to chat.
func TestPaneSidebarKeyRouting(t *testing.T) {
	m := paneTestModel(t, nil)
	m.focus = paneBottomID
	m.bottomVP.Height = 4
	body := strings.Repeat("row\n", 20)
	m.bottomVP.SetContent(body)

	mo, _ := m.Update(runeKey("j"))
	m = asModel(t, mo)
	if m.bottomVP.YOffset != 1 {
		t.Fatalf("j scrolled bottom to %d, want 1", m.bottomVP.YOffset)
	}
	mo, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	m = asModel(t, mo)
	if m.bottomVP.YOffset != 0 {
		t.Fatalf("pgup scrolled bottom to %d, want 0", m.bottomVP.YOffset)
	}

	// Runes are ignored while a sidebar holds the focus.
	mo, _ = m.Update(runeKey("a"))
	m = asModel(t, mo)
	if m.textarea.Value() != "" {
		t.Fatalf("sidebar leaked into the textarea: %q", m.textarea.Value())
	}
	if m.focusedPane() != paneBottomID {
		t.Fatalf("rune stole the focus: %q", m.focusedPane())
	}
}

// TestPaneRunOutput covers the run band contract: in-flight
// tool/thinking output renders below the columns and out of the main
// viewport; the chat response stays in chat; the idle band dims.
func TestPaneRunOutput(t *testing.T) {
	m := paneTestModel(t, nil)
	geo := paneSizedGeometry(t, m)

	m.busy = true
	m.segments = []streamSegment{
		{kind: "tool", content: "[tool: files.read]\n  path: /tmp/x\n"},
		{kind: "thinking", content: "weighing options"},
		{kind: "response", content: "chat response"},
	}
	(&m).refresh()

	vp := ansiStrip(m.viewport.View())
	if strings.Contains(vp, "[tool: files.read]") || strings.Contains(vp, "weighing options") {
		t.Fatal("in-flight tool/thinking leaked into the main viewport")
	}
	if !strings.Contains(vp, "chat response") {
		t.Fatal("response left the chat viewport")
	}
	if !strings.Contains(ansiStrip(m.bottomPaneBody()), "[tool: files.read]") {
		t.Fatal("run band lost the tool output")
	}
	if !strings.Contains(ansiStrip(m.bottomPaneBody()), "[thinking]") {
		t.Fatal("run band lost the thinking output")
	}

	// The composed view puts it below the columns.
	lines := strings.Split(ansiStrip(m.View()), "\n")
	runRow := findLineIndex(lines, "[tool: files.read]")
	if runRow < geo.colH {
		t.Fatalf("tool output at row %d, want inside the run band (>= %d)", runRow, geo.colH)
	}

	// Idle: band dims, and after the fold the transcript keeps the
	// completed output (existing contract).
	m.busy = false
	m.segments = nil
	if !strings.Contains(ansiStrip(m.bottomPaneBody()), "(no active run)") {
		t.Fatal("idle band did not dim")
	}
}

// TestPaneMouseRouting covers click-to-focus over the fixed grid and
// wheel routing to the pane under the pointer; panes off, the legacy
// handler scrolls the main viewport.
func TestPaneMouseRouting(t *testing.T) {
	m := paneTestModel(t, nil)
	geo := paneSizedGeometry(t, m)

	mo, _ := m.Update(tea.MouseMsg{Type: tea.MouseLeft, X: 5, Y: 2})
	m = asModel(t, mo)
	if m.focusedPane() != paneLeftID {
		t.Fatalf("click in left = %q, want left", m.focusedPane())
	}
	mo, _ = m.Update(tea.MouseMsg{Type: tea.MouseLeft, X: geo.main.x + 5, Y: 2})
	m = asModel(t, mo)
	if m.focusedPane() != paneMainID {
		t.Fatalf("click in main = %q, want main", m.focusedPane())
	}
	mo, _ = m.Update(tea.MouseMsg{Type: tea.MouseLeft, X: 5, Y: geo.colH + 1})
	m = asModel(t, mo)
	if m.focusedPane() != paneBottomID {
		t.Fatalf("click in run band = %q, want bottom", m.focusedPane())
	}
	mo, _ = m.Update(tea.MouseMsg{Type: tea.MouseLeft, X: 120 - rightPaneWidth + 3, Y: 2})
	m = asModel(t, mo)
	if m.focusedPane() != paneRightID {
		t.Fatalf("click in right = %q, want right", m.focusedPane())
	}

	// Wheel over the right pane scrolls its viewport (content set like
	// View does, offset persists on the model).
	m.rightVP.Height = 6
	m.rightVP.SetContent(strings.Repeat("panel row\n", 30))
	m.focus = paneMainID
	mo, _ = m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown, X: 120 - rightPaneWidth + 3, Y: 2})
	m = asModel(t, mo)
	if m.rightVP.YOffset != 1 {
		t.Fatalf("wheel over right pane: YOffset = %d, want 1", m.rightVP.YOffset)
	}

	// Panes off: wheel keeps the legacy main-viewport behavior.
	legacy := NewModel(nil, core.FrontendOptions{})
	legacy.viewport.SetContent(strings.Repeat("x\n", 40))
	mo, _ = legacy.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown, X: 3, Y: 3})
	legacy = asModel(t, mo)
	if legacy.viewport.AtBottom() {
		t.Fatal("legacy wheel did not scroll the viewport")
	}
}

// TestPaneRightPaneRegistration covers Q5a in the pane: panel presence
// rides the canvas registration - a fresh update to the spine shows up
// on the next View with no shell-side list, and the plain v1 renderer
// is the same one /panels uses.
func TestPaneRightPaneRegistration(t *testing.T) {
	fake := kanbanFake()
	m := paneTestModel(t, &core.Context{PanelProviders: []core.PanelProvider{fake}})

	if !strings.Contains(ansiStrip(m.View()), "Kanban board") {
		t.Fatal("registered panel missing from the panels pane")
	}
	// Registration updates flow live: deltas ride the channel; the
	// pane re-snapshots per View.
	fake.panels[0].Title = "Board v2"
	if err := m.canvasFeed.canvas.Update("kanban", fake.panels[0]); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !strings.Contains(ansiStrip(m.View()), "Board v2") {
		t.Fatal("panel delta did not reach the panels pane view")
	}

	// No registration at all: the pane renders its notice, not a
	// hardcoded list.
	bare := paneTestModel(t, nil)
	if !strings.Contains(ansiStrip(bare.View()), "(no panels mounted)") {
		t.Fatal("providerless shell did not fold the pane to its notice")
	}
}

// TestLegacyViewFallback pins the single-column composition still
// stands below the pane minimums - the legacy shape is the
// small-terminal contract.
func TestLegacyViewFallback(t *testing.T) {
	m := NewModel(nil, core.FrontendOptions{})
	m.viewport.Width = 80
	m.screenHeight = 24 // termWidth stays 0: never saw a pane-sized resize
	view := ansiStrip(m.View())
	if strings.Contains(view, "panels: no") || strings.Contains(view, "(no active run)") {
		t.Fatal("pane chrome leaked into the legacy view")
	}
	if !strings.Contains(view, "Ω |") {
		t.Fatal("legacy view lost the status line")
	}
}