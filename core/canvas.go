// The canvas contract. One contract, one transport: the http_channel serializes the canvas
// shapes over SSE/REST. The schema is minimal by law - the vocabulary grows one migration at
// a time, never designed on paper ahead of a consumer.
package core

import (
	"encoding/json"
	"errors"
)

type PanelAction struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Module string `json:"module"`
}

// Panel is one view-model panel descriptor. Payload carries the render data as JSON - the
// contract knows nothing about its shape; each panel kind does.
type Panel struct {
	ID      string        `json:"id"`
	Title   string        `json:"title"`
	Kind    string        `json:"kind"`
	Payload any           `json:"payload,omitempty"`
	Actions []PanelAction `json:"actions,omitempty"`
}

type ActionCall struct {
	Module string          `json:"module"`
	Action string          `json:"action"`
	Args   json.RawMessage `json:"args,omitempty"`
}

var (
	// ErrUnknownModule - the named capsule contributed no panel provider.
	ErrUnknownModule = errors.New("canvas: unknown module")
	// ErrUnknownAction - the module exists but registered no such action id.
	ErrUnknownAction = errors.New("canvas: unknown action")
)

type ViewModelDelta struct {
	Type  string `json:"type"`
	Panel Panel  `json:"panel"`
}

func (ViewModelDelta) isEvent() {}

type PanelUpdateSink interface {
	SetPanelUpdate(update func(panel Panel) error)
}

// The chat kind vocabulary: Panel kind 'chat' is the conversation itself as a canvas surface,
// grown by a real migration (the http channel's chat runs), never on paper.

const PanelKindChat = "chat"

const (
	ChatStatusStreaming = "streaming"
	ChatStatusComplete  = "complete"
)

const (
	ChatRoleUser      = "user"
	ChatRoleAssistant = "assistant"
	ChatRoleTool      = "tool"
)

const (
	ChatBlockText       = "text"
	ChatBlockThinking   = "thinking"
	ChatBlockToolCall   = "tool_call"
	ChatBlockToolResult = "tool_result"
)

// ChatBlock is one content block of a chat row. Running marks the in-flight blocks of a live
// stream - the block the frontend keeps appending to until a later delta closes the row;
// persisted rows never carry it.
type ChatBlock struct {
	Type    string         `json:"type"`
	Content string         `json:"content,omitempty"`
	Images  []ImageContent `json:"images,omitempty"`
	// Attachments rides a user text block: references, never content.
	Attachments []Attachment `json:"attachments,omitempty"`
	Running     bool         `json:"running,omitempty"`
	ToolCall    *ToolCall    `json:"tool_call,omitempty"`
	ToolCallID  string       `json:"tool_call_id,omitempty"`
	IsError     bool         `json:"is_error,omitempty"`
	Gate      string `json:"gate,omitempty"`
	ElapsedMs int64  `json:"elapsed_ms,omitempty"`
	// StartedAt rides an UNANSWERED tool_call block: the reader derives now - StartedAt as it
	// paints, so a start stamp cannot go stale.
	StartedAt string `json:"started_at,omitempty"`
	AnswerMs int64 `json:"answer_ms,omitempty"`
	// ResponseMs rides a text block; a completed row renders its frozen duration from it.
	ResponseMs int64 `json:"response_ms,omitempty"`
	ThinkingMs int64 `json:"thinking_ms,omitempty"`
}

type ChatRow struct {
	ID     string `json:"id,omitempty"`
	Role   string `json:"role"`
	Status string `json:"status"`
	Time   string      `json:"time,omitempty"`
	Blocks []ChatBlock `json:"blocks,omitempty"`
}

type ChatPayload struct {
	Session string    `json:"session,omitempty"`
	Running bool      `json:"running,omitempty"`
	Rows    []ChatRow `json:"rows,omitempty"`
}

// PanelProvider is the additive canvas registration seam. buildPlugins never mounts, so its
// panels are simply absent from the snapshot.
type PanelProvider interface {
	Panels() []Panel
	HandleAction(call ActionCall) (any, error)
}
