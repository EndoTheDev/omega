// The canvas contract (factory-flow ADR, arc 3a): view-model v1
// schema, the panel delta event kind, and the module-facing
// registration seam. One contract, two transports (settled Q1): the
// TUI consumes it in-process, http_channel serializes the same shapes
// over SSE/REST. The schema is minimal by law (settled Q2b) - the
// vocabulary grows one migration at a time, never designed on paper
// ahead of a consumer. Zero transport behavior lives here: snapshot
// assembly, delta emission and action routing are runtime concerns
// (agent.Canvas), serialization is each channel's concern.
package core

import (
	"encoding/json"
	"errors"
)

// PanelAction is one action a frontend can invoke on a panel through
// the action pipe. Module names the capsule that owns the handler -
// action ids are module-qualified (settled Q4b), so two panels may
// safely reuse the same id.
type PanelAction struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Module string `json:"module"`
}

// Panel is one view-model panel descriptor: the entire v1 schema
// (settled Q2b). Payload carries the panel's render data as JSON -
// the contract knows nothing about the payload's shape; each panel
// kind does. Actions lists what the frontend may invoke on the panel.
type Panel struct {
	ID      string        `json:"id"`
	Title   string        `json:"title"`
	Kind    string        `json:"kind"`
	Payload any           `json:"payload,omitempty"`
	Actions []PanelAction `json:"actions,omitempty"`
}

// ActionCall is one action-pipe call (settled Q4b): module + action id
// + arguments. The runtime routes it to the named capsule's registered
// handler - zero per-module code in any channel.
type ActionCall struct {
	Module string          `json:"module"`
	Action string          `json:"action"`
	Args   json.RawMessage `json:"args,omitempty"`
}

// The action pipe's 404 shapes. Route failures are wrapped with the
// offending module or action id and match these sentinels via
// errors.Is.
var (
	// ErrUnknownModule - the named capsule contributed no panel provider.
	ErrUnknownModule = errors.New("canvas: unknown module")
	// ErrUnknownAction - the module exists but registered no such action id.
	ErrUnknownAction = errors.New("canvas: unknown action")
)

// ViewModelDelta is the canvas event kind (settled Q3b): it carries
// one panel descriptor and rides the existing event spine like every
// other event. It is the after-load half of the snapshot + deltas
// pattern - GET the snapshot on load, deltas after.
type ViewModelDelta struct {
	Type  string `json:"type"`
	Panel Panel  `json:"panel"`
}

func (ViewModelDelta) isEvent() {}

// PanelUpdateSink is the optional capability of a PanelProvider whose
// panel state changes outside the action pipe - state that turns on a
// module-internal event (an emit, a poll) rather than a frontend call.
// The canvas runtime installs the update closure when it registers the
// provider: the closure routes the panel into the provider's own module
// namespace, so a push updates the snapshot and emits a ViewModelDelta
// on the same spine an action-pipe refresh would. Re-registration (a
// second canvas built over the same providers) re-installs the closure -
// last one wins, one frontend is live per process by the exclusive
// frontend seam.
type PanelUpdateSink interface {
	SetPanelUpdate(update func(panel Panel) error)
}

// The chat kind vocabulary (chat-last 1/3, card t_ec45b670): the
// conversation itself as a canvas surface. Panel kind 'chat' is the
// first NEW kind since the canvas contract - grown by a real migration
// (the http channel's chat runs), never on paper ahead of a consumer.
// Both frontends render chat from these types (card 2a webui, 2b TUI),
// so the vocabulary lives here beside Panel - not in one module's
// namespace, where the import law would strand the other frontend.
// Zero behavior: the row composition is each module's concern.

// PanelKindChat is the chat panel kind.
const PanelKindChat = "chat"

// ChatRow statuses: streaming while any block of the row is still in
// flight, complete otherwise.
const (
	ChatStatusStreaming = "streaming"
	ChatStatusComplete  = "complete"
)

// Roles of a chat row: the user bubble, an assistant bubble (its
// blocks split thinking/text/calls), and a tool's result row.
const (
	ChatRoleUser      = "user"
	ChatRoleAssistant = "assistant"
	ChatRoleTool      = "tool"
)

// Content-block kinds of a chat row. One block per row in v1 - the
// same granularity the conversation's transcript renders; rows grow
// more blocks only when a real renderer needs them.
const (
	ChatBlockText       = "text"
	ChatBlockThinking   = "thinking"
	ChatBlockToolCall   = "tool_call"
	ChatBlockToolResult = "tool_result"
)

// ChatBlock is one content block of a chat row. Content carries text
// and thinking bodies, and the tool_result body; a tool_call block
// carries the call verbatim (id, name, arguments). Running marks the
// in-flight blocks of a live stream - the block the frontend keeps
// appending to until a later delta closes the row; persisted rows
// never carry it.
type ChatBlock struct {
	Type       string         `json:"type"`
	Content    string         `json:"content,omitempty"`
	Images     []ImageContent `json:"images,omitempty"`
	Running    bool           `json:"running,omitempty"`
	ToolCall   *ToolCall      `json:"tool_call,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	IsError    bool           `json:"is_error,omitempty"`
	ElapsedMs  int64          `json:"elapsed_ms,omitempty"`
}

// ChatRow is one message row of a chat panel: role, its content
// blocks and the row's status. ID is the store row id when the row is
// persisted (the branch-point address variants address); a live
// stream's in-flight rows carry none.
type ChatRow struct {
	ID     string      `json:"id,omitempty"`
	Role   string      `json:"role"`
	Status string      `json:"status"`
	Blocks []ChatBlock `json:"blocks,omitempty"`
}

// ChatPayload is the chat panel's view-model: the session the rows
// cover (the session ref), whether a live run is streaming it right
// now, and the newest message rows. Deltas carry the whole payload -
// rows are small, and self-describing beats a second message kind.
type ChatPayload struct {
	Session string    `json:"session,omitempty"`
	Running bool      `json:"running,omitempty"`
	Rows    []ChatRow `json:"rows,omitempty"`
}

// PanelProvider is the additive canvas registration seam (settled
// Q5a/Q4b): a module declares its panels and registers their action
// handlers through the same interface, appended to
// Context.PanelProviders during Mount - the same registration point
// as ToolProviders and TUIProviders. A plugin left out of
// buildPlugins never mounts, so its panels are simply absent from the
// snapshot - the same law the webui enforces through directory
// registration.
type PanelProvider interface {
	// Panels returns the panel descriptors this module mounts. The
	// runtime seeds its current panel state from these at mount.
	Panels() []Panel
	// HandleAction executes one action call routed to this module. The
	// call already passed the runtime's route checks (module known,
	// action id among the module's registered actions); it may carry
	// any of the module's panels' action ids.
	HandleAction(call ActionCall) (any, error)
}
