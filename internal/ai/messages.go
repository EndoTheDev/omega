package ai

import (
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

// User is a user chat message.
type User struct {
	Content   string `json:"content"`
	Timestamp string `json:"timestamp,omitempty"`
}

func (User) isMessage() {}

// NewUser creates a User message with timestamp set.
func NewUser(content string) User {
	return User{Content: content, Timestamp: NowISO()}
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
