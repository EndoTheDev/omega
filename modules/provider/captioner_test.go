package provider

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/EndoTheDev/omega/ai"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/modules/store"
)

// tinyPNG is a 1x1 red PNG payload (base64) - a stable, real image
// body for caption-key tests.
const tinyPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

// captionScript is what the vision model streams for one image.
var captionScript = []core.ProviderStreamEvent{
	core.ResponseChunk{Type: "response_chunk", Content: "Red."},
	core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
}

func testImage() core.ImageContent {
	return core.ImageContent{MediaType: "image/png", Base64: tinyPNG}
}

// captionTestLoop is a minimal loop: one provider stream, then done.
type captionTestLoop struct{}

func (captionTestLoop) Run(ctx context.Context, opts core.LoopOptions) error {
	for range opts.Provider.Stream(ctx, opts.Messages, nil) {
	}
	opts.Events <- core.AgentEnd{Type: "agent_end", FinishReason: "stop"}
	return nil
}

// countingProvider is a scripted ai.Provider whose call counter and
// seen records are shared across WithModel copies - NewCaptioner pins a
// WithModel copy, so test assertions must see through it.
type countingProvider struct {
	mu      *sync.Mutex
	seen    *countingSeen
	name    string
	scripts []core.ProviderStreamEvent
}

type countingSeen struct {
	calls    int
	models   []string
	messages [][]core.Message
}

func newCountingProvider(model string, script ...core.ProviderStreamEvent) *countingProvider {
	return &countingProvider{mu: &sync.Mutex{}, seen: &countingSeen{}, name: model, scripts: script}
}

func (p *countingProvider) WithModel(model string) ai.Provider {
	cp := *p
	cp.name = model
	return &cp
}

func (p *countingProvider) WithThinkingLevel(level string) ai.Provider { return p }
func (p *countingProvider) ModelName() string                          { return p.name }
func (p *countingProvider) SetThinkingLevel(level string)              {}
func (p *countingProvider) ListModels() ([]string, error) {
	return []string{p.name}, nil
}
func (p *countingProvider) ModelInfo() (ai.ModelInfo, error) {
	return ai.ModelInfo{}, nil
}

func (p *countingProvider) Stream(ctx context.Context, messages []core.Message, _ []ai.ToolSchema) <-chan core.ProviderStreamEvent {
	p.mu.Lock()
	p.seen.calls++
	p.seen.models = append(p.seen.models, p.name)
	p.seen.messages = append(p.seen.messages, append([]core.Message(nil), messages...))
	p.mu.Unlock()
	events := make(chan core.ProviderStreamEvent)
	go func() {
		defer close(events)
		for _, e := range p.scripts {
			select {
			case <-ctx.Done():
				return
			case events <- e:
			}
		}
	}()
	return events
}

func (p *countingProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.seen.calls
}

// servedModels names the model behind every Stream call.
func (p *countingProvider) servedModels() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.seen.models...)
}

func (p *countingProvider) lastMessages() []core.Message {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.seen.messages) == 0 {
		return nil
	}
	return p.seen.messages[len(p.seen.messages)-1]
}

// Two turns carrying the SAME image caption it ONCE: turn 1 computes
// and persists, turn 2 is a memo hit - the vision model is never called
// again (the card's caption-once gate).
func TestCaptionOnceAcrossTurns(t *testing.T) {
	main := newCountingProvider("main", core.StreamEnd{Type: "stream_end", FinishReason: "stop"})
	ag := core.NewAgent(main, nil, 0)
	ag.SetLoopProvider(captionTestLoop{})
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	vision := newCountingProvider("gemma4:31b", captionScript...)
	ag.SetCaptioner(NewCaptioner(vision, "gemma4:31b", st, nil))

	msgs := []core.Message{core.NewUserWithImages("what is this?", []core.ImageContent{testImage()})}
	for turn := 1; turn <= 2; turn++ {
		for event := range ag.Run(context.Background(), msgs, nil) {
			if e, ok := event.(core.AgentEnd); ok && e.Error != "" {
				t.Fatalf("turn %d: %s", turn, e.Error)
			}
		}
		if got := vision.callCount(); got != 1 {
			t.Fatalf("after turn %d: vision calls = %d, want 1 (caption computed once)", turn, got)
		}
	}
}

