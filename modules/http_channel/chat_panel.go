package http_channel

// The chat canvas surface (chat-last 1/3, card t_ec45b670): the
// conversation itself rides the canvas contract. The capsule here is
// the PanelProvider http_channel mounts - one 'chat' panel whose
// view-model rows are the render-ready message vocabulary (core's
// ChatRow/ChatBlock, mapped one row per transcript item), snapshot at
// load and deltas after: run starts and structural stream events push
// immediately, streaming token chunks batch onto the cadence ticker.
// The /chat REST + SSE API surface stays exactly where it is (settled
// Q6a); the canvas is additive for renderers.
//
// Push law (the kanban capsule's law): mutations through the module's
// own faces (the launchRun loop, the action pipe) push through the
// sink the canvas runtime installed at registration; a nil or failed
// push never fails a run - the store is already updated, the snapshot
// is the catch-up. The panel covers ONE conversation: the focus
// session ref. A run's start takes the panel over (a newer run's
// begin, a stale run's events no longer paint it); with no live run
// the most recent conversation holds it.

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/EndoTheDev/omega/ai"
	"github.com/EndoTheDev/omega/core"
)

// Panel, action and cadence constants of the capsule. Module-qualified
// on the wire (core.PanelAction.Module carries "http_channel").
const (
	chatPanelID         = "chat"
	chatActionStop      = "stop"
	chatActionRetry     = "retry"
	chatModuleNamespace = "http_channel"
	chatPanelRows       = 40 // newest rows in the payload; the store keeps the rest
	chatDefaultBatch    = 250 * time.Millisecond
)

// chatCapsule is the chat module's canvas surface: it is the
// PanelProvider the plugin appends to Context.PanelProviders during
// Mount and the core.PanelUpdateSink the canvas runtime wires at
// registration. The server pointer is late-bound: the Server does not
// exist at Mount time; the channel's Start sets it before newCanvas
// runs (a nil server means the panel degrades to its bare descriptor
// and the actions answer "chat channel not started" - the same
// degradation every optional seam applies).
type chatCapsule struct {
	srv *Server
	// batch is the streaming-token flush cadence. Zero disables
	// batching: every token event flushes (tests pin the batch law
	// with both settings).
	batch time.Duration

	mu      sync.Mutex
	update  func(core.Panel) error
	focusID string                   // the session the panel currently covers
	dirty   map[string]bool          // pending token batches, by session
	watch   map[string]chan struct{} // live batchers, by session
}

var _ core.PanelProvider = (*chatCapsule)(nil)
var _ core.PanelUpdateSink = (*chatCapsule)(nil)

func newChatCapsule() *chatCapsule {
	return &chatCapsule{
		batch: chatDefaultBatch,
		dirty: map[string]bool{},
		watch: map[string]chan struct{}{},
	}
}

// SetPanelUpdate installs the canvas closure (core.PanelUpdateSink).
// Re-registration (a second canvas over the same providers) re-installs
// it - last one wins, one frontend is live per process.
func (c *chatCapsule) SetPanelUpdate(update func(core.Panel) error) {
	c.mu.Lock()
	c.update = update
	c.mu.Unlock()
}

// push routes one panel through the installed closure. A missing sink
// or a failed push is not a run failure: the rows are already in the
// store, the snapshot is the catch-up.
func (c *chatCapsule) push(p core.Panel) {
	c.mu.Lock()
	up := c.update
	c.mu.Unlock()
	if up == nil {
		return
	}
	_ = up(p)
}

// chatActions is the module's action vocabulary, declared on the panel
// at mount - the canvas runtime routes by these ids.
func chatActions() []core.PanelAction {
	return []core.PanelAction{
		{ID: chatActionStop, Label: "stop", Module: chatModuleNamespace},
		{ID: chatActionRetry, Label: "retry", Module: chatModuleNamespace},
	}
}

// Panels declares the snapshot: the single chat panel over the focus
// session. A server that never started (or home with no sessions yet)
// still declares the panel - the kind and actions stay on the wire,
// the payload stays empty until a conversation exists.
func (c *chatCapsule) Panels() []core.Panel {
	if c.srv == nil {
		return []core.Panel{{ID: chatPanelID, Title: "Chat", Kind: core.PanelKindChat, Actions: chatActions()}}
	}
	session, running := c.focus()
	return []core.Panel{c.buildPanel(session, running)}
}

