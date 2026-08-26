package ai

import (
	"encoding/json"
	"fmt"
	"time"
)

// NowISO returns a UTC timestamp in ISO 8601 format.
func NowISO() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

// EncodeMessage serializes an ai.Message to its role discriminator
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
	case ModelChange:
		role = "model_change"
	case ThinkingLevelChange:
		role = "thinking_level_change"
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
	default:
		return nil, fmt.Errorf("unknown role %q", role)
	}
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
type User struct {
	Content   string         `json:"content"`
	Images    []ImageContent `json:"images,omitempty"`
	Timestamp string         `json:"timestamp,omitempty"`
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

// Assistant is the model's response in a turn.
type Assistant struct {
	Thinking  *string    `json:"thinking,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	Content   string     `json:"content"`
	Timestamp string     `json:"timestamp,omitempty"`
}

func (Assistant) isMessage() {}

// NewAssistant creates an Assistant message with timestamp set.
// Thinking and tool calls are set by the caller.
func NewAssistant(content string) Assistant {
	return Assistant{Content: content, Timestamp: NowISO()}
}

// ToolResult is the result of a tool execution, appended to the
// message history so the model can see the result.
type ToolResult struct {
	Content    string `json:"content"`
	ToolCallID string `json:"tool_call_id"`
	IsError    bool   `json:"is_error,omitempty"`
	Timestamp  string `json:"timestamp,omitempty"`
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
