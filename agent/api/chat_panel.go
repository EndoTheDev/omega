package api

// The chat canvas surface: the conversation itself rides the canvas contract. The /chat
// REST + SSE API surface stays exactly where it is (settled Q6a); the canvas is
// additive for renderers. Push law (the kanban capsule's law): mutations through the
// module's own faces (the launchRun loop, the action pipe) push through the sink the
// canvas runtime installed at registration; a nil or failed push never fails a run -
// the store is already updated, the snapshot is the catch-up.

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/EndoTheDev/omega/core"
)

// Panel, action and cadence constants of the capsule.
const (
	ChatPanelID         = "chat"
	ChatActionStop      = "stop"
	ChatActionRetry     = "retry"
	ChatModuleNamespace = "http_channel"
	chatPanelRows       = 40 // newest rows in the payload; the store keeps the rest
	chatDefaultBatch    = 250 * time.Millisecond
)

// ChatCapsule is the chat module's canvas surface. The server pointer is late-bound:
// the Server does not exist at Mount time; the channel's Start sets it before newCanvas
// runs (a nil server means the panel degrades to its bare descriptor and the actions
// answer "chat channel not started" - the same degradation every optional seam
// applies).
type ChatCapsule struct {
	// srv is the late-bound server pin. Atomic because multi-bind starts channels in
	// goroutines (cmdServe) and each bind's wire re-pins the shared capsule while the
	// other mirrors may already serve requests.
	srv atomic.Pointer[Server]
	batch time.Duration

	mu sync.Mutex
	paintMu sync.Mutex
	update  func(core.Panel) error
	focusID string                   // the session the panel currently covers
	dirty   map[string]bool          // pending token batches, by session
	watch   map[string]chan struct{} // live batchers, by session
}

var _ core.PanelProvider = (*ChatCapsule)(nil)
var _ core.PanelUpdateSink = (*ChatCapsule)(nil)

func NewChatCapsule() *ChatCapsule {
	return &ChatCapsule{
		batch: chatDefaultBatch,
		dirty: map[string]bool{},
		watch: map[string]chan struct{}{},
	}
}

// registerAt appends the capsule to the context's panel providers once. Under the
// capsule mutex: multi-bind starts channels in goroutines, so two wires register
// concurrently and the append must serialize against the other bind's read.
func (c *ChatCapsule) registerAt(ctx *core.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, p := range ctx.PanelProviders {
		if p == core.PanelProvider(c) {
			return
		}
	}
	ctx.PanelProviders = append(ctx.PanelProviders, c)
}

// PinServer publishes the served server for readers that pull the pin.
func (c *ChatCapsule) PinServer(s *Server) { c.srv.Store(s) }

// SetPanelUpdate installs the canvas closure (core.PanelUpdateSink).
func (c *ChatCapsule) SetPanelUpdate(update func(core.Panel) error) {
	c.mu.Lock()
	c.update = update
	c.mu.Unlock()
}

// push routes one panel through the installed closure. A rejection IS loud: the only
// current rejection is an undeclared panel id (canvas runtime), a wiring bug the
// silent-swallow law must not hide.
func (c *ChatCapsule) push(p core.Panel) {
	c.mu.Lock()
	up := c.update
	c.mu.Unlock()
	if up == nil {
		return
	}
	if err := up(p); err != nil {
		if srv := c.srv.Load(); srv != nil && srv.Logger() != nil {
			srv.Logger().Errorf("chat panel push rejected (module %q): %v", ChatModuleNamespace, err)
		}
	}
}

// Push routes one panel push through the installed closure.
func (c *ChatCapsule) Push(p core.Panel) { c.push(p) }

// chatActions is the module's action vocabulary, declared on the panel at mount.
func chatActions() []core.PanelAction {
	return []core.PanelAction{
		{ID: ChatActionStop, Label: "stop", Module: ChatModuleNamespace},
		{ID: ChatActionRetry, Label: "retry", Module: ChatModuleNamespace},
	}
}