// focus returns the panel's session ref and its live flag: the session
// streaming right now (the runHub is the live activity's truth), else
// the most recent durable conversation (not running), else nothing.
// (The focusID mutation side of this is beginRun; the read is
// stateless over the hub + store.)
func (c *chatCapsule) focus() (string, bool) {
	if id, ok := c.srv.runs.firstActive(); ok {
		return id, true
	}
	if c.srv.store == nil {
		return "", false
	}
	sessions, err := c.srv.store.ListSessions(context.Background())
	if err != nil {
		return "", false
	}
	newest, when := "", ""
	for _, sess := range sessions {
		if sess.Archived {
			continue
		}
		if when == "" || sess.UpdatedAt > when {
			newest, when = sess.ID, sess.UpdatedAt
		}
	}
	return newest, false
}

// buildPanel composes the panel descriptor for one session: rows from
// the sessionItems composition, the live-run flag from the runHub.
func (c *chatCapsule) buildPanel(session string, running bool) core.Panel {
	return core.Panel{
		ID:      chatPanelID,
		Title:   "Chat",
		Kind:    core.PanelKindChat,
		Payload: c.payloadFor(session, running),
		Actions: chatActions(),
	}
}

// payloadFor composes the view-model: the session ref, the running
// flag, and the session's newest rows (capped to chatPanelRows - the
// store keeps the history, the panel is small by law).
func (c *chatCapsule) payloadFor(session string, running bool) core.ChatPayload {
	p := core.ChatPayload{Session: session, Running: running}
	if session == "" {
		return p
	}
	rows := chatRows(c.sessionItems(session, running))
	if len(rows) > chatPanelRows {
		rows = rows[len(rows)-chatPanelRows:]
	}
	p.Rows = rows
	return p
}

// sessionItems collects the transcript items the panel renders for one
// session. Durable sessions compose persisted history plus the live
// run's OPEN items (the runHub's open tail); store-less sessions
// (ephemeral) compose from the registry's items plus the run's full
// tail - nothing they own was ever persisted. A store fault answers
// nil: the surface degrades, the REST routes surface the fault.
// Tails compose only while the run streams: the final exit flush
// (running=false) renders the persisted truth alone.
func (c *chatCapsule) sessionItems(session string, running bool) []transcriptItem {
	if isEphemeralID(session) {
		items := c.srv.eph.snapshotItems(session)
		if running {
			if tail, live := c.srv.runs.tailItems(session); live {
				items = append(items, tail...)
			}
		}
		return items
	}
	if c.srv.store == nil {
		return nil
	}
	view, err := c.srv.store.GetActiveView(context.Background(), session)
	if err != nil {
		return nil
	}
	persisted := buildTranscriptItems(view.Path)
	if !running {
		return persisted
	}
	open, live := c.srv.runs.openItems(session)
	if !live || len(open) == 0 {
		return persisted
	}
	return composeItems(persisted, open)
}

// composeItems merges the persisted items and the open-record items
// into one wire-ordered item stream: persisted first, then the open
// items the store does not already carry. The overlap exists because
// the loop's TurnSink persists every closed segment the moment it
// closes while the accumulator only empties a record at the NEXT
// segment split - so an open record can carry items (thinking, the
// answer text, its calls, answered results) that are already persisted
// rows. Signature law: a text/thinking open item whose content matches
// any persisted content item of its kind is dropped, and a
// tool_call/tool_result open item whose call id matches a persisted
// one is dropped - the persisted row is the truth. A streaming prefix
// matches nothing persisted until the turn lands, so live tokens keep
// flowing; the next flush after persistence renders them once. Reused
// tool call ids across a re-run (some providers mint colliding ids)
// can hide the re-run's rows until their content diverges - a v1
// cosmetics race, the persisted truth is never wrong.
func composeItems(persisted, open []transcriptItem) []transcriptItem {
	seen := map[string]bool{}
	for _, it := range persisted {
		if sig, ok := itemSignature(it); ok {
			seen[sig] = true
		}
	}
	out := persisted
	for _, it := range open {
		if sig, ok := itemSignature(it); ok && seen[sig] {
			continue
		}
		out = append(out, it)
	}
	return out
}

