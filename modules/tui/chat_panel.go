package tui

// The TUI chat capsule (chat-last 2b, the module-owned panel): the
// conversation itself rides the canvas contract as a panel of kind
// 'chat' in the module namespace "tui". The 2b design ruling settled
// ONE CHAT PANEL PER LIVE FRONTEND, MODULE-OWNED: in TUI-only mode the
// http channel never starts (its Start-time registration is dead), so
// the TUI registers its own capsule - gated on the frontend actually
// running, which registration-into-NewModel gives by construction (only
// the live Frontend.Run reaches it). One live chat panel per process
// follows: both modes' panels speak the same core.ChatPayload
// vocabulary, only the owner differs.
//
// The payload state is the capsule's own: completed rows composed from
// the conversation history (setConversation / commit), the in-flight
// turn's rows composed live from the agent events (stream*), and the
// run/stream-error line. Every mutation pushes the whole panel through
// the PanelUpdateSink the canvas runtime installed at registration -
// streaming rides deltas, not a second per-frontend channel. The run's
// stream events reach the capsule from the model's handleEvent; the
// capsule holds no model.
//
// Chat actions stop/retry are module-qualified ("tui"): stop wraps the
// native run cancel (self-contained, no model loop), retry drops a
// request the Update loop drains (the retry replays as a sibling
// variant - the same handleRetry the /retry command runs). The panels
// screen's digit dispatch and the chat pane's esc both route through
// the one pipe (canvas.Dispatch), module-qualified.

import (
	"fmt"
	"sync"

	"github.com/EndoTheDev/omega/core"
)

// Panel, action and vocabulary constants of the TUI chat capsule.
// Module-qualified on the wire (core.PanelAction.Module carries "tui").
const (
	chatPanelID     = "chat"
	chatActionStop  = "stop"
	chatActionRetry = "retry"
	tuiChatModule   = "tui"
	// chatPanelRows caps the payload to the newest rows - the same cap
	// the http channel's panel keeps; the conversation history stays
	// in the model, only the canvas surface is trimmed to stay small.
	chatPanelRows = 40
)

// chatCanvas is the TUI module's chat PanelProvider + PanelUpdateSink.
// Safe for concurrent use: the Update loop mutates it from handleEvent
// while the canvas runtime seeds and re-snapshots it.
type chatCanvas struct {
	mu      sync.Mutex
	session string         // the session the panel covers
	running bool           // a live run streams the panel's session
	rows    []core.ChatRow // completed rows (last setConversation/commit)
	live    []core.ChatRow // the in-flight turn's rows (open tail)
	legErr  string         // the committed run's stream error, "" = none
	marker  string         // the just-retried response's variant marker
	stopped func()         // the run's native cancel; nil when idle
	request chan string    // pipe-invoked actions that need the Update loop (retry)
	update  func(core.Panel) error
	trim    int // rows dropped by the payload cap at the last recompute
}

var _ core.PanelProvider = (*chatCanvas)(nil)
var _ core.PanelUpdateSink = (*chatCanvas)(nil)

func newChatCanvas() *chatCanvas {
	return &chatCanvas{request: make(chan string, 1)}
}

// registerChatCanvas creates the TUI chat capsule and registers it
// with the canvas seam - gated on the frontend actually running:
// only the live Frontend.Run path (NewModel) calls this. A re-run in
// the same process reuses the instance (one live panel, "tui"
// registered once).
func registerChatCanvas(pctx *core.Context) *chatCanvas {
	if pctx == nil {
		return nil
	}
	for _, p := range pctx.PanelProviders {
		if c, ok := p.(*chatCanvas); ok {
			return c
		}
	}
	c := newChatCanvas()
	pctx.PanelProviders = append(pctx.PanelProviders, c)
	return c
}

// SetPanelUpdate installs the canvas closure (core.PanelUpdateSink).
func (c *chatCanvas) SetPanelUpdate(update func(core.Panel) error) {
	c.mu.Lock()
	c.update = update
	c.mu.Unlock()
}

// push composes the current panel and routes it through the installed
// closure. The canvas runtime stores the state synchronously, so a
// viewer re-snapshotting this frame sees the push even before the
// delta lands on the spine.
func (c *chatCanvas) push() {
	c.mu.Lock()
	up := c.update
	panel := c.composeLocked()
	c.mu.Unlock()
	if up == nil {
		return
	}
	// A missing sink or a failed push is not a run failure: the model
	// holds the conversation, the next push catches up.
	_ = up(panel)
}

