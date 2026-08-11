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
	delay     time.Duration
}

// NewFakeProvider creates a FakeProvider that replays script in order
// on each Stream call. A non-zero delay is applied before each event.
func NewFakeProvider(model string, script ...StreamEvent) *FakeProvider {
	return &FakeProvider{modelName: model, script: script}
}

// WithDelay returns a copy of the provider that sleeps delay before
// emitting each event, so cancellation can be observed mid-stream.
func (p *FakeProvider) WithDelay(delay time.Duration) *FakeProvider {
	cp := *p
	cp.delay = delay
	return &cp
}

// ModelName returns the model name used by this provider.
func (p *FakeProvider) ModelName() string {
	return p.modelName
}

// Stream replays the scripted events on a channel, respecting context
// cancellation, and closes the channel when done. An empty script
// closes the channel immediately.
func (p *FakeProvider) Stream(ctx context.Context, _ []Message, _ []ToolSchema) <-chan StreamEvent {
	events := make(chan StreamEvent)
	go func() {
		defer close(events)
		for _, event := range p.script {
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
	}()
	return events
}
