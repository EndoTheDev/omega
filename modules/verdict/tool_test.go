package verdict

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/engine/agent"
)

// toolRun runs the verdict tool in a goroutine (the request sub-action
// blocks on the hub) and returns a channel for the result string / error.
func toolRun(t *testing.T, p *verdictToolProvider, ctx context.Context, args map[string]any) <-chan toolResult {
	t.Helper()
	ch := make(chan toolResult, 1)
	go func() {
		s, err := p.Tools()["verdict"].Run(ctx, args)
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
	ctx = core.WithRunOrigin(ctx, core.RunOriginInteractive)
	if session != "" {
		ctx = agent.WithSessionID(ctx, session)
	}
	return ctx
}

// requestArgs is a valid verdict args map with the extras
// merged in.
func requestArgs(extra map[string]any) map[string]any {
	args := map[string]any{"action": "request", "title": "q"}
	for k, v := range extra {
		args[k] = v
	}
	return args
}

func TestToolRefusesBackgroundOrigins(t *testing.T) {
	h := NewHub(nil, nil)
	p := &verdictToolProvider{hub: h}
	args := requestArgs(map[string]any{"options": []any{"y"}})

	for _, origin := range []string{core.RunOriginCron, core.RunOriginDelegate} {
		ctx := core.WithRunOrigin(context.Background(), origin)
		_, err := p.Tools()["verdict"].Run(ctx, args)
		if err == nil {
			t.Fatalf("origin %q must refuse the verdict tool", origin)
		}
	}
}

func TestToolRequiresAction(t *testing.T) {
	h := NewHub(nil, nil)
	p := &verdictToolProvider{hub: h}
	for name, args := range map[string]map[string]any{
		"no action":      {"title": "q"},
		"unknown action": {"action": "status", "title": "q"},
	} {
		_, err := p.Tools()["verdict"].Run(interactiveCtx("s1"), args)
		if err == nil || !containsStr(err.Error(), "action is required") {
			t.Fatalf("%s: err = %v, want 'action is required'", name, err)
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
		{"no title", map[string]any{"action": "request", "options": []any{"y"}}, "title is required"},
		{"blank title", requestArgs(map[string]any{"title": "   ", "options": []any{"y"}}), "title is required"},
		{"no options no allowtext", requestArgs(nil), "at least one option or allow_text"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := p.Tools()["verdict"].Run(interactiveCtx("s1"), tc.args)
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
	args := requestArgs(map[string]any{"title": "Approve?", "body": "ship it", "options": []any{"yes", "no"}})
	ch := toolRun(t, p, interactiveCtx("s1"), args)

	// the card is pending before the answer resolves the block.
	cards := waitForPending(t, h, "s1", 1)
	if cards[0].Title != "Approve?" {
		t.Fatalf("Pending = %+v, want one Approve? card", cards)
	}
	if _, err := h.Answer("s1", "", core.Verdict{OptionIndex: 1}); err != nil {
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

func TestToolParsesDescribedOptionsAndMulti(t *testing.T) {
	h := NewHub(nil, nil)
	p := &verdictToolProvider{hub: h}
	args := requestArgs(map[string]any{
		"title": "Which lanes?",
		"options": []any{
			map[string]any{"label": "Ship", "description": "merge now"},
			map[string]any{"label": "Hold"},
			// plain strings stay legal (one-word rows)
			"Stop",
		},
		"multi":      true,
		"allow_text": true,
	})
	ch := toolRun(t, p, interactiveCtx("s1"), args)
	cards := waitForPending(t, h, "s1", 1)
	if len(cards[0].Options) != 3 {
		t.Fatalf("parsed options = %+v, want 3 rows", cards[0].Options)
	}
	if cards[0].Options[0].Label != "Ship" || cards[0].Options[0].Description != "merge now" {
		t.Fatalf("first option = %+v, want label Ship description merge-now", cards[0].Options[0])
	}
	if cards[0].Options[2].Label != "Stop" {
		t.Fatalf("string pick = %+v, want a label-only Stop row", cards[0].Options[2])
	}
	if !cards[0].Multi || !cards[0].AllowText {
		t.Fatalf("card flags multi=%v allow_text=%v, want both true", cards[0].Multi, cards[0].AllowText)
	}
	if _, err := h.Answer("s1", cards[0].ID, core.Verdict{OptionIndexes: []int{1, 3}}); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	r := drainTool(t, ch)
	if r.err != nil {
		t.Fatalf("tool error: %v", r.err)
	}
	var got map[string]any
	_ = json.Unmarshal([]byte(r.s), &got)
	ixs, ok := got["option_indexes"].([]any)
	if !ok || len(ixs) != 2 || ixs[0] != float64(1) || ixs[1] != float64(3) {
		t.Fatalf("tool result indexes = %v, want [1 3]", got["option_indexes"])
	}
	opts, _ := got["options"].([]any)
	if len(opts) != 2 || opts[0] != "Ship" || opts[1] != "Stop" {
		t.Fatalf("tool result labels = %v, want [Ship Stop]", got["options"])
	}
}

func TestToolTimeoutFloorClampsTinyOverride(t *testing.T) {
	h := NewHub(nil, nil)
	p := &verdictToolProvider{hub: h}
	args := requestArgs(map[string]any{"options": []any{"y"}, "timeout_s": float64(2)})
	ch := toolRun(t, p, interactiveCtx("s1"), args)
	cards := waitForPending(t, h, "s1", 1)
	if cards[0].TimeoutS != minTimeout {
		t.Fatalf("clamped TimeoutS = %d, want %d (the floor)", cards[0].TimeoutS, minTimeout)
	}
	_, _ = h.Answer("s1", cards[0].ID, core.Verdict{OptionIndex: 1})
	_ = drainTool(t, ch)
}

func TestToolAllowTextOnlyCard(t *testing.T) {
	h := NewHub(nil, nil)
	p := &verdictToolProvider{hub: h}
	args := requestArgs(map[string]any{"title": "Name it", "allow_text": true})
	ch := toolRun(t, p, interactiveCtx("s1"), args)
	waitForPending(t, h, "s1", 1)
	_, _ = h.Answer("s1", "", core.Verdict{Text: "omega"})
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
	_, err := p.Tools()["verdict"].Run(interactiveCtx("s1"), requestArgs(map[string]any{"options": []any{"y"}}))
	if err == nil {
		t.Fatal("tool must refuse when the seam is not mounted")
	}
}
