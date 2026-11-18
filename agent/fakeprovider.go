package agent

import (
	"context"
	"github.com/EndoTheDev/omega/core"
	"time"
)

// FakeProvider implements the agent.Provider face and emits a scripted sequence.
type FakeProvider struct {
	modelName string
	script    []core.ProviderStreamEvent
	scripts   [][]core.ProviderStreamEvent // per-call scripts; the last repeats
	delay     time.Duration
	calls     int
	lastRequestedLevel string
	LastMessages []core.Message
	// instances carries the named fakes the Instance resolver serves. Nil = no instances;
	// a nil-map copy in WithModel/WithThinkingLevel keeps resolving the same set.
	instances map[string]Provider
}

// NewFakeProvider creates a FakeProvider that replays script in order on each Stream.
func NewFakeProvider(model string, script ...core.ProviderStreamEvent) *FakeProvider {
	return &FakeProvider{modelName: model, script: script}
}

// NewFakeProviderScripts creates a FakeProvider that replays each script in order.
func NewFakeProviderScripts(model string, scripts ...[]core.ProviderStreamEvent) *FakeProvider {
	return &FakeProvider{modelName: model, scripts: scripts}
}

// WithDelay returns a copy of the provider that sleeps delay before emitting.
func (p *FakeProvider) WithDelay(delay time.Duration) *FakeProvider {
	cp := *p
	cp.delay = delay
	return &cp
}

// Calls returns the number of Stream calls made. Safe to read after the caller has
// drained a stream channel: the counter increments before the channel closes.
func (p *FakeProvider) Calls() int {
	return p.calls
}

// ModelName returns the model name used by this provider.
func (p *FakeProvider) ModelName() string {
	return p.modelName
}

// SetThinkingLevel is a mutating setter for live toggles.
func (p *FakeProvider) SetThinkingLevel(level string) { p.lastRequestedLevel = level }

// ThinkingLevel returns the level this provider (or the copy it came from) carries.
func (p *FakeProvider) ThinkingLevel() string { return p.lastRequestedLevel }

// WithModel returns a copy of the provider serving model.
func (p *FakeProvider) WithModel(model string) Provider {
	cp := *p
	cp.modelName = model
	return &cp
}

// WithThinkingLevel returns a copy of the provider at the given thinking level.
func (p *FakeProvider) WithThinkingLevel(level string) Provider {
	cp := *p
	cp.lastRequestedLevel = level
	return &cp
}

// Instance returns the named instance fake, implementing agent.ProviderInstances.
func (p *FakeProvider) Instance(name string) (Provider, bool) {
	inst, ok := p.instances[name]
	return inst, ok
}

// SetInstances installs named instance fakes for the Instance resolver.
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

// Stream replays the scripted events on a channel, respecting context cancellation.
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