// composeLocked builds the panel descriptor (caller holds c.mu).
func (c *chatCanvas) composeLocked() core.Panel {
	rows := make([]core.ChatRow, 0, len(c.rows)+len(c.live))
	rows = append(rows, c.rows...)
	rows = append(rows, c.live...)
	if n := len(rows); n > chatPanelRows {
		rows = rows[n-chatPanelRows:]
	}
	return core.Panel{
		ID:      chatPanelID,
		Title:   "Chat",
		Kind:    core.PanelKindChat,
		Payload: core.ChatPayload{Session: c.session, Running: c.running, Rows: rows},
		Actions: c.actions(),
	}
}

// chatActions is the capsule's action vocabulary, module-qualified tui.
func (c *chatCanvas) actions() []core.PanelAction {
	return []core.PanelAction{
		{ID: chatActionStop, Label: "stop", Module: tuiChatModule},
		{ID: chatActionRetry, Label: "retry", Module: tuiChatModule},
	}
}

// Panel implements core.PanelProvider: the current chat panel.
func (c *chatCanvas) Panels() []core.Panel {
	c.mu.Lock()
	defer c.mu.Unlock()
	return []core.Panel{c.composeLocked()}
}

// RowCount reports the number of rows currently rendered (completed +
// live) - the notice anchor the shell's non-conversation lines pin to.
func (c *chatCanvas) RowCount() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.rows) + len(c.live)
}

// consumeTrim reports and clears the row count the last payload-cap
// trim dropped; the shell shifts its notice anchors by it.
func (c *chatCanvas) consumeTrim() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	trim := c.trim
	c.trim = 0
	c.mu.Unlock()
	return trim
}

// sessionOf reports the session the panel covers.
func (c *chatCanvas) sessionOf() string {
	if c == nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.session
}

// legErrOf reports the committed run's stream error.
func (c *chatCanvas) legErrOf() string {
	if c == nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.legErr
}

// markerOf reports the pinned variant marker of the last committed
// response ("" = none).
func (c *chatCanvas) markerOf() string {
	if c == nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.marker
}

// setConversation recomputes the completed rows from the full
// conversation and resets the session ref, the live tail, the error
// line and the marker. The shell call sites: send (the user row
// lands), resume/branch/variant/new - every point the conversation's
// renderable truth changes wholesale.
func (c *chatCanvas) setConversation(session string, msgs []core.Message) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.session = session
	c.rows, c.trim = trimRows(chatRowsOf(msgs))
	c.live = nil
	c.legErr = ""
	c.marker = ""
	c.mu.Unlock()
	c.push()
}

// commit paints the persisted truth after a run (AgentEnd): the rows
// recompute from the full history - the exit flush - while the marker
// and the stream error survive one render pass (the retried response's
// variant marker, the stream failure line).
func (c *chatCanvas) commit(msgs []core.Message, legErr string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.rows, c.trim = trimRows(chatRowsOf(msgs))
	c.live = nil
	c.legErr = legErr
	c.mu.Unlock()
	c.push()
}

// setSession updates the session ref alone (the auto-created session
// lands after the first send's rows already exist). No push: the
// payload shape changed by one field and the send's push carries it.
func (c *chatCanvas) setSession(session string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.session = session
	c.mu.Unlock()
}

// setRun marks the panel's session as streaming; stopped is the run's
// native cancel, wrapped by the stop action.
func (c *chatCanvas) setRun(session string, stopped func()) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.session = session
	c.running = true
	c.stopped = stopped
	c.legErr = ""
	c.marker = ""
	c.mu.Unlock()
	c.push()
}

// setIdle flips the running flag off (the run's exit already committed
// the persisted truth).
func (c *chatCanvas) setIdle() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.running = false
	c.stopped = nil
	c.mu.Unlock()
	c.push()
}

// setMarker pins a variant marker to the newest assistant text row
// (the just-retried response). Survives until the next run or
// conversation reload recomposes the rows.
func (c *chatCanvas) setMarker(marker string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.marker = marker
	c.mu.Unlock()
	c.push()
}

