package verdict

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/agent"
)

// toolRun runs request_verdict in a goroutine (it blocks on the hub)
// and returns a channel for the result string / error.
func toolRun(t *testing.T, p *verdictToolProvider, ctx context.Context, args map[string]any) <-chan toolResult {
	t.Helper()
	ch := make(chan toolResult, 1)
	go func() {
		s, err := p.Tools()["request_verdict"].Run(ctx, args)
		ch <- toolResult{s: s, err: err}
	}()
	return ch
}

type toolResult struct {
	s   string
	err error
}

func drainTool(t *testing.T, ch <-chan toolResult) toolResult {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(2 * time.Second):
		t.Fatal("tool did not return within 2s")
		return toolResult{}
	}
}

func interactiveCtx(session string) context.Context {
	ctx := context.Background()
	ctx = agent.WithRunOrigin(ctx, agent.RunOriginInteractive)
	if session != "" {
		ctx = agent.WithSessionID(ctx, session)
	}
	return ctx
}

func TestToolRefusesBackgroundOrigins(t *testing.T) {
	h := NewHub(nil, nil)
	p := &verdictToolProvider{hub: h}
	args := map[string]any{"title": "q", "options": []any{"y"}}

	for _, origin := range []string{agent.RunOriginCron, agent.RunOriginDelegate} {
		ctx := agent.WithRunOrigin(context.Background(), origin)
		_, err := p.Tools()["request_verdict"].Run(ctx, args)
		if err == nil {
			t.Fatalf("origin %q must refuse request_verdict", origin)
		}
	}
}

func TestToolRequiresTitleAndShape(t *testing.T) {
	h := NewHub(nil, nil)
	p := &verdictToolProvider{hub: h}

	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"no title", map[string]any{"options": []any{"y"}}, "title is required"},
		{"blank title", map[string]any{"title": "   ", "options": []any{"y"}}, "title is required"},
		{"no options no freetext", map[string]any{"title": "q"}, "at least one option or free_text"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := p.Tools()["request_verdict"].Run(interactiveCtx("s1"), tc.args)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !containsStr(err.Error(), tc.want) {
				t.Fatalf("err = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestToolBlocksAndReturnsVerdictJSON(t *testing.T) {
	h := NewHub(nil, nil)
	p := &verdictToolProvider{hub: h}
	args := map[string]any{"title": "Approve?", "body": "ship it", "options": []any{"yes", "no"}}
	ch := toolRun(t, p, interactiveCtx("s1"), args)

	// the card is pending before the answer resolves the block.
	cards := waitForPending(t, h, "s1", 1)
	if cards[0].Title != "Approve?" {
		t.Fatalf("Pending = %+v, want one Approve? card", cards)
	}
	if _, err := h.Answer("s1", "", agent.Verdict{OptionIndex: 1}); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	r := drainTool(t, ch)
	if r.err != nil {
		t.Fatalf("tool error: %v", r.err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(r.s), &got); err != nil {
		t.Fatalf("tool result not JSON: %v (raw: %q)", err, r.s)
	}
	if got["verdict"] != "answer" || got["option"] != "yes" || got["option_index"] != float64(1) {
		t.Fatalf("tool result = %v, want verdict=answer option=yes option_index=1", got)
	}
}

func TestToolTimeoutFloorClampsTinyOverride(t *testing.T) {
	h := NewHub(nil, nil)
	p := &verdictToolProvider{hub: h}
	args := map[string]any{"title": "q", "options": []any{"y"}, "timeout_s": float64(2)}
	ch := toolRun(t, p, interactiveCtx("s1"), args)
	cards := waitForPending(t, h, "s1", 1)
	if cards[0].TimeoutS != minTimeout {
		t.Fatalf("clamped TimeoutS = %d, want %d (the floor)", cards[0].TimeoutS, minTimeout)
	}
	_, _ = h.Answer("s1", cards[0].ID, agent.Verdict{OptionIndex: 1})
	_ = drainTool(t, ch)
}

func TestToolFreeTextOnlyCard(t *testing.T) {
	h := NewHub(nil, nil)
	p := &verdictToolProvider{hub: h}
	args := map[string]any{"title": "Name it", "free_text": true}
	ch := toolRun(t, p, interactiveCtx("s1"), args)
	waitForPending(t, h, "s1", 1)
	_, _ = h.Answer("s1", "", agent.Verdict{Text: "omega"})
	r := drainTool(t, ch)
	if r.err != nil {
		t.Fatalf("tool error: %v", r.err)
	}
	var got map[string]any
	_ = json.Unmarshal([]byte(r.s), &got)
	if got["verdict"] != "answer" || got["text"] != "omega" {
		t.Fatalf("tool result = %v, want verdict=answer text=omega", got)
	}
}

func TestToolRefusesWithoutSeam(t *testing.T) {
	p := &verdictToolProvider{hub: nil}
	_, err := p.Tools()["request_verdict"].Run(interactiveCtx("s1"), map[string]any{"title": "q", "options": []any{"y"}})
	if err == nil {
		t.Fatal("tool must refuse when the seam is not mounted")
	}
}