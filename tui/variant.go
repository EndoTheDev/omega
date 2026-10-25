package tui

// Variant navigation (t_7337870a): the terminal face of the message
// tree backend (t_17365247). /variant flips the active sibling variant
// of an assistant run, /fork pins the historical point the next
// message continues from, /retry re-runs the last user turn as a
// sibling variant. Selects persist as core.VariantSelect bookkeeping
// entries - the same store surface the http channel's
// /sessions/{id}/variants writes - so the TUI and the webui share one
// branch state per session.

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/EndoTheDev/omega/core"
	tea "github.com/charmbracelet/bubbletea"
)

// pathMarkers returns variant markers ("[2/3]") keyed by the view
// path index of each variant head, for path entries below upto. Only
// branch points carrying 2+ siblings produce markers.
func pathMarkers(view core.ActiveView, upto int) map[int]string {
	if len(view.Branches) == 0 || upto <= 0 {
		return nil
	}
	idx := make(map[string]int, upto)
	for i, sm := range view.Path {
		if i >= upto {
			break
		}
		idx[core.MsgID(sm.ID)] = i
	}
	markers := map[int]string{}
	for _, b := range view.Branches {
		if b.Count < 2 {
			continue
		}
		for i, head := range b.Siblings {
			if at, ok := idx[head]; ok {
				markers[at] = fmt.Sprintf("[%d/%d]", i+1, b.Count)
			}
		}
	}
	if len(markers) == 0 {
		return nil
	}
	return markers
}

// branchPointsOnPath filters a view's branch points to those whose
// parent message sits on the active path - /variant only speaks about
// branch points the user can actually see in the transcript.
func branchPointsOnPath(view core.ActiveView) []core.BranchInfo {
	onPath := make(map[string]bool, len(view.Path))
	for _, sm := range view.Path {
		onPath[core.MsgID(sm.ID)] = true
	}
	var out []core.BranchInfo
	for _, b := range view.Branches {
		if onPath[b.ParentID] {
			out = append(out, b)
		}
	}
	return out
}

// markersForHistory returns path markers for m.history when it
// exactly matches the loaded view's path (index-aligned); nil
// otherwise, so a drifted history can never mislabel messages.
func (m model) markersForHistory() map[int]string {
	if len(m.history) == 0 || len(m.view.Path) != len(m.history) {
		return nil
	}
	return pathMarkers(m.view, len(m.history))
}

