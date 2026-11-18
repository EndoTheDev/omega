// The canvas contract (factory-flow ADR, arc 3a). One contract, one transport
// (settled): the http_channel serializes the canvas shapes over SSE/REST. The schema is
// minimal by law (settled Q2b) - the vocabulary grows one migration at a time, never
// designed on paper ahead of a consumer.
package core

import (
	"encoding/json"
	"errors"
)

// PanelAction is one action a frontend can invoke on a panel through the action pipe.
type PanelAction struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Module string `json:"module"`
}

// Panel is one view-model panel descriptor: the entire v1 schema (settled Q2b). Payload
// carries the panel's render data as JSON - the contract knows nothing about the
// payload's shape; each panel kind does.
type Panel struct {
	ID      string        `json:"id"`
	Title   string        `json:"title"`
	Kind    string        `json:"kind"`
	Payload any           `json:"payload,omitempty"`
	Actions []PanelAction `json:"actions,omitempty"`
}

// ActionCall is one action-pipe call (settled Q4b): module + action id + arguments.
type ActionCall struct {
	Module string          `json:"module"`
	Action string          `json:"action"`
	Args   json.RawMessage `json:"args,omitempty"`
}

// The action pipe's 404 shapes.
var (
	// ErrUnknownModule - the named capsule contributed no panel provider.
	ErrUnknownModule = errors.New("canvas: unknown module")
	// ErrUnknownAction - the module exists but registered no such action id.
	ErrUnknownAction = errors.New("canvas: unknown action")
)

// ViewModelDelta is the canvas event kind (settled Q3b).
type ViewModelDelta struct {
	Type  string `json:"type"`
	Panel Panel  `json:"panel"`
}

func (ViewModelDelta) isEvent() {}

// PanelUpdateSink is the optional capability of a PanelProvider whose panel state.
type PanelUpdateSink interface {
	SetPanelUpdate(update func(panel Panel) error)
}

// The chat kind vocabulary: the conversation itself as a canvas surface. Panel kind
// 'chat' is the first NEW kind since the canvas contract - grown by a real migration
// (the http channel's chat runs), never on paper ahead of a consumer.

// PanelKindChat is the chat panel kind.
const PanelKindChat = "chat"

// ChatRow statuses: streaming while any block of the row is still in flight, complete.
const (
	ChatStatusStreaming = "streaming"
	ChatStatusComplete  = "complete"
)

// Roles of a chat row: the user bubble, an assistant bubble (its blocks split.
const (
	ChatRoleUser      = "user"
	ChatRoleAssistant = "assistant"
	ChatRoleTool      = "tool"
)

// Content-block kinds of a chat row.
const (
	ChatBlockText       = "text"
	ChatBlockThinking   = "thinking"
	ChatBlockToolCall   = "tool_call"
	ChatBlockToolResult = "tool_result"
)

// ChatBlock is one content block of a chat row. Running marks the in-flight blocks of a
// live stream - the block the frontend keeps appending to until a later delta closes
// the row; persisted rows never carry it.
type ChatBlock struct {
	Type    string         `json:"type"`
	Content string         `json:"content,omitempty"`
	Images  []ImageContent `json:"images,omitempty"`
	// Attachments rides a user text block. Same shape the message persisted - references,
	// never content.
	Attachments []Attachment `json:"attachments,omitempty"`
	Running     bool         `json:"running,omitempty"`
	ToolCall    *ToolCall    `json:"tool_call,omitempty"`
	ToolCallID  string       `json:"tool_call_id,omitempty"`
	IsError     bool         `json:"is_error,omitempty"`
	Gate      string `json:"gate,omitempty"`
	ElapsedMs int64  `json:"elapsed_ms,omitempty"`
	// StartedAt rides an UNANSWERED tool_call block. A start stamp cannot go stale: the
	// reader derives now - StartedAt at the moment it paints.
	StartedAt string `json:"started_at,omitempty"`
	AnswerMs int64 `json:"answer_ms,omitempty"`
	// ResponseMs rides a text block. A completed row renders its frozen duration from
	// them, on a reload exactly as before it.
	ResponseMs int64 `json:"response_ms,omitempty"`
	ThinkingMs int64 `json:"thinking_ms,omitempty"`
}

// ChatRow is one message row of a chat panel.
type ChatRow struct {
	ID     string `json:"id,omitempty"`
	Role   string `json:"role"`
	Status string `json:"status"`
	Time   string      `json:"time,omitempty"`
	Blocks []ChatBlock `json:"blocks,omitempty"`
}

// ChatPayload is the chat panel's view-model.
type ChatPayload struct {
	Session string    `json:"session,omitempty"`
	Running bool      `json:"running,omitempty"`
	Rows    []ChatRow `json:"rows,omitempty"`
}

// PanelProvider is the additive canvas registration seam (settled Q5a/Q4b).

// buildPlugins never mounts, so its panels are simply absent from the snapshot.
type PanelProvider interface {
	// Panels returns the panel descriptors this module mounts.
	Panels() []Panel
	// HandleAction executes one action call routed to this module.
	HandleAction(call ActionCall) (any, error)
}
