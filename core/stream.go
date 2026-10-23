package core

// ProviderStreamEvent is the sealed interface for all provider stream
// event types. Consumers dispatch on the concrete type via a type
// switch. Named ProviderStreamEvent (not StreamEvent) because the
// agent-event contract already carries core.StreamEvent - the wrapper
// that forwards one of these events into the run's event stream.
type ProviderStreamEvent interface {
	isProviderStreamEvent()
}

// ThinkingChunk is a chunk of the model's thinking content.
type ThinkingChunk struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

func (ThinkingChunk) isProviderStreamEvent() {}

// ResponseChunk is a chunk of the model's response content.
type ResponseChunk struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

func (ResponseChunk) isProviderStreamEvent() {}

// ToolCall is a tool invocation request from the model.
type ToolCall struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// ToolCallEvent carries a tool call parsed from the stream.
type ToolCallEvent struct {
	Type     string   `json:"type"`
	ToolCall ToolCall `json:"tool_call"`
}

func (ToolCallEvent) isProviderStreamEvent() {}

// StreamEnd is emitted at the end of a stream. Errors are encoded
// here (FinishReason="error", Error set), not returned as Go errors.
type StreamEnd struct {
	Type            string `json:"type"`
	FinishReason    string `json:"finish_reason"`
	PromptEvalCount *int   `json:"prompt_eval_count,omitempty"`
	EvalCount       *int   `json:"eval_count,omitempty"`
	CachedCount     *int   `json:"cached_count,omitempty"`
	Error           string `json:"error,omitempty"`
}

func (StreamEnd) isProviderStreamEvent() {}