// itemSignature is the compose dedupe key: content text for the
// assistant prose items, call id for the tool items. user items are
// never open items, so they get no signature.
func itemSignature(it transcriptItem) (string, bool) {
	switch it.Kind {
	case "message":
		return "message:" + it.Content, true
	case "thinking":
		return "thinking:" + it.Content, true
	case "tool_call":
		if it.ToolCall != nil {
			return "tool_call:" + it.ToolCall.ID, true
		}
		return "", false
	case "tool_result":
		return "tool_result:" + it.ToolCallID, true
	case "user":
		return "", false
	}
	return "", false
}

// beginRun is the run-start hook (launchRun): the panel takes over the
// session being streamed, running flips on and the first delta travels
// immediately. A run with no session id (the legacy store-less /chat
// path) owns no panel. The cadence ticker is this session's batcher:
// one per streaming session.
func (c *chatCapsule) beginRun(session string) {
	if c.srv == nil || session == "" {
		return
	}
	c.mu.Lock()
	c.focusID = session
	c.dirty[session] = false
	watch, live := c.watch[session]
	if watch == nil {
		watch = make(chan struct{})
		c.watch[session] = watch
	}
	c.mu.Unlock()
	if live {
		// A previous run's batcher should be gone (endRun cleaned up
		// or the 409 law would have refused the slot) - belt and
		// braces: reuse the watch, no second ticker.
	} else if c.batch > 0 {
		go c.batchLoop(watch, session)
	}
	c.push(c.buildPanel(session, true))
}

// batchLoop is the streaming cadence: while the run holds the panel,
// tick the cadence and push the rebuilt rows when token chunks marked
// them dirty. Exits when endRun closes the watch.
func (c *chatCapsule) batchLoop(watch <-chan struct{}, session string) {
	ticker := time.NewTicker(c.batch)
	defer ticker.Stop()
	for {
		select {
		case <-watch:
			return
		case <-ticker.C:
			if c.takeDirty(session) {
				c.push(c.buildPanel(session, true))
			}
		}
	}
}

// takeDirty consumes the pending token batch for one session.
func (c *chatCapsule) takeDirty(session string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.dirty[session] {
		return false
	}
	c.dirty[session] = false
	return true
}

// runEvent is the streaming hook (the launchRun loop, one call per
// agent event). Token chunks only mark the panel dirty - the cadence
// ticker (or the next structural event) carries them; structural
// events push immediately, because the row shape changed (a call
// opened, a result answered, the assistant message landed in the
// store). A stale run's events never paint a newer run's panel.
func (c *chatCapsule) runEvent(session string, event core.Event) {
	if c.srv == nil || session == "" {
		return
	}
	c.mu.Lock()
	focused := c.focusID == session
	c.mu.Unlock()
	if !focused {
		return
	}
	if c.batch == 0 {
		c.flushNowUnlocked(session)
		return
	}
	switch event := event.(type) {
	case core.StreamEvent:
		switch event.Event.(type) {
		case core.ThinkingChunk, core.ResponseChunk:
			c.dirtied(session)
		case core.ToolCallEvent, core.StreamEnd:
			c.flushNowUnlocked(session)
		}
	case core.ToolResultEvent, core.TurnStart, core.AssistantMessageEvent:
		c.flushNowUnlocked(session)
	}
}

// dirtied marks a token batch pending.
func (c *chatCapsule) dirtied(session string) {
	c.mu.Lock()
	c.dirty[session] = true
	c.mu.Unlock()
}

// flushNowUnlocked composes and pushes immediately ("now" refers to
// the caller's decision; the "unlocked" refers to batching, not to
// c.mu, which push and build take inside).
func (c *chatCapsule) flushNowUnlocked(session string) {
	c.mu.Lock()
	c.dirty[session] = false
	c.mu.Unlock()
	c.push(c.buildPanel(session, true))
}

// endRun is the run-exit hook (called after the run's persistence
// epilogue, while the hub still holds the slot): the final delta
// carries the rows in their completed state, running off. Only the
// focused session's exit paints it - the 409 law may already have
// registered a newer run for the session. The batcher exits here too.
func (c *chatCapsule) endRun(session string) {
	if c.srv == nil || session == "" {
		return
	}
	c.mu.Lock()
	watch, known := c.watch[session]
	if known {
		delete(c.watch, session)
	}
	c.dirty[session] = false
	focused := c.focusID == session
	c.mu.Unlock()
	if known {
		close(watch)
	}
	if !focused {
		return
	}
	c.push(c.buildPanel(session, false))
}

