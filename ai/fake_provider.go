package ai

import (
	"context"
	"time"
)

// FakeProvider implements Provider and emits a scripted sequence of
// StreamEvents instead of calling a real API. It unlocks deterministic
// agent loop testing.
type FakeProvider struct {
	modelName string
	script    []StreamEvent
	scripts   [][]StreamEvent // per-call scripts; the last repeats
	delay     time.Duration
	calls     int
	// LastMessages records the messages passed to the most recent Stream
	// call, so tests can assert on the history the agent built up.
	LastMessages []Message
}

// NewFakeProvider creates a FakeProvider that replays script in order
// on each Stream call. A non-zero delay is applied before each event.
func NewFakeProvider(model string, script ...StreamEvent) *FakeProvider {
	return &FakeProvider{modelName: model, script: script}
}

// NewFakeProviderScripts creates a FakeProvider that replays each script
// in order on successive Stream calls; the last script repeats. Use it
// for multi-turn loops where each turn needs different events.
func NewFakeProviderScripts(model string, scripts ...[]StreamEvent) *FakeProvider {
	return &FakeProvider{modelName: model, scripts: scripts}
}

// WithDelay returns a copy of the provider that sleeps delay before
// emitting each event, so cancellation can be observed mid-stream.
func (p *FakeProvider) WithDelay(delay time.Duration) *FakeProvider {
	cp := *p
	cp.delay = delay
	return &cp
}

// Calls returns the number of Stream calls made. Safe to read after the
// caller has drained a stream channel: the counter increments before the
// channel closes.
func (p *FakeProvider) Calls() int {
	return p.calls
}

// ModelName returns the model name used by this provider.
func (p *FakeProvider) ModelName() string {
	return p.modelName
}

// SetThinkingLevel is a no-op for the fake provider.
func (p *FakeProvider) SetThinkingLevel(level string) {}

// SetModel updates the model name for testing.
func (p *FakeProvider) SetModel(model string) { p.modelName = model }

// ListModels returns a hardcoded list for testing.
func (p *FakeProvider) ListModels() ([]string, error) {
	return []string{p.modelName, "other-model"}, nil
}

// ModelInfo returns a fixed context window for testing.
func (p *FakeProvider) ModelInfo() (ModelInfo, error) {
	return ModelInfo{ContextWindow: 8192}, nil
}

// Stream replays the scripted events on a channel, respecting context
// cancellation, and closes the channel when done. An empty script
// closes the channel immediately.
func (p *FakeProvider) Stream(ctx context.Context, messages []Message, _ []ToolSchema) <-chan StreamEvent {
	p.LastMessages = messages
	events := make(chan StreamEvent)
	go func() {
		defer close(events)
		script := p.script
		if p.scripts != nil {
			index := p.calls
			if index >= len(p.scripts) {
				index = len(p.scripts) - 1
			}
			if index >= 0 {
				script = p.scripts[index]
			}
		}
		for _, event := range script {
			if p.delay > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(p.delay):
				}
			}
			select {
			case <-ctx.Done():
				return
			case events <- event:
			}
		}
		p.calls++
	}()
	return events
}