// Panels declares the snapshot: the single chat panel over the focus session. A server
// that never started (or home with no sessions yet) still declares the panel - the kind
// and actions stay on the wire, the payload stays empty until a conversation exists.
func (c *ChatCapsule) Panels() []core.Panel {
	if c.srv.Load() == nil {
		return []core.Panel{{ID: ChatPanelID, Title: "Chat", Kind: core.PanelKindChat, Actions: chatActions()}}
	}
	session, running := c.focus()
	return []core.Panel{c.buildPanel(session, running)}
}

// focus returns the panel's session ref and its live flag.
func (c *ChatCapsule) focus() (string, bool) {
	if id, ok := c.srv.Load().runs.firstActive(); ok {
		return id, true
	}
	if c.srv.Load().store == nil {
		return "", false
	}
	sessions, err := c.srv.Load().store.ListSessions(context.Background())
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

// buildPanel composes the panel descriptor for one session.
func (c *ChatCapsule) buildPanel(session string, running bool) core.Panel {
	return core.Panel{
		ID:      ChatPanelID,
		Title:   "Chat",
		Kind:    core.PanelKindChat,
		Payload: c.payloadFor(session, running),
		Actions: chatActions(),
	}
}

// payloadFor composes the view-model.
func (c *ChatCapsule) payloadFor(session string, running bool) core.ChatPayload {
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

// branchPath cuts the active path for a branch-pinned live run.
func branchPath(path []core.StoredMessage, branch string, floor int64) []core.StoredMessage {
	if branch == "" {
		return path
	}
	branchID, err := core.ParseMsgID(branch)
	if err != nil {
		return path
	}
	cut := -1
	for i, sm := range path {
		if sm.ID == branchID {
			cut = i
			break
		}
	}
	if cut == -1 {
		return path
	}
	out := make([]core.StoredMessage, 0, len(path))
	for i, sm := range path {
		if i <= cut || sm.ID > floor {
			out = append(out, sm)
		}
	}
	return out
}

// sessionItems collects the transcript items the panel renders for one session. A store
// fault answers nil: the surface degrades, the REST routes surface the fault. A
// branch-pinned run (retry, fork-of-user) cuts the old sibling's rows off the path
// first, so the open tail streams into the response's slot - one slot, N variants,
// never stacked.
func (c *ChatCapsule) sessionItems(session string, running bool) []transcriptItem {
	if isEphemeralID(session) {
		items := c.srv.Load().eph.snapshotItems(session)
		if running {
			if tail, builtAt, live := c.srv.Load().runs.tailSnapshot(session); live {
				items = append(items, freshenRunningCalls(tail, time.Since(builtAt))...)
			}
		}
		return items
	}
	if c.srv.Load().store == nil {
		return nil
	}
	view, err := c.srv.Load().store.GetActiveView(context.Background(), session)
	if err != nil {
		return nil
	}
	path := view.Path
	if running {
		if branch, floor, pinned := c.srv.Load().runs.runBranch(session); pinned {
			path = branchPath(path, branch, floor)
		}
	}
	persisted := buildTranscriptItems(path)
	if !running {
		return persisted
	}
	open, builtAt, live := c.srv.Load().runs.openSnapshot(session)
	if !live || len(open) == 0 {
		return persisted
	}
	return composeItems(persisted, freshenRunningCalls(open, time.Since(builtAt)))
}

// composeItems merges the persisted items and the open-record items into one. Reused
// tool call ids across a re-run (some providers mint colliding ids) can hide the
// re-run's rows until their content diverges - a v1 cosmetics race, the persisted truth
// is never wrong.
func composeItems(persisted, open []transcriptItem) []transcriptItem {
	seen := map[string]int{}
	// copied: the caller's slices stay untouched.
	out := make([]transcriptItem, len(persisted), len(persisted)+len(open))
	copy(out, persisted)
	for i, it := range out {
		if sig, ok := itemSignature(it); ok {
			seen[sig] = i
		}
	}
	for _, it := range open {
		sig, ok := itemSignature(it)
		if !ok {
			out = append(out, it)
			continue
		}
		if i, dup := seen[sig]; dup {
			if out[i].Kind == "tool_call" && it.StartedAt != "" {
				// the persisted twin adopts the open row's start stamp too: the row the renderer
				// keeps is the one that must carry the clock (round 3 - the stamp is the only
				// anchor that survives a cached payload)
				out[i].StartedAt = it.StartedAt
			}
			if it.Running && out[i].Kind == "tool_call" {
				out[i].Running = true
				out[i].ElapsedMs = it.ElapsedMs
			}
			continue
		}
		seen[sig] = len(out)
		out = append(out, it)
	}
	return out
}

// itemSignature is the compose dedupe key. user items are never open items, so they get
// no signature.
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

// beginRun is the run-start hook (launchRun).
func (c *ChatCapsule) beginRun(session string) {
	if c.srv.Load() == nil || session == "" {
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
		// A previous run's batcher should be gone.
	} else if c.batch > 0 {
		go c.batchLoop(watch, session)
	}
	c.push(c.buildPanel(session, true))
}

// batchLoop is the streaming cadence. Each tick holds paintMu across takeDirty + build
// + push: a tick that is past takeDirty must never push a running=true panel after
// endRun's exit flush. An OPEN TOOL CALL answers dirty on its own: a tool run emits no
// event between the call opening and its result, so the token batch never re-arms, the
// panel would keep the call-open anchor (elapsed_ms ~0) for the whole call, and a
// tab-switch remount would restart the visible meter at zero. That arm is
// lifecycle-gated (tick, watching): it must never paint for a session the panel no
// longer owns.
func (c *ChatCapsule) batchLoop(watch <-chan struct{}, session string) {
	ticker := time.NewTicker(c.batch)
	defer ticker.Stop()
	for {
		select {
		case <-watch:
			return
		case <-ticker.C:
			c.tick(session)
		}
	}
}

// tick is one cadence: rebuild and push when the session has a pending token batch.
// paintMu is held across the decision and the push, so a tick can never land a
// running=true panel after endRun's exit flush.
func (c *ChatCapsule) tick(session string) {
	c.paintMu.Lock()
	defer c.paintMu.Unlock()
	if c.takeDirty(session) || (c.watching(session) && c.hasRunningCall(session)) {
		c.push(c.buildPanel(session, true))
	}
}

// watching reports whether the session still owns the panel's batcher.
func (c *ChatCapsule) watching(session string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.focusID == session && c.watch[session] != nil
}

// hasRunningCall reports whether the session's live run carries a tool call.
func (c *ChatCapsule) hasRunningCall(session string) bool {
	srv := c.srv.Load()
	if srv == nil {
		return false
	}
	open, _, live := srv.runs.openSnapshot(session)
	if !live {
		return false
	}
	for _, it := range open {
		if it.Kind == "tool_call" && it.Running {
			return true
		}
	}
	return false
}

// takeDirty consumes the pending token batch for one session.
func (c *ChatCapsule) takeDirty(session string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.dirty[session] {
		return false
	}
	c.dirty[session] = false
	return true
}

// runEvent is the streaming hook (the launchRun loop, one call per agent event). A
// stale run's events never paint a newer run's panel.
func (c *ChatCapsule) runEvent(session string, event core.Event) {
	if c.srv.Load() == nil || session == "" {
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
func (c *ChatCapsule) dirtied(session string) {
	c.mu.Lock()
	c.dirty[session] = true
	c.mu.Unlock()
}

// flushNowUnlocked composes and pushes immediately.
func (c *ChatCapsule) flushNowUnlocked(session string) {
	c.mu.Lock()
	c.dirty[session] = false
	c.mu.Unlock()
	c.push(c.buildPanel(session, true))
}

// endRun is the run-exit hook.
func (c *ChatCapsule) endRun(session string) {
	if c.srv.Load() == nil || session == "" {
		return
	}
	c.mu.Lock()
	watch, known := c.watch[session]
	if known {
		delete(c.watch, session)
	}
	delete(c.dirty, session) // the entry leaves with the run - a stale batcher's takeDirty answers false
	focused := c.focusID == session
	c.mu.Unlock()
	if known {
		close(watch)
	}
	if !focused {
		return
	}
	// The exit flush is the session's last push and must land last: held across
	// build+push, a batcher tick blocked mid-build (or already past takeDirty) serializes
	// behind it or ahead of it, never a stale running=true panel after the run is gone.
	c.paintMu.Lock()
	defer c.paintMu.Unlock()
	c.push(c.buildPanel(session, false))
}

// HandleAction routes one action call through the pipe (core. Both actions take the
// session ref as their argument and both serve the same faces the REST routes serve -
// the pipe routes the existing chat verbs, no new behavior (render, do not redesign).
func (c *ChatCapsule) HandleAction(call core.ActionCall) (any, error) {
	if c.srv.Load() == nil {
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
	case ChatActionStop:
		// The same face POST /sessions/{id}/stop serves.
		ok, cancellable := c.srv.Load().stopRun(args.Session)
		if !ok {
			return nil, fmt.Errorf("session not active")
		}
		if !cancellable {
			return nil, fmt.Errorf("run not started")
		}
		return map[string]any{"session": args.Session, "status": "cancelling"}, nil
	case ChatActionRetry:
		req := chatRequest{SessionID: args.Session, Retry: true}
		cr, err := c.srv.Load().prepareRun(context.Background(), &req, nil)
		if err != nil {
			return nil, err
		}
		c.srv.Load().launchRun(context.Background(), cr.run, cr.prov, cr.messages, cr.tools, req)
		return map[string]any{"session": args.Session, "running": true}, nil
	default:
		return nil, fmt.Errorf("%w: %q", core.ErrUnknownAction, call.Action)
	}
}

// chatRows maps transcript items into the chat view-model rows.
func chatRows(items []transcriptItem) []core.ChatRow {
	rows := make([]core.ChatRow, 0, len(items))
	for _, it := range items {
		row := core.ChatRow{ID: it.ID, Time: it.Time, Status: core.ChatStatusComplete}
		if it.Running {
			row.Status = core.ChatStatusStreaming
		}
		switch it.Kind {
		case "user":
			row.Role = core.ChatRoleUser
			row.Blocks = []core.ChatBlock{{Type: core.ChatBlockText, Content: it.Content, Images: it.Images, Attachments: it.Attachments}}
		case "message":
			row.Role = core.ChatRoleAssistant
			row.Blocks = []core.ChatBlock{{Type: core.ChatBlockText, Content: it.Content, Running: it.Running, ElapsedMs: it.ElapsedMs, ResponseMs: it.ResponseMs}}
		case "thinking":
			row.Role = core.ChatRoleAssistant
			row.Blocks = []core.ChatBlock{{Type: core.ChatBlockThinking, Content: it.Content, Running: it.Running, ElapsedMs: it.ElapsedMs, ThinkingMs: it.ThinkingMs}}
		case "tool_call":
			row.Role = core.ChatRoleAssistant
			// an unanswered call rides its open wall clock: the reader derives the row's meter
			// from it, so a payload that outlives its build (the yield window pushes no delta)
			// can never hand a remounted pane a stale anchor
			row.Blocks = []core.ChatBlock{{Type: core.ChatBlockToolCall, Running: it.Running, ToolCall: it.ToolCall, ElapsedMs: it.ElapsedMs, StartedAt: it.StartedAt}}
		case "tool_result":
			row.Role = core.ChatRoleTool
			row.Blocks = []core.ChatBlock{{Type: core.ChatBlockToolResult, Content: it.Content, ToolCallID: it.ToolCallID, IsError: it.IsError, Gate: it.Gate, ElapsedMs: it.ElapsedMs, AnswerMs: it.AnswerMs}}
		default:
			continue
		}
		rows = append(rows, row)
	}
	return rows
}