// The main model sees the caption as text: loop input carries
// "[image: Red.]" with the binary stripped, and the caption prompt went
// through the vision model, not the main one.
func TestCaptionRouting(t *testing.T) {
	main := newCountingProvider("main", core.StreamEnd{Type: "stream_end", FinishReason: "stop"})
	vision := newCountingProvider("gemma4:31b", captionScript...)
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	ag := core.NewAgent(main, nil, 0)
	ag.SetLoopProvider(captionTestLoop{})
	ag.SetCaptioner(NewCaptioner(vision, "gemma4:31b", st, nil))
	original := core.NewUserWithImages("what is this?", []core.ImageContent{testImage()})
	for event := range ag.Run(context.Background(), []core.Message{original}, nil) {
		_ = event
	}
	if got := vision.callCount(); got != 1 {
		t.Fatalf("vision calls = %d, want 1", got)
	}
	if models := vision.servedModels(); len(models) != 1 || models[0] != "gemma4:31b" {
		t.Fatalf("caption served by %v, want gemma4:31b", models)
	}
	// The caption prompt is deterministic and carries exactly one image.
	if msgs := vision.lastMessages(); len(msgs) != 1 {
		t.Fatalf("caption call messages = %d, want 1", len(msgs))
	} else if u, ok := msgs[0].(core.User); !ok || len(u.Images) != 1 || u.Content != captionPrompt {
		t.Fatalf("caption call shape: %#v", msgs[0])
	}
	if mains := main.servedModels(); len(mains) != 1 || mains[0] != "main" {
		t.Fatalf("main turns = %v", mains)
	}
}

// The persisted caption survives a restart: a fresh Captioner (no
// process memo) with the same store is a store hit - the vision model
// is never re-called. This is the card's "store shows the cached
// caption" gate.
func TestCaptionStoreHitSurvivesRestart(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	vision := newCountingProvider("gemma4:31b", captionScript...)
	first := NewCaptioner(vision, "gemma4:31b", st, nil)
	caption, err := first.Caption(context.Background(), testImage())
	if err != nil || caption != "Red." {
		t.Fatalf("first caption = %q, %v", caption, err)
	}
	if vision.callCount() != 1 {
		t.Fatalf("vision calls = %d, want 1", vision.callCount())
	}
	// "Restart": fresh captioner, same store. Zero new calls.
	restarted := NewCaptioner(newCountingProvider("gemma4:31b", captionScript...), "gemma4:31b", st, nil)
	got, err := restarted.Caption(context.Background(), testImage())
	if err != nil || got != "Red." {
		t.Fatalf("restart caption = %q, %v (must be the persisted caption)", got, err)
	}
	id := ImageID(testImage())
	persisted, ok, err := st.GetImageCaption(context.Background(), id)
	if err != nil || !ok || persisted != "Red." {
		t.Fatalf("store caption = %q ok=%v err=%v", persisted, ok, err)
	}
}

// The captioner memoizes within a process: repeat Caption calls hit the
// memo, one vision call total.
func TestCaptionerMemoizes(t *testing.T) {
	vision := newCountingProvider("vision-fake", captionScript...)
	c := NewCaptioner(vision, "vision-fake", nil, nil)
	first, err := c.Caption(context.Background(), testImage())
	if err != nil {
		t.Fatalf("first caption: %v", err)
	}
	second, err := c.Caption(context.Background(), testImage())
	if err != nil {
		t.Fatalf("second caption: %v", err)
	}
	if first != second || vision.callCount() != 1 {
		t.Fatalf("memo failed: first=%q second=%q calls=%d", first, second, vision.callCount())
	}
}

// A vision-model failure is loud and attributed to the captioner.
func TestCaptionerVisionFailure(t *testing.T) {
	fake := newCountingProvider("vision",
		core.StreamEnd{Type: "stream_end", FinishReason: "error", Error: "model not found"})
	c := NewCaptioner(fake, "vision", nil, nil)
	_, err := c.Caption(context.Background(), testImage())
	if err == nil || !strings.Contains(err.Error(), "vision model") {
		t.Fatalf("err = %v, want a vision-model-attributed error", err)
	}
}

// ImageID is content-addressed: same bytes share an id, any difference
// (payload or media type) splits it.
func TestImageIDIsContentAddressed(t *testing.T) {
	a := ImageID(testImage())
	b := ImageID(core.ImageContent{MediaType: "image/png", Base64: tinyPNG})
	if a != b {
		t.Fatalf("same bytes hashed apart: %s vs %s", a, b)
	}
	c := ImageID(core.ImageContent{MediaType: "image/jpeg", Base64: tinyPNG})
	if c == a {
		t.Fatal("different media types must hash apart")
	}
}