// HandleAction routes one action call through the pipe (core.
// PanelProvider): the runtime checked module + action id already.
// Both actions take the session ref as their argument and both serve
// the same faces the REST routes serve - the pipe routes the existing
// chat verbs, no new behavior (render, do not redesign).
func (c *chatCapsule) HandleAction(call core.ActionCall) (any, error) {
	if c.srv == nil {
		return nil, fmt.Errorf("chat channel not started")
	}
	var args struct {
		Session string `json:"session"`
	}
	if len(call.Args) > 0 {
		if err := json.Unmarshal(call.Args, &args); err != nil {
			return nil, fmt.Errorf("chat %s: %v", call.Action, err)
		}
	}
	if args.Session == "" {
		return nil, fmt.Errorf("chat %s: session is required", call.Action)
	}
	switch call.Action {
	case chatActionStop:
		// The same face POST /sessions/{id}/stop serves: cancel the
		// live run's context; the loop unwinds through its normal
		// path and the run's exit hook paints the completed state.
		ok, cancellable := c.srv.runs.stop(args.Session)
		if !ok {
			return nil, fmt.Errorf("session not active")
		}
		if !cancellable {
			return nil, fmt.Errorf("run not started")
		}
		return map[string]any{"session": args.Session, "status": "cancelling"}, nil
	case chatActionRetry:
		// The same face /chat's retry body serves: the replay of the
		// session's LAST user turn as a sibling variant, detached -
		// the panel's deltas and the reattach stream are the
		// surfaces; no response body to stream.
		req := chatRequest{SessionID: args.Session, Retry: true}
		cr, err := c.srv.prepareRun(context.Background(), &req, nil)
		if err != nil {
			return nil, err
		}
		c.srv.launchRun(context.Background(), cr.run, cr.prov, cr.messages, cr.tools, req)
		return map[string]any{"session": args.Session, "running": true}, nil
	default:
		return nil, fmt.Errorf("%w: %q", core.ErrUnknownAction, call.Action)
	}
}

// chatRows maps transcript items into the chat view-model rows - one
// row per item in v1. Pure.
func chatRows(items []transcriptItem) []core.ChatRow {
	rows := make([]core.ChatRow, 0, len(items))
	for _, it := range items {
		row := core.ChatRow{ID: it.ID, Status: core.ChatStatusComplete}
		if it.Running {
			row.Status = core.ChatStatusStreaming
		}
		switch it.Kind {
		case "user":
			row.Role = core.ChatRoleUser
			row.Blocks = []core.ChatBlock{{Type: core.ChatBlockText, Content: it.Content, Images: imagesOf(it.Images)}}
		case "message":
			row.Role = core.ChatRoleAssistant
			row.Blocks = []core.ChatBlock{{Type: core.ChatBlockText, Content: it.Content}}
		case "thinking":
			row.Role = core.ChatRoleAssistant
			row.Blocks = []core.ChatBlock{{Type: core.ChatBlockThinking, Content: it.Content}}
		case "tool_call":
			row.Role = core.ChatRoleAssistant
			row.Blocks = []core.ChatBlock{{Type: core.ChatBlockToolCall, Running: it.Running, ToolCall: it.ToolCall, ElapsedMs: it.ElapsedMs}}
		case "tool_result":
			row.Role = core.ChatRoleTool
			row.Blocks = []core.ChatBlock{{Type: core.ChatBlockToolResult, Content: it.Content, ToolCallID: it.ToolCallID, IsError: it.IsError, ElapsedMs: it.ElapsedMs}}
		default:
			continue
		}
		rows = append(rows, row)
	}
	return rows
}

// imagesOf copies ai.ImageContent into the core vocabulary. Pure.
func imagesOf(imgs []ai.ImageContent) []core.ImageContent {
	if len(imgs) == 0 {
		return nil
	}
	out := make([]core.ImageContent, len(imgs))
	for i, im := range imgs {
		out[i] = core.ImageContent{MediaType: im.MediaType, Base64: im.Base64}
	}
	return out
}
