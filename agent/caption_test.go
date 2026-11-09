package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// countingCaptioner implements core.Captioner by returning a fixed caption
// per call and counting them, so tests can assert the once-per-image
// discipline.
type countingCaptioner struct {
	calls   int
	caption string
	err     error
}

func (c *countingCaptioner) Caption(ctx context.Context, img core.ImageContent) (string, error) {
	c.calls++
	return c.caption, c.err
}

// recordingLoop is a LoopProvider that captures the messages it was
// handed and streams one assistant response, so tests can inspect what
// the loop would send to the main model.
type recordingLoop struct {
	messages   []core.Message
	streamCall bool
}

func (l *recordingLoop) Run(ctx context.Context, opts LoopOptions) error {
	l.messages = opts.Messages
	streamErr := ""
	for event := range opts.Provider.Stream(ctx, opts.Messages, nil) {
		if e, ok := event.(core.StreamEnd); ok {
			streamErr = e.Error
		}
	}
	l.streamCall = true
	finish := "stop"
	if streamErr != "" {
		finish = "error"
	}
	opts.Events <- core.AgentEnd{Type: "agent_end", FinishReason: finish, Error: streamErr}
	return nil
}

// capsFakeProvider is a FakeProvider that also answers the capability
// upgrade (ModelCapabilitiesProvider) - the shape harness/provider
// implements against the ollama /api/tags "capabilities" array.
type capsFakeProvider struct {
	*FakeProvider
	entries []core.ModelEntry
	err     error
}

func (p *capsFakeProvider) ListModelEntries() ([]core.ModelEntry, error) {
	return p.entries, p.err
}

// WithModel keeps the capability upgrade on the derived copy: the
// production provider copies its whole struct through WithModel and
// the shell hands that per-run copy to RunWithProvider.
func (p *capsFakeProvider) WithModel(model string) Provider {
	cp := *p
	cp.FakeProvider = p.FakeProvider.WithModel(model).(*FakeProvider)
	return &cp
}

// imageUserMessage mirrors the webui shape: user text plus one image.
func imageUserMessage(content string) core.User {
	return core.NewUserWithImages(content, []core.ImageContent{
		{MediaType: "image/png", Base64: "iVBORw0KGgo="},
	})
}

// runOnce drives one RunWithProvider to completion and returns the
// terminal event.
func runOnce(t *testing.T, ag *Agent, messages []core.Message) (core.AgentEnd, *recordingLoop) {
	t.Helper()
	loop := &recordingLoop{}
	ag.SetLoopProvider(loop)
	var end core.AgentEnd
	for event := range ag.Run(context.Background(), messages, nil) {
		if e, ok := event.(core.AgentEnd); ok {
			end = e
		}
	}
	return end, loop
}

// The gate: caption-once, then the main model reasons over captions -
// the loop-visible copy carries "[image: ...]" and no binary images,
// while the caller's original message keeps its images untouched
// (channels persist and re-render the originals).
func TestRunCaptionsImagesBeforeLoop(t *testing.T) {
	ag := NewAgent(NewFakeProvider("main", []core.ProviderStreamEvent{
		core.ResponseChunk{Type: "response_chunk", Content: "ok"},
		core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	}...), nil, 0)
	captioner := &countingCaptioner{caption: "an 8x8 solid red square"}
	ag.SetCaptioner(captioner)

	original := imageUserMessage("what is this?")
	end, loop := runOnce(t, ag, []core.Message{original})
	if end.Error != "" {
		t.Fatalf("run ended with error: %s", end.Error)
	}
	if captioner.calls != 1 {
		t.Fatalf("captioner calls = %d, want 1", captioner.calls)
	}

	// The loop-visible copy: no images, caption as text.
	if len(loop.messages) != 1 {
		t.Fatalf("loop messages = %d, want 1", len(loop.messages))
	}
	u, ok := loop.messages[0].(core.User)
	if !ok {
		t.Fatalf("loop message type = %T, want core.User", loop.messages[0])
	}
	if len(u.Images) != 0 {
		t.Fatalf("loop-visible message still carries %d images", len(u.Images))
	}
	if u.Content != "what is this?\n[image: an 8x8 solid red square]" {
		t.Fatalf("loop-visible content = %q", u.Content)
	}

	// The caller's original is untouched.
	if original.Content != "what is this?" || len(original.Images) != 1 {
		t.Fatalf("caller's message mutated: %q, %d images", original.Content, len(original.Images))
	}
}

