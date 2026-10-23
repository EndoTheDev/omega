package core

import (
	"encoding/json"
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

// User is a user chat message. Images is optional; when empty the
// message is text-only and providers send content as a plain string.
// ParentID is the message tree pointer (branching, t_17365247): the
// store-row id of the message this one follows on the conversation
// tree. Empty = legacy message (no pointers) or the session's first
// message. Providers strip it from wire payloads; it only ever
// matters to store-side tree derivation.
type User struct {
	Content   string         `json:"content"`
	Images    []ImageContent `json:"images,omitempty"`
	Timestamp string         `json:"timestamp,omitempty"`
	ParentID  string         `json:"parent_id,omitempty"`
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
type ToolResult struct {
	Content    string  `json:"content"`
	ToolCallID string  `json:"tool_call_id"`
	IsError    bool    `json:"is_error,omitempty"`
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

// VerdictOption is one pick row on a verdict card (verdict cards
// v1.1): the label the card shows, plus an optional one-line
// description rendered under the label. UnmarshalJSON accepts the
// v1 shape (a bare string) so old audit rows keep decoding; the
// marshal side is always the object form.
type VerdictOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

func (v *VerdictOption) UnmarshalJSON(b []byte) error {
	var label string
	if err := json.Unmarshal(b, &label); err == nil {
		v.Label = label
		return nil
	}
	type plain VerdictOption
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	*v = VerdictOption(p)
	return nil
}

// VerdictCard is the transcript record of one agent-issued question
// (verdict cards v1): appended when the verdict tool issues the
// card, before the user answers. A bookkeeping entry exactly like
// ModelChange: persisted to the store, invisible to context assembly
// (the model sees the tool call and result, never this row), rendered
// by every transcript surface that wants the audit trail. Roles on the
// store row: "verdict_card".
type VerdictCard struct {
	CardID    string          `json:"card_id"`
	Title     string          `json:"title"`
	Body      string          `json:"body,omitempty"`
	Options   []VerdictOption `json:"options,omitempty"`
	Multi     bool            `json:"multi,omitempty"`
	AllowText bool            `json:"allow_text,omitempty"`
	TimeoutS  int             `json:"timeout_s,omitempty"`
	Timestamp string          `json:"timestamp,omitempty"`
}

func (VerdictCard) isMessage() {}

// NewVerdictCard creates a VerdictCard entry with timestamp set.
func NewVerdictCard(cardID, title, body string, options []VerdictOption, multi, allowText bool, timeoutS int) VerdictCard {
	return VerdictCard{
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

// VerdictRecord is the transcript record of one answer: a single
// pick, a multi-select pick (every selected index), free text, a
// recorded skip (no silent dismissal), or the expiry firing with no
// answer. Role on the store row: "verdict". Named VerdictRecord (not
// Verdict) because core.Verdict is already the runtime answer record
// returned by the verdict seam - the transcript row is a superset with
// the store timestamp.
type VerdictRecord struct {
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

func (VerdictRecord) isMessage() {}

// NewVerdictRecord creates a VerdictRecord entry with timestamp set. A
// single pick indexes one option (optionIndex >= 1, option its label);
// a multi pick carries the selected indexes and their labels; text,
// skip, and expiry carry none.
func NewVerdictRecord(cardID string, optionIndexes []int, options []string, text string, skipped, expired bool) VerdictRecord {
	v := VerdictRecord{
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
// importing omega/ai. Shared by export, store commands, the TUI, and
// the transcript surfaces to avoid duplicating the type switch.
func MessageRole(m Message) string {
	switch m.(type) {
	case System:
		return "system"
	case User:
		return "user"
	case Assistant:
		return "assistant"
	case ToolResult:
		return "tool"
	case VerdictCard:
		return "verdict_card"
	case VerdictRecord:
		return "verdict"
	default:
		return "unknown"
	}
}
