package core

import (
	"encoding/json"
	"fmt"
	"time"
)

// NowISO returns a UTC timestamp in ISO 8601 format.
func NowISO() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

// Message is the sealed interface for all message types.
// Consumers use type switches or type assertions to access
// concrete types.
type Message interface {
	isMessage()
}

// System is the system prompt message.
type System struct {
	Content   string `json:"content"`
	Timestamp string `json:"timestamp,omitempty"`
}

func (System) isMessage() {}

// NewSystem creates a System message with timestamp set.
func NewSystem(content string) System {
	return System{Content: content, Timestamp: NowISO()}
}

// ImageContent carries a base64-encoded image with its MIME type.
// When present on a User message, providers serialize it as image
// content blocks alongside the text content.
type ImageContent struct {
	MediaType string `json:"media_type"` // "image/png", "image/jpeg", etc.
	Base64    string `json:"base64"`     // base64-encoded image data (no data: prefix)
}

// Attachment is a typed reference to a non-image file the user attached
// to a message (card t_1ffd07de). The bytes live on disk in the
// session's upload directory; the wire - and the persisted message -
// carry only this reference. Nothing here extracts, inlines or parses
// the content: Kind is a LABEL of the media class the filename declares
// (the lowercased extension without its dot, "file" when it has none),
// Path is the absolute file the agent can open with its own tools, and
// Size is the byte count on disk.
type Attachment struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
	Size int64  `json:"size"`
	Path string `json:"path"`
}

// User is a user chat message. Images is optional; when empty the
// message is text-only and providers send content as a plain string.
// Attachments is the non-image sibling of Images (card t_1ffd07de):
// references only, never bytes.
// ParentID is the message tree pointer (branching, t_17365247): the
// store-row id of the message this one follows on the conversation
// tree. Empty = legacy message (no pointers) or the session's first
// message. Providers strip it from wire payloads; it only ever
// matters to store-side tree derivation.
type User struct {
	Content     string         `json:"content"`
	Images      []ImageContent `json:"images,omitempty"`
	Attachments []Attachment   `json:"attachments,omitempty"`
	Timestamp   string         `json:"timestamp,omitempty"`
	ParentID    string         `json:"parent_id,omitempty"`
}

func (User) isMessage() {}

// NewUser creates a User message with timestamp set.
func NewUser(content string) User {
	return User{Content: content, Timestamp: NowISO()}
}

// NewUserWithImages creates a User message with image content.
func NewUserWithImages(content string, images []ImageContent) User {
	return User{Content: content, Images: images, Timestamp: NowISO()}
}

// Timing carries server-measured generation durations so a reloaded
// session renders real elapsed values - client-side meters die with
// the SSE stream. Assistant messages carry the thinking/response
// spans; ToolResult carries the elapsed from its matching call.
type Timing struct {
	ThinkingMs int64 `json:"thinking_ms,omitempty"`
	ResponseMs int64 `json:"response_ms,omitempty"`
	ElapsedMs  int64 `json:"elapsed_ms,omitempty"`
}