// A text-only turn never consults the captioner, even when set.
func TestRunTextOnlySkipsCaptioner(t *testing.T) {
	ag := NewAgent(NewFakeProvider("main", []core.ProviderStreamEvent{
		core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	}...), nil, 0)
	captioner := &countingCaptioner{caption: "unused"}
	ag.SetCaptioner(captioner)

	end, _ := runOnce(t, ag, []core.Message{core.NewUser("just words")})
	if end.Error != "" {
		t.Fatalf("run ended with error: %s", end.Error)
	}
	if captioner.calls != 0 {
		t.Fatalf("captioner calls = %d, want 0", captioner.calls)
	}
}

// vision_model IS set (a captioner installed) but the captioner fails:
// the run ends with a LOUD, captioner-attributed error before any
// streaming - not a provider 400 about unseen pixels.
func TestRunCaptionFailureEndsRun(t *testing.T) {
	ag := NewAgent(NewFakeProvider("main", []core.ProviderStreamEvent{
		core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	}...), nil, 0)
	ag.SetCaptioner(&countingCaptioner{err: context.DeadlineExceeded})
	end, loop := runOnce(t, ag, []core.Message{imageUserMessage("look")})
	if !strings.Contains(end.Error, "image captioner") {
		t.Fatalf("error = %q, want a captioner-attributed error", end.Error)
	}
	if end.FinishReason != "error" {
		t.Fatalf("finish reason = %q, want error", end.FinishReason)
	}
	if loop.streamCall {
		t.Fatal("the loop ran despite the captioning failure")
	}
}

// The loud-400 fallback: vision_model unset (no captioner) means today's
// behavior, byte for byte - images ride to the main provider, which
// surfaces its own refusal.
func TestRunNoCaptionerKeepsImages(t *testing.T) {
	ag := NewAgent(NewFakeProvider("main", []core.ProviderStreamEvent{
		core.StreamEnd{Type: "stream_end", FinishReason: "error", Error: "model does not support images (400)"},
	}...), nil, 0)
	end, loop := runOnce(t, ag, []core.Message{imageUserMessage("look")})
	u, ok := loop.messages[0].(core.User)
	if !ok || len(u.Images) != 1 {
		t.Fatalf("no-captioner fallback must pass images through unchanged, got %#v", loop.messages)
	}
	if !strings.Contains(end.Error, "400") {
		t.Fatalf("error = %q, want the loud provider refusal", end.Error)
	}
}

// visionRegistry builds a capability-carrying provider whose listing
// marks the named model vision-capable - the shape the keypool mirror
// answers for an ollama-family catalog.
func visionRegistry(model string, vision bool) *capsFakeProvider {
	caps := []string{"completion", "tools", "thinking"}
	if vision {
		caps = append(caps, "vision")
	}
	return &capsFakeProvider{
		FakeProvider: NewFakeProvider(model,
			core.ResponseChunk{Type: "response_chunk", Content: "ok"},
			core.StreamEnd{Type: "stream_end", FinishReason: "stop"}),
		entries: []core.ModelEntry{
			{Name: "blind-model", Capabilities: []string{"completion", "tools"}},
			{Name: model, Capabilities: caps},
		},
	}
}

// runOnceOn drives one RunWithProvider on the given provider to
// completion - the shape the shell uses when it hands the per-run
// model copy to the agent.
func runOnceOn(t *testing.T, ag *Agent, p Provider, messages []core.Message) (core.AgentEnd, *recordingLoop) {
	t.Helper()
	loop := &recordingLoop{}
	ag.SetLoopProvider(loop)
	var end core.AgentEnd
	for event := range ag.RunWithProvider(context.Background(), p, messages, nil) {
		if e, ok := event.(core.AgentEnd); ok {
			end = e
		}
	}
	return end, loop
}

