package agent

import (
	"context"
	"github.com/EndoTheDev/omega/core"
	"time"
)

// FakeProvider implements the agent.Provider face and emits a scripted sequence of
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
	// instances carries the named fakes the Instance resolver serves
	// (card t_7dcd4280). Nil = no instances; a nil-map copy in
	// WithModel/WithThinkingLevel keeps resolving the same set.
	instances map[string]Provider
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

// SetThinkingLevel is a mutating setter for live toggles; per-run
// derivations go through WithThinkingLevel instead.
func (p *FakeProvider) SetThinkingLevel(level string) { p.lastRequestedLevel = level }

// ThinkingLevel returns the level this provider (or the copy it came
// from) carries. WithThinkingLevel threads the level through the copy
// so tests can assert per-run derivation.
func (p *FakeProvider) ThinkingLevel() string { return p.lastRequestedLevel }

// WithModel returns a copy of the provider serving model. The copy
// shares the scripts so it streams the same sequence.
func (p *FakeProvider) WithModel(model string) Provider {
	cp := *p
	cp.modelName = model
	return &cp
}

// WithThinkingLevel returns a copy of the provider at the given
// thinking level. Same copy discipline as WithModel: shares the
// scripts, leaves the receiver unchanged.
func (p *FakeProvider) WithThinkingLevel(level string) Provider {
	cp := *p
	cp.lastRequestedLevel = level
	return &cp
}

// Instance returns the named instance fake (card t_7dcd4280),
// implementing agent.ProviderInstances for shell tests.
func (p *FakeProvider) Instance(name string) (Provider, bool) {
	inst, ok := p.instances[name]
	return inst, ok
}

// SetInstances installs named instance fakes for the Instance resolver.
// Receiver mutation, so tests call it before wiring the provider in.
func (p *FakeProvider) SetInstances(byName map[string]Provider) {
	p.instances = byName
}

// ListModels returns a hardcoded list for testing.
func (p *FakeProvider) ListModels() ([]string, error) {
	return []string{p.modelName, "other-model"}, nil
}

// ModelInfo returns a fixed context window for testing.
func (p *FakeProvider) ModelInfo() (core.ModelInfo, error) {
	return core.ModelInfo{ContextWindow: 8192}, nil
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