// Assistant is the model's response in a turn.
// ParentID: see User's field comment (message tree pointer).
type Assistant struct {
	Thinking  *string    `json:"thinking,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	Content   string     `json:"content"`
	Timing    *Timing    `json:"timing,omitempty"`
	Timestamp string     `json:"timestamp,omitempty"`
	ParentID  string     `json:"parent_id,omitempty"`
}

func (Assistant) isMessage() {}

// NewAssistant creates an Assistant message with timestamp set.
// Thinking and tool calls are set by the caller.
func NewAssistant(content string) Assistant {
	return Assistant{Content: content, Timestamp: NowISO()}
}

// ToolResult is the result of a tool execution, appended to the
// message history so the model can see the result. ParentID chains it
// to the message it follows within one run (see User's comment).
// Gate is the smart-approval gate's decision record (card
// t_1b3e2a8b): the transcript-facing verdict line the gate produced
// for the call this result answers, written by the approval capsule
// and rendered by the webui on the call's own row. Empty = no machine
// decision (ungated command, manual mode, an escalation with no
// resolution) - no transcript line. It rides the persisted payload,
// so a reloaded session shows the same record.
type ToolResult struct {
	Content    string  `json:"content"`
	ToolCallID string  `json:"tool_call_id"`
	IsError    bool    `json:"is_error,omitempty"`
	Gate       string  `json:"gate,omitempty"`
	Timing     *Timing `json:"timing,omitempty"`
	Timestamp  string  `json:"timestamp,omitempty"`
	ParentID   string  `json:"parent_id,omitempty"`
}

func (ToolResult) isMessage() {}

// NewToolResult creates a ToolResult with timestamp set.
func NewToolResult(content, toolCallID string, isError bool) ToolResult {
	return ToolResult{
		Content:    content,
		ToolCallID: toolCallID,
		IsError:    isError,
		Timestamp:  NowISO(),
	}
}

// YieldResult is the synthetic re-entry message for a yield-class tool
// call (async agent loop, design of record
// Workshop/omega/async-agent-loop.md): the executor that ran the
// dispatched call writes one through the home's input path when the
// call settles, and it starts a new turn exactly like a user message
// does. First-class and RAP-compatible from day one (ruling 3): an
// out-of-process executor, a gateway webhook, or another omega machine
// is just another writer of this shape - the loop never changes.
// Timeout marks the synthetic recovery injections (boot recovery and a
// runtime expiry): the agent sees an interrupted result and may retry.
// Store role: "yield_result". Render role: "tool" - every transcript
// surface treats it as its call's result row.
type YieldResult struct {
	ToolCallID string `json:"tool_call_id"`
	ToolName   string `json:"tool_name,omitempty"`
	Content    string `json:"content"`
	IsError    bool   `json:"is_error,omitempty"`
	// Timeout marks the synthetic "interrupted" injection - boot
	// recovery and a dispatched call's runtime expiry - never a real
	// tool's verdict.
	Timeout   bool    `json:"timeout,omitempty"`
	Gate      string  `json:"gate,omitempty"`
	Timing    *Timing `json:"timing,omitempty"`
	Timestamp string  `json:"timestamp,omitempty"`
	ParentID  string  `json:"parent_id,omitempty"`
}

func (YieldResult) isMessage() {}

// NewYieldResult creates a YieldResult with timestamp set.
func NewYieldResult(content, toolCallID string, isError, timeout bool) YieldResult {
	return YieldResult{
		Content:    content,
		ToolCallID: toolCallID,
		IsError:    isError,
		Timeout:    timeout,
		Timestamp:  NowISO(),
	}
}

// ModelChange is a session entry recording a model switch. It is
// persisted to the store and replayed on resume to restore the model.
type ModelChange struct {
	Model     string `json:"model"`
	Timestamp string `json:"timestamp,omitempty"`
}

func (ModelChange) isMessage() {}

// NewModelChange creates a ModelChange with timestamp set.
func NewModelChange(model string) ModelChange {
	return ModelChange{Model: model, Timestamp: NowISO()}
}

// ThinkingLevelChange is a session entry recording a thinking level
// change. It is persisted to the store and replayed on resume.
type ThinkingLevelChange struct {
	Level     string `json:"level"`
	Timestamp string `json:"timestamp,omitempty"`
}

func (ThinkingLevelChange) isMessage() {}

// NewThinkingLevelChange creates a ThinkingLevelChange with timestamp set.
func NewThinkingLevelChange(level string) ThinkingLevelChange {
	return ThinkingLevelChange{Level: level, Timestamp: NowISO()}
}

// VariantSelect records the ACTIVE variant of one message branch point
// (t_17365247). It is a store bookkeeping entry exactly like
// ModelChange: appended to the linear list (no schema or payload
// mutation of existing rows), read by tree derivation, invisible to
// rendering and context assembly. Branch is the parent message id (the
// branch point - a user message row id); Child is the id of the run's
// head message (assistant row id) the selection activates. The LAST
// entry per branch wins.
type VariantSelect struct {
	Branch    string `json:"branch"`
	Child     string `json:"child"`
	Timestamp string `json:"timestamp,omitempty"`
}

func (VariantSelect) isMessage() {}

// NewVariantSelect creates a VariantSelect entry with timestamp set.
func NewVariantSelect(branch, child string) VariantSelect {
	return VariantSelect{Branch: branch, Child: child, Timestamp: NowISO()}
}

// QuestionOption is one pick row on a verdict card (verdict cards
// v1.1): the label the card shows, plus an optional one-line
// description rendered under the label. UnmarshalJSON accepts the
// v1 shape (a bare string) so old audit rows keep decoding; the
// marshal side is always the object form.
type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

func (v *QuestionOption) UnmarshalJSON(b []byte) error {
	var label string
	if err := json.Unmarshal(b, &label); err == nil {
		v.Label = label
		return nil
	}
	type plain QuestionOption
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	*v = QuestionOption(p)
	return nil
}

// QuestionCard is the transcript record of one agent-issued question
// (verdict cards v1): appended when the verdict tool issues the
// card, before the user answers. A bookkeeping entry exactly like
// ModelChange: persisted to the store, invisible to context assembly
// (the model sees the tool call and result, never this row), rendered
// by every transcript surface that wants the audit trail. Roles on the
// store row: "verdict_card".
type QuestionCard struct {
	CardID    string           `json:"card_id"`
	Title     string           `json:"title"`
	Body      string           `json:"body,omitempty"`
	Options   []QuestionOption `json:"options,omitempty"`
	Multi     bool             `json:"multi,omitempty"`
	AllowText bool             `json:"allow_text,omitempty"`
	TimeoutS  int              `json:"timeout_s,omitempty"`
	Timestamp string           `json:"timestamp,omitempty"`
}

func (QuestionCard) isMessage() {}

// NewQuestionCard creates a QuestionCard entry with timestamp set.
func NewQuestionCard(cardID, title, body string, options []QuestionOption, multi, allowText bool, timeoutS int) QuestionCard {
	return QuestionCard{
		CardID:    cardID,
		Title:     title,
		Body:      body,
		Options:   options,
		Multi:     multi,
		AllowText: allowText,
		TimeoutS:  timeoutS,
		Timestamp: NowISO(),
	}
}

// QuestionRecord is the transcript record of one answer: a single
// pick, a multi-select pick (every selected index), free text, a
// recorded skip (no silent dismissal), or the expiry firing with no
// answer. Role on the store row: "verdict". Named QuestionRecord (not
// Verdict) because core.QuestionAnswer is already the runtime answer record
// returned by the verdict seam - the transcript row is a superset with
// the store timestamp.
type QuestionRecord struct {
	CardID        string   `json:"card_id"`
	OptionIndex   int      `json:"option_index,omitempty"`
	Option        string   `json:"option,omitempty"`
	OptionIndexes []int    `json:"option_indexes,omitempty"`
	Options       []string `json:"options,omitempty"`
	Text          string   `json:"text,omitempty"`
	Skipped       bool     `json:"skipped,omitempty"`
	Expired       bool     `json:"expired,omitempty"`
	Timestamp     string   `json:"timestamp,omitempty"`
}

func (QuestionRecord) isMessage() {}

// NewVerdictRecord creates a QuestionRecord entry with timestamp set. A
// single pick indexes one option (optionIndex >= 1, option its label);
// a multi pick carries the selected indexes and their labels; text,
// skip, and expiry carry none.
func NewVerdictRecord(cardID string, optionIndexes []int, options []string, text string, skipped, expired bool) QuestionRecord {
	v := QuestionRecord{
		CardID:    cardID,
		Text:      text,
		Skipped:   skipped,
		Expired:   expired,
		Timestamp: NowISO(),
	}
	if len(optionIndexes) > 0 {
		v.OptionIndexes = optionIndexes
		v.OptionIndex = optionIndexes[0]
	}
	if len(options) > 0 {
		v.Options = options
		v.Option = options[0]
	}
	return v
}

// MessageRole returns the role string for a message ("user", "assistant",
// "system", "tool", or "unknown"). Lived in ai until messagetree moved
// into core (arc 1c, t_483de955) and dragged its closure: tree
// derivation classifies rows by role, and the law test forbids core
// importing omega/llm. Shared by export, store commands, the CLI, and
// the transcript surfaces to avoid duplicating the type switch.
func MessageRole(m Message) string {
	switch m.(type) {
	case System:
		return "system"
	case User:
		return "user"
	case Assistant:
		return "assistant"
	case ToolResult, YieldResult:
		// The synthetic re-entry message renders as its call's tool
		// result row on every transcript surface.
		return "tool"
	case QuestionCard:
		return "verdict_card"
	case QuestionRecord:
		return "verdict"
	default:
		return "unknown"
	}
}

// EncodeMessage serializes a Message to its role discriminator
// and JSON payload. Used by the store extension for persistence
// and wire transfer.
func EncodeMessage(msg Message) (string, []byte, error) {
	var role string
	switch msg.(type) {
	case System:
		role = "system"
	case User:
		role = "user"
	case Assistant:
		role = "assistant"
	case ToolResult:
		role = "tool"
	case YieldResult:
		role = "yield_result"
	case ModelChange:
		role = "model_change"
	case ThinkingLevelChange:
		role = "thinking_level_change"
	case VariantSelect:
		role = "variant_select"
	case QuestionCard:
		role = "verdict_card"
	case QuestionRecord:
		role = "verdict"
	default:
		return "", nil, fmt.Errorf("unknown message type %T", msg)
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		return "", nil, err
	}
	return role, payload, nil
}

// DecodeMessage reconstructs a Message from a role discriminator
// and JSON payload. Inverse of EncodeMessage.
func DecodeMessage(role string, payload []byte) (Message, error) {
	switch role {
	case "system":
		var m System
		if err := json.Unmarshal(payload, &m); err != nil {
			return nil, err
		}
		return m, nil
	case "user":
		var m User
		if err := json.Unmarshal(payload, &m); err != nil {
			return nil, err
		}
		return m, nil
	case "assistant":
		var m Assistant
		if err := json.Unmarshal(payload, &m); err != nil {
			return nil, err
		}
		return m, nil
	case "tool":
		var m ToolResult
		if err := json.Unmarshal(payload, &m); err != nil {
			return nil, err
		}
		return m, nil
	case "yield_result":
		var m YieldResult
		if err := json.Unmarshal(payload, &m); err != nil {
			return nil, err
		}
		return m, nil
	case "model_change":
		var m ModelChange
		if err := json.Unmarshal(payload, &m); err != nil {
			return nil, err
		}
		return m, nil
	case "thinking_level_change":
		var m ThinkingLevelChange
		if err := json.Unmarshal(payload, &m); err != nil {
			return nil, err
		}
		return m, nil
	case "variant_select":
		var m VariantSelect
		if err := json.Unmarshal(payload, &m); err != nil {
			return nil, err
		}
		return m, nil
	case "verdict_card":
		var m QuestionCard
		if err := json.Unmarshal(payload, &m); err != nil {
			return nil, err
		}
		return m, nil
	case "verdict":
		var m QuestionRecord
		if err := json.Unmarshal(payload, &m); err != nil {
			return nil, err
		}
		return m, nil
	default:
		return nil, fmt.Errorf("unknown role %q", role)
	}
}