// The gate: a vision-capable SELECTED main model receives the pixels
// itself - the auxiliary caption hop never runs, so the image is
// processed once (one provider stream, zero captioner calls), not
// twice. The per-run copy is derived through WithModel exactly as the
// shell derives it.
func TestRunVisionMainModelSkipsCaptionHop(t *testing.T) {
	ag := NewAgent(NewFakeProvider("blind-model"), nil, 0)
	captioner := &countingCaptioner{caption: "unused"}
	ag.SetCaptioner(captioner)

	reg := visionRegistry("vision-main", true)
	runProvider := reg.WithModel("vision-main")
	original := imageUserMessage("what is this?")
	end, loop := runOnceOn(t, ag, runProvider, []core.Message{original})
	if end.Error != "" {
		t.Fatalf("run ended with error: %s", end.Error)
	}
	if captioner.calls != 0 {
		t.Fatalf("captioner calls = %d, want 0 (vision main model skips the aux hop)", captioner.calls)
	}
	if !loop.streamCall {
		t.Fatal("the main model never streamed")
	}
	// Count on the per-run copy: WithModel copies the struct, so the
	// shared parent's counter never moves.
	if calls := runProvider.(*capsFakeProvider).Calls(); calls != 1 {
		t.Fatalf("main provider stream calls = %d, want 1 (one image pass, not two)", calls)
	}
	u, ok := loop.messages[0].(core.User)
	if !ok || len(u.Images) != 1 {
		t.Fatalf("vision main model must receive the pixels, got %#v", loop.messages[0])
	}
	if u.Content != "what is this?" {
		t.Fatalf("loop-visible content = %q, want the original text", u.Content)
	}
}

// Regression: a main model the registry does NOT mark vision-capable
// keeps today's aux behavior - one caption per image, the loop sees
// the description and no binary.
func TestRunBlindMainModelKeepsCaptionHop(t *testing.T) {
	ag := NewAgent(NewFakeProvider("blind-model"), nil, 0)
	captioner := &countingCaptioner{caption: "an 8x8 solid red square"}
	ag.SetCaptioner(captioner)

	reg := visionRegistry("blind-model", false)
	end, loop := runOnceOn(t, ag, reg.WithModel("blind-model"), []core.Message{imageUserMessage("what is this?")})
	if end.Error != "" {
		t.Fatalf("run ended with error: %s", end.Error)
	}
	if captioner.calls != 1 {
		t.Fatalf("captioner calls = %d, want 1 (the aux hop still serves a blind main model)", captioner.calls)
	}
	u, ok := loop.messages[0].(core.User)
	if !ok || len(u.Images) != 0 {
		t.Fatalf("blind main model must not receive pixels, got %#v", loop.messages[0])
	}
	if u.Content != "what is this?\n[image: an 8x8 solid red square]" {
		t.Fatalf("loop-visible content = %q", u.Content)
	}
}

// A capability listing that fails leaves the aux hop in place - the
// pre-existing behavior, never an image turn that silently skips the
// caption for a blind model.
func TestRunCapabilityListingFailureKeepsCaptionHop(t *testing.T) {
	ag := NewAgent(NewFakeProvider("blind-model"), nil, 0)
	captioner := &countingCaptioner{caption: "described"}
	ag.SetCaptioner(captioner)

	reg := visionRegistry("blind-model", false)
	reg.err = errors.New("registry unreachable")
	end, _ := runOnceOn(t, ag, reg.WithModel("blind-model"), []core.Message{imageUserMessage("look")})
	if end.Error != "" {
		t.Fatalf("run ended with error: %s", end.Error)
	}
	if captioner.calls != 1 {
		t.Fatalf("captioner calls = %d, want 1 (listing failure keeps the aux hop)", captioner.calls)
	}
}