// rowMarkers returns variant markers keyed by row index in the chat
// payload: the path-index-keyed message markers shift onto rows through
// the deterministic chatRowsOf alignment (chat_view.go). nil unless the
// history exactly matches the loaded view path (index-aligned) - a
// drifted history can never mislabel rows, the same guard
// markersForHistory enforced on the transcript renderer.
func (m model) rowMarkers() map[int]string {
	if len(m.history) == 0 || len(m.view.Path) != len(m.history) {
		return nil
	}
	byMsg := pathMarkers(m.view, len(m.history))
	if byMsg == nil {
		return nil
	}
	offs := chatRowOffsets(m.history)
	out := map[int]string{}
	for i, mk := range byMsg {
		if offs[i] >= 0 {
			out[offs[i]] = mk
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// variantGuards reports why the variant commands cannot run right
// now, or "" when they can.
func (m model) variantGuards() string {
	if m.store == nil {
		return "no store available"
	}
	if m.ephemeral {
		return "no sessions in ephemeral mode"
	}
	if m.sessionID == "" {
		return "no active session"
	}
	return ""
}

// loadSessionView fetches the active view of the current session.
func (m model) loadSessionView() (core.ActiveView, error) {
	return m.store.GetActiveView(context.Background(), m.sessionID)
}

// reloadView re-renders the transcript from a freshly derived view:
// the active path becomes history and transcript, with variant
// markers. Shared by /variant select, /fork, and /retry.
func (m *model) reloadView(view core.ActiveView) {
	msgs := make([]core.Message, 0, len(view.Path))
	for _, sm := range view.Path {
		msgs = append(msgs, sm.Message)
	}
	m.history = msgs
	m.view = view
	m.notices = nil
	m.chatCanvas.setConversation(m.sessionID, msgs)
}

// handleVariant implements /variant: no branch state argument lists
// the branch points on the active path (active/total per point);
// next/prev/<n> flip the LAST branch point and persist the selection.
func (m model) handleVariant(args []string) (tea.Model, tea.Cmd) {
	if why := m.variantGuards(); why != "" {
		m.err = why
		return m, nil
	}
	if m.busy {
		m.err = "a run is in flight"
		m.refresh()
		return m, nil
	}
	view, err := m.loadSessionView()
	if err != nil {
		m.err = "variants: " + err.Error()
		m.refresh()
		return m, nil
	}
	branches := branchPointsOnPath(view)
	if len(branches) == 0 {
		m.addNotice("\n" + m.theme.Info.Render("[no variants in this session]") + "\n")
		m.refresh()
		return m, nil
	}
	// No argument: list the branch state per branch point.
	if len(args) == 0 {
		var sb strings.Builder
		sb.WriteString("\n")
		for _, b := range branches {
			fmt.Fprintf(&sb, "  #%s  variant %d/%d\n", b.ParentID, b.ActiveIndex+1, b.Count)
		}
		m.addNotice(m.theme.Info.Render(sb.String()))
		m.refresh()
		return m, nil
	}

	// Navigation operates on the last branch point on the path.
	last := branches[len(branches)-1]
	if last.Count < 2 {
		m.err = "no variants at branch #" + last.ParentID
		m.refresh()
		return m, nil
	}
	which := -1
	switch args[len(args)-1] {
	case "next":
		which = (last.ActiveIndex + 1) % last.Count
	case "prev":
		which = (last.ActiveIndex - 1 + last.Count) % last.Count
	default:
		n, err := strconv.Atoi(args[len(args)-1])
		if err != nil || n < 1 || n > last.Count {
			m.err = fmt.Sprintf("usage: /variant [<n>|prev|next] (1-%d at branch #%s)", last.Count, last.ParentID)
			m.refresh()
			return m, nil
		}
		which = n - 1
	}
	return m.selectVariant(view, last.ParentID, last.Siblings[which], which+1, last.Count)
}

// selectVariant persists an core.VariantSelect for branch and reloads
// the active path. Selecting the already-active variant writes
// nothing (mirrors the http channel), it only re-renders.
func (m model) selectVariant(view core.ActiveView, branch, child string, shown, total int) (tea.Model, tea.Cmd) {
	if view.ActiveHeads[branch] == child {
		m.addNotice("\n" + m.theme.Info.Render(fmt.Sprintf("[variant %d/%d at #%s already active]", shown, total, branch)) + "\n")
		m.refresh()
		return m, nil
	}
	ctx := context.Background()
	if err := m.store.AppendMessage(ctx, m.sessionID, core.NewVariantSelect(branch, child)); err != nil {
		m.storeErr = "select variant: " + err.Error()
		m.refresh()
		return m, nil
	}
	m.storeErr = ""
	updated, err := m.loadSessionView()
	if err != nil {
		m.err = "variants: " + err.Error()
		m.refresh()
		return m, nil
	}
	m.reloadView(updated)
	m.forkParent = "" // the pinned point may no longer be on the path
	m.addNotice("\n" + m.theme.Info.Render(fmt.Sprintf("[variant %d/%d at #%s]", shown, total, branch)) + "\n")
	m.refresh()
	return m, nil
}

// handleFork implements /fork: no argument lists the active path
// numbered (1-based, same numbers /fork <n> takes); <n> pins that
// path point so the next send continues from it - the message chains
// onto the fork target's row id (withParentNode at submit).
func (m model) handleFork(args []string) (tea.Model, tea.Cmd) {
	if why := m.variantGuards(); why != "" {
		m.err = why
		return m, nil
	}
	if m.busy {
		m.err = "a run is in flight"
		m.refresh()
		return m, nil
	}
	view, err := m.loadSessionView()
	if err != nil {
		m.err = "fork: " + err.Error()
		m.refresh()
		return m, nil
	}
	if len(args) == 0 {
		var sb strings.Builder
		sb.WriteString("\n")
		for i, sm := range view.Path {
			role := core.MessageRole(sm.Message)
			text := ""
			switch msg := sm.Message.(type) {
			case core.User:
				text = msg.Content
			case core.Assistant:
				text = msg.Content
			case core.ToolResult:
				text = msg.Content
			}
			fmt.Fprintf(&sb, "  %d  %-9s  %s\n", i+1, role, truncate(wordWrap(text, m.viewport.Width), 60))
		}
		m.addNotice(m.theme.Info.Render(sb.String()))
		m.refresh()
		return m, nil
	}
	n, err := strconv.Atoi(args[0])
	if err != nil || n < 1 || n > len(view.Path) {
		m.err = fmt.Sprintf("usage: /fork <path #> (1-%d; no arg lists the path)", len(view.Path))
		m.refresh()
		return m, nil
	}
	at := n - 1
	target := view.Path[at]
	m.reloadViewAt(view, at)
	m.forkParent = core.MsgID(target.ID)
	m.addNotice("\n" + m.theme.Info.Render("[fork: next message continues from #"+
		core.MsgID(target.ID)+"]") + "\n")
	m.refresh()
	return m, nil
}

// reloadViewAt renders the transcript truncated at path index at
// (inclusive) - the forked perspective. The dropped turns stay in the
// store; they come back whenever a variant selection or fork makes
// them part of the active path again.
func (m *model) reloadViewAt(view core.ActiveView, at int) {
	msgs := make([]core.Message, 0, at+1)
	for _, sm := range view.Path[:at+1] {
		msgs = append(msgs, sm.Message)
	}
	m.history = msgs
	m.view = view
	m.notices = nil
	m.chatCanvas.setConversation(m.sessionID, msgs)
}

// handleRetry implements /retry: the active path's last user turn
// re-runs as a sibling variant (t_17365247 semantics, mirroring the
// http channel's retry). The in-memory history trims to that user
// turn, the run head pins its parent to the user's row id
// (retryParent + handleEvent), and the transcript re-renders the
// trimmed path so the fresh response is the only one on screen.
func (m model) handleRetry() (tea.Model, tea.Cmd) {
	if m.busy {
		m.err = "a run is in flight"
		m.refresh()
		return m, nil
	}
	if why := m.variantGuards(); why != "" {
		m.err = why
		return m, nil
	}
	view, err := m.loadSessionView()
	if err != nil {
		m.err = "retry: " + err.Error()
		m.refresh()
		return m, nil
	}
	end := -1
	for i := len(view.Path) - 1; i >= 0; i-- {
		if _, ok := view.Path[i].Message.(core.User); ok {
			end = i
			break
		}
	}
	if end == -1 {
		m.err = "nothing to retry: no user message in this session"
		m.refresh()
		return m, nil
	}
	branch := core.MsgID(view.Path[end].ID)
	// The fresh response becomes active by default (last sibling in
	// row order), unless a /variant selection still pins this branch -
	// a dangling select entry re-derives the default, so clear it.
	times, err := m.store.GetMessagesWithTimes(context.Background(), m.sessionID)
	if err == nil {
		for _, sm := range times {
			if vs, ok := sm.Message.(core.VariantSelect); ok && vs.Branch == branch {
				_ = m.store.AppendMessage(context.Background(), m.sessionID, core.NewVariantSelect(branch, ""))
				break
			}
		}
	}
	heads := view.SiblingHeads[branch]
	m.reloadViewAt(view, end)
	m.forkParent = ""
	m.retryParent = branch
	// The fresh response's head chains onto the branch-point user at
	// persist time too - the sink is built with this headParent
	// (t_17365247's sibling-variant hop, now loop-side).
	m.headParent = branch
	m.headPinned = false
	m.retryMarker = ""
	if n := len(heads) + 1; n >= 2 {
		m.retryMarker = fmt.Sprintf("[%d/%d]", n, n)
	}
	m.busy = true
	m.err = ""
	m.resetRenderCache()
	m.chatCanvas.setConversation(m.sessionID, m.history)
	m.startRun()
	return m, tea.Batch(m.drainEvents(), m.titleCmd())
}
