package ai

// StreamEvent is the sealed interface for all stream event types.
// Consumers dispatch on the concrete type via a type switch.
type StreamEvent interface {
	isStreamEvent()
}

// ThinkingChunk is a chunk of the model's thinking content.
type ThinkingChunk struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

func (ThinkingChunk) isStreamEvent() {}

// ResponseChunk is a chunk of the model's response content.
type ResponseChunk struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

func (ResponseChunk) isStreamEvent() {}

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

func (ToolCallEvent) isStreamEvent() {}

// StreamEnd is emitted at the end of a stream. Errors are encoded
// here (FinishReason="error", Error set), not returned as Go errors.
type StreamEnd struct {
	Type            string `json:"type"`
	FinishReason    string `json:"finish_reason"`
	PromptEvalCount *int   `json:"prompt_eval_count,omitempty"`
	EvalCount       *int   `json:"eval_count,omitempty"`
	Error           string `json:"error,omitempty"`
}

func (StreamEnd) isStreamEvent() {}
