package ai

import (
	"context"
	"github.com/EndoTheDev/omega/core"
	"time"
)

// FakeProvider implements core.Provider and emits a scripted sequence of
// StreamEvents instead of calling a real API. It unlocks deterministic
// agent loop testing.
type FakeProvider struct {
	modelName string
	script    []core.ProviderStreamEvent
	scripts   [][]core.ProviderStreamEvent // per-call scripts; the last repeats
	delay     time.Duration
	calls     int
	// lastRequestedLevel records the thinking level this provider
	// carries - set by SetThinkingLevel (mutations) and threaded
	// through WithThinkingLevel copies - so tests can assert it.
	lastRequestedLevel string
	// LastMessages records the messages passed to the most recent Stream
	// call, so tests can assert on the history the agent built up.
	LastMessages []core.Message
}

// NewFakeProvider creates a FakeProvider that replays script in order
// on each Stream call. A non-zero delay is applied before each event.
func NewFakeProvider(model string, script ...core.ProviderStreamEvent) *FakeProvider {
	return &FakeProvider{modelName: model, script: script}
}

// NewFakeProviderScripts creates a FakeProvider that replays each script
// in order on successive Stream calls; the last script repeats. Use it
// for multi-turn loops where each turn needs different events.
func NewFakeProviderScripts(model string, scripts ...[]core.ProviderStreamEvent) *FakeProvider {
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

// SetThinkingLevel is a mutating setter for the TUI; per-run
// derivations go through WithThinkingLevel instead.
func (p *FakeProvider) SetThinkingLevel(level string) { p.lastRequestedLevel = level }

// ThinkingLevel returns the level this provider (or the copy it came
// from) carries. WithThinkingLevel threads the level through the copy
// so tests can assert per-run derivation.
func (p *FakeProvider) ThinkingLevel() string { return p.lastRequestedLevel }

// WithModel returns a copy of the provider serving model. The copy
// shares the scripts so it streams the same sequence.
func (p *FakeProvider) WithModel(model string) core.Provider {
	cp := *p
	cp.modelName = model
	return &cp
}

// WithThinkingLevel returns a copy of the provider at the given
// thinking level. Same copy discipline as WithModel: shares the
// scripts, leaves the receiver unchanged.
func (p *FakeProvider) WithThinkingLevel(level string) core.Provider {
	cp := *p
	cp.lastRequestedLevel = level
	return &cp
}

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
func (p *FakeProvider) Stream(ctx context.Context, messages []core.Message, _ []core.ToolSchema) <-chan core.ProviderStreamEvent {
	p.LastMessages = messages
	events := make(chan core.ProviderStreamEvent)
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