// trimRows applies the payload cap, returning the kept rows and the
// dropped count.
func trimRows(rows []core.ChatRow) ([]core.ChatRow, int) {
	if len(rows) > chatPanelRows {
		return rows[len(rows)-chatPanelRows:], len(rows) - chatPanelRows
	}
	return rows, 0
}

// streamThinking folds one thinking chunk into the live tail: the open
// thinking row keeps absorbing, a new one opens otherwise. Narrative
// order preserved - the rows append in emit order.
func (c *chatCanvas) streamThinking(content string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	if n := len(c.live); n > 0 && c.live[n-1].Blocks[0].Type == core.ChatBlockThinking {
		c.live[n-1].Blocks[0].Content += content
	} else {
		c.live = append(c.live, core.ChatRow{
			Role:   core.ChatRoleAssistant,
			Status: core.ChatStatusStreaming,
			Blocks: []core.ChatBlock{{Type: core.ChatBlockThinking, Content: content, Running: true}},
		})
	}
	c.mu.Unlock()
	c.push()
}

// streamResponse folds one response chunk into the live tail (the open
// text row, else a new one).
func (c *chatCanvas) streamResponse(content string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	if n := len(c.live); n > 0 && c.live[n-1].Blocks[0].Type == core.ChatBlockText {
		c.live[n-1].Blocks[0].Content += content
	} else {
		c.live = append(c.live, core.ChatRow{
			Role:   core.ChatRoleAssistant,
			Status: core.ChatStatusStreaming,
			Blocks: []core.ChatBlock{{Type: core.ChatBlockText, Content: content, Running: true}},
		})
	}
	c.mu.Unlock()
	c.push()
}

// streamCall opens one tool call row on the live tail (the open call
// stays running until a later delta or the exit flush closes it).
func (c *chatCanvas) streamCall(call core.ToolCall) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.live = append(c.live, core.ChatRow{
		Role:   core.ChatRoleAssistant,
		Status: core.ChatStatusStreaming,
		Blocks: []core.ChatBlock{{Type: core.ChatBlockToolCall, ToolCall: &call, Running: true}},
	})
	c.mu.Unlock()
	c.push()
}

// streamResult lands one tool result on the live tail as its own
// completed row - results render in narrative order between the call
// and the answer while the run streams (the persisted truth recomposes
// at the exit flush).
func (c *chatCanvas) streamResult(msg core.ToolResult) {
	if c == nil {
		return
	}
	c.mu.Lock()
	elapsed := int64(0)
	if msg.Timing != nil {
		elapsed = msg.Timing.ElapsedMs
	}
	c.live = append(c.live, core.ChatRow{
		Role:   core.ChatRoleTool,
		Status: core.ChatStatusComplete,
		Blocks: []core.ChatBlock{{
			Type:       core.ChatBlockToolResult,
			Content:    msg.Content,
			ToolCallID: msg.ToolCallID,
			IsError:    msg.IsError,
			ElapsedMs:  elapsed,
		}},
	})
	c.mu.Unlock()
	c.push()
}

// HandleAction routes one action call through the pipe
// (core.PanelProvider). stop wraps the native cancel - self-contained
// host state, the loop unwinds through its normal path; retry drops a
// request the Update loop drains (a model-loop replay is not a
// goroutine concern).
func (c *chatCanvas) HandleAction(call core.ActionCall) (any, error) {
	if c == nil {
		return nil, fmt.Errorf("chat: no canvas in this process")
	}
	switch call.Action {
	case chatActionStop:
		c.mu.Lock()
		stopped := c.stopped
		c.mu.Unlock()
		if stopped == nil {
			return nil, fmt.Errorf("chat %s: no run in flight", call.Action)
		}
		stopped()
		return map[string]any{"status": "cancelling"}, nil
	case chatActionRetry:
		select {
		case c.request <- chatActionRetry:
			return map[string]any{"status": "requested"}, nil
		default:
			return nil, fmt.Errorf("chat %s: retry already pending", call.Action)
		}
	default:
		return nil, fmt.Errorf("%w: %q", core.ErrUnknownAction, call.Action)
	}
}

// drainRequest pops the pending pipe action for the Update loop to
// apply ("" = none).
func (c *chatCanvas) drainRequest() string {
	if c == nil {
		return ""
	}
	select {
	case action := <-c.request:
		return action
	default:
		return ""
	}
}
