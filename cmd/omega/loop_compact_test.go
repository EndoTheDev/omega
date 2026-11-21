package main

// loop_compact_test.go: the CLI loop's half of the /compact contract. A capsule declares
// the intent with no text; the loop owns the session, so it must run the compact the HTTP
// dispatch runs - and print the SAME line (agent.CompactLine), or say why not. Silence is
// the defect this path was carded for, so every branch here asserts a printed line.

import (
	"bufio"
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/store"
)

// loopCompactor is the capsule's manual compact face, answering a fixed compact.
type loopCompactor struct {
	out []core.Message
}

func (f *loopCompactor) CompactNow(context.Context, []core.Message) ([]core.Message, error) {
	return f.out, nil
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
// loop, the store's boundary + summary move, the printed line names them, and the loop's
// OWN in-memory context is rebuilt on the new boundary (it sends that list itself).
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
	view, err := st.GetActiveView(ctx, "s-1")
	if err != nil {
		t.Fatalf("active view: %v", err)
	}
	wantStart := core.MsgID(view.Path[4].ID)
	lo.sessionID = "s-1"
	lo.pctx.ApplyOffer("compactor", "compactor", &loopCompactor{out: []core.Message{
		core.NewSystem(core.ContextCompactPrefix + "the middle of it" + core.ContextCompactSuffix),
		core.NewUser("five"),
	}})

	if err := lo.commandExtension("/compact", ""); err != nil {
		t.Fatalf("commandExtension: %v", err)
	}
	want := "folded 4 messages into a summary - the context now starts at message " + wantStart + ", and the last 1 stay verbatim"
	if !strings.Contains(out.String(), want) {
		t.Fatalf("output = %q, want %q", out.String(), want)
	}
	row, err := st.GetSession(ctx, "s-1")
	if err != nil {
		t.Fatalf("session row: %v", err)
	}
	if row.ContextStart != wantStart || row.ContextSummary != "the middle of it" {
		t.Fatalf("boundary = (%q, %q), want (%q, the middle of it)", row.ContextStart, row.ContextSummary, wantStart)
	}
	// The loop sends its own history: a compact that did not rebuild it would be a placebo.
	if len(lo.history) != 2 {
		t.Fatalf("loop context = %d rows, want the summary plus the kept tail (2)", len(lo.history))
	}
	if _, ok := lo.history[0].(core.System); !ok {
		t.Fatalf("loop context head = %#v, want the compact summary", lo.history[0])
	}
	// The transcript keeps every row - a compact moves the boundary, it never deletes.
	msgs, err := st.GetMessages(ctx, "s-1")
	if err != nil {
		t.Fatalf("messages: %v", err)
	}
	if len(msgs) != 5 {
		t.Fatalf("transcript = %d rows, want all 5 kept", len(msgs))
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
	if want := "nothing folded: no session is bound to this chat yet"; !strings.Contains(out.String(), want) {
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
