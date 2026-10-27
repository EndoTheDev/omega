package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/engine/ai"
)

// countingCaptioner implements Captioner by returning a fixed caption
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

func (l *recordingLoop) Run(ctx context.Context, opts core.LoopOptions) error {
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

// imageUserMessage mirrors the webui shape: user text plus one image.
func imageUserMessage(content string) core.User {
	return core.NewUserWithImages(content, []core.ImageContent{
		{MediaType: "image/png", Base64: "iVBORw0KGgo="},
	})
}

// runOnce drives one RunWithProvider to completion and returns the
// terminal event.
func runOnce(t *testing.T, ag *core.Agent, messages []core.Message) (core.AgentEnd, *recordingLoop) {
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
	ag := core.NewAgent(ai.NewFakeProvider("main", []core.ProviderStreamEvent{
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
	ag := core.NewAgent(ai.NewFakeProvider("main", []core.ProviderStreamEvent{
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
	ag := core.NewAgent(ai.NewFakeProvider("main", []core.ProviderStreamEvent{
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
	ag := core.NewAgent(ai.NewFakeProvider("main", []core.ProviderStreamEvent{
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
