package ai

import (
	"context"
	"fmt"
)

// ProviderDispatcher is the interface that an extension manager
// implements to dispatch provider calls to a provider-seam extension.
// StdioManager in agent/ satisfies this interface.
type ProviderDispatcher interface {
	ProviderStream(ctx context.Context, messages []Message, tools []ToolSchema) <-chan StreamEvent
	ProviderModelName() string
	ProviderListModels() ([]string, error)
	ProviderModelInfo() (ModelInfo, error)
	ProviderSetThinking(level string)
	ProviderSetModel(model string)
}

// ExtensionProvider implements Provider by delegating to a
// ProviderDispatcher (typically a StdioManager with a loaded
// core-provider extension). With no provider extension loaded,
// Stream returns a single StreamEnd error.
type ExtensionProvider struct {
	Dispatcher ProviderDispatcher
}

// Stream delegates to the dispatcher's ProviderStream.
func (p ExtensionProvider) Stream(ctx context.Context, messages []Message, tools []ToolSchema) <-chan StreamEvent {
	if p.Dispatcher == nil {
		ch := make(chan StreamEvent, 1)
		ch <- StreamEnd{Type: "stream_end", FinishReason: "error", Error: "no provider extension loaded"}
		close(ch)
		return ch
	}
	return p.Dispatcher.ProviderStream(ctx, messages, tools)
}

// ModelName delegates to the dispatcher's ProviderModelName.
func (p ExtensionProvider) ModelName() string {
	if p.Dispatcher == nil {
		return ""
	}
	return p.Dispatcher.ProviderModelName()
}

// SetThinkingLevel delegates to the dispatcher's ProviderSetThinking.
func (p ExtensionProvider) SetThinkingLevel(level string) {
	if p.Dispatcher == nil {
		return
	}
	p.Dispatcher.ProviderSetThinking(level)
}

// SetModel delegates to the dispatcher's ProviderSetModel.
func (p ExtensionProvider) SetModel(model string) {
	if p.Dispatcher == nil {
		return
	}
	p.Dispatcher.ProviderSetModel(model)
}

// ListModels delegates to the dispatcher's ProviderListModels.
func (p ExtensionProvider) ListModels() ([]string, error) {
	if p.Dispatcher == nil {
		return nil, fmt.Errorf("no provider extension loaded")
	}
	return p.Dispatcher.ProviderListModels()
}

// ModelInfo delegates to the dispatcher's ProviderModelInfo.
func (p ExtensionProvider) ModelInfo() (ModelInfo, error) {
	if p.Dispatcher == nil {
		return ModelInfo{}, nil
	}
	return p.Dispatcher.ProviderModelInfo()
}
