package main

// loop_compact_test.go: the CLI loop's half of the /compact contract. A capsule declares
// the intent with no text; the loop owns the session, so it must run the compact the HTTP
// dispatch runs - and print the SAME line (agent.CompactLine), or say why not. Silence is
// the defect this path was carded for, so every branch here asserts a printed line.

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/store"
)

// loopCompactor is the capsule's compact face, answering a fixed summary.
type loopCompactor struct {
	answer agent.CompactAnswer
}

func (f *loopCompactor) PlanCompact(context.Context, agent.CompactRequest) agent.CompactPlan {
	return agent.CompactPlan{Fire: true, Keep: f.answer.Keep}
}

func (f *loopCompactor) CompactNow(context.Context, agent.CompactRequest) (agent.CompactAnswer, error) {
	return f.answer, nil
}

// newLoopForCommands builds a loop with a real store and the given command surface.
func newLoopForCommands(t *testing.T, commands []string, handler func(ctx context.Context, name, args string) (core.CommandResult, error)) (*cliLoop, *bytes.Buffer) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	pctx := &agent.Context{CommandHandler: handler}
	for _, name := range commands {
		pctx.Commands = append(pctx.Commands, core.ExtensionCommand{Name: name})
	}
	out := &bytes.Buffer{}
	lo := &cliLoop{store: st, pctx: pctx, out: bufio.NewWriter(out)}
	return lo, out
}

// TestLoopCommandDispatchBindsTheSession pins the first half of the defect: the loop's
// session rides the dispatch context, so a session-scoped command can resolve it instead
// of demanding the id in args (the same context the HTTP palette builds).
func TestLoopCommandDispatchBindsTheSession(t *testing.T) {
	var seen string
	lo, out := newLoopForCommands(t, []string{"/compact"}, func(ctx context.Context, name, args string) (core.CommandResult, error) {
		seen = core.CommandSession(ctx)
		return core.CommandResult{Text: "bound=" + seen + " args=" + args}, nil
	})
	lo.sessionID = "sess-9"

	if err := lo.commandExtension("/compact", "now"); err != nil {
		t.Fatalf("commandExtension: %v", err)
	}
	if seen != "sess-9" {
		t.Fatalf("handler saw session %q, want the loop's own session", seen)
	}
	if !strings.Contains(out.String(), "bound=sess-9 args=now") {
		t.Fatalf("output = %q, want the handler's text", out.String())
	}
}

// TestLoopCompactRunsTheCompaction is the card's law: a run_compact action is executed by the
// loop, the store records the block and its carrier, the printed line names them, and the
// loop's OWN in-memory context is rebuilt from the derived wire (it sends that list itself).
func TestLoopCompactRunsTheCompaction(t *testing.T) {
	ctx := context.Background()
	lo, out := newLoopForCommands(t, []string{"/compact"}, func(context.Context, string, string) (core.CommandResult, error) {
		return core.CommandResult{Actions: []core.CmdAction{{Type: core.CmdRunCompact}}}, nil
	})
	st := lo.store.(agent.StoreProvider)
	if err := st.CreateSession(ctx, "s-1", "", "work"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	for _, row := range []string{"one", "two", "three", "four", "five"} {
		if err := st.AppendMessage(ctx, "s-1", core.NewUser(row)); err != nil {
			t.Fatalf("append %q: %v", row, err)
		}
	}
	lo.sessionID = "s-1"
	lo.pctx.ApplyOffer("compactor", "compactor", &loopCompactor{
		answer: agent.CompactAnswer{Summary: "the middle of it", Keep: 1},
	})

	if err := lo.commandExtension("/compact", ""); err != nil {
		t.Fatalf("commandExtension: %v", err)
	}
	blocks, err := lo.store.(agent.SessionBlockStore).ContextBlocks(ctx, "s-1")
	if err != nil {
		t.Fatalf("blocks: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("blocks = %d, want the one compact", len(blocks))
	}
	want := fmt.Sprintf("compacted 3 messages into a summary carried by compress-%d - the last 1 stays verbatim", blocks[0].Call)
	if !strings.Contains(out.String(), want) {
		t.Fatalf("output = %q, want %q", out.String(), want)
	}
	// The loop sends its own history: a compact that did not rebuild it would be a placebo.
	if len(lo.history) != 4 {
		t.Fatalf("loop context = %d messages, want 4 (head + carrier pair + kept tail)", len(lo.history))
	}
	if !agent.IsCarrierResult(lo.history[2]) || agent.MessageText(lo.history[2]) != "the middle of it" {
		t.Fatalf("loop context carrier = %q, want the summary", agent.MessageText(lo.history[2]))
	}
	// The transcript keeps every row - a compact moves the context, it never deletes.
	msgs, err := st.GetMessages(ctx, "s-1")
	if err != nil {
		t.Fatalf("messages: %v", err)
	}
	if len(msgs) != 7 {
		t.Fatalf("transcript = %d rows, want all 5 kept plus the carrier pair", len(msgs))
	}
}

// TestLoopCompactWithoutASessionIsHonest pins the refusal instead of silence.
func TestLoopCompactWithoutASessionIsHonest(t *testing.T) {
	lo, out := newLoopForCommands(t, []string{"/compact"}, func(context.Context, string, string) (core.CommandResult, error) {
		return core.CommandResult{Actions: []core.CmdAction{{Type: core.CmdRunCompact}}}, nil
	})
	if err := lo.commandExtension("/compact", ""); err != nil {
		t.Fatalf("commandExtension: %v", err)
	}
	if want := "nothing compacted: no session is bound to this chat yet"; !strings.Contains(out.String(), want) {
		t.Fatalf("output = %q, want %q", out.String(), want)
	}
}

// TestLoopActionWithNoExecutorSaysSo pins the general case: an action this surface cannot
// run is named, never swallowed.
func TestLoopActionWithNoExecutorSaysSo(t *testing.T) {
	lo, out := newLoopForCommands(t, []string{"/newsession"}, func(context.Context, string, string) (core.CommandResult, error) {
		return core.CommandResult{Actions: []core.CmdAction{{Type: "new_session"}}}, nil
	})
	if err := lo.commandExtension("/newsession", ""); err != nil {
		t.Fatalf("commandExtension: %v", err)
	}
	if want := "new_session: this surface has no executor - nothing ran"; !strings.Contains(out.String(), want) {
		t.Fatalf("output = %q, want %q", out.String(), want)
	}
}
