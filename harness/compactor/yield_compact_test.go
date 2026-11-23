package compactor

// The never-compact law: a turn whose tool calls are still pending NEVER compacts.

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// pendingCtx wraps a context the way the loop threads it.
func pendingCtx(sessionID string, ledger agent.YieldLedger) context.Context {
	return agent.WithYieldLedger(agent.WithSessionID(context.Background(), sessionID), ledger)
}

// pendingAnchor builds the assistant message that carries one call.
func pendingAnchor(callID string) core.Assistant {
	a := core.NewAssistant("waiting on the probe")
	a.ToolCalls = []core.ToolCall{{ID: callID, Name: "probe"}}
	return a
}

func summarizingCompactor() *Compactor {
	provider := agent.NewFakeProvider("test",
		core.ResponseChunk{Type: "response_chunk", Content: "summary of the compactable middle"},
		core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	)
	return &Compactor{
		provider: provider,
		config: agent.CompactionConfig{
			Enabled:       true,
			Threshold:     0.6,
			ContextWindow: 10,
			KeepLast:      1,
			ReserveTokens: 1,
		},
	}
}

func pendingHistory() []core.Message {
	a := pendingAnchor("c2")
	return []core.Message{
		core.NewUser("first message long enough to exceed tiny budget"),
		core.NewUser("middle message - compactable"),
		a,
		core.NewUser("last message also long enough"),
	}
}

// compactWire runs the manual arm and rebuilds the wire exactly as the loop does, so the
// pending clamp is judged by the shape a provider would see.
func compactWire(t *testing.T, c *Compactor, ctx context.Context, msgs []core.Message) []core.Message {
	t.Helper()
	start := agent.LiveStart(msgs)
	answer, err := c.CompactNow(ctx, agent.CompactRequest{Wire: msgs, LiveStart: start, Keep: -1})
	if err != nil {
		t.Fatal(err)
	}
	if answer.Summary == "" {
		return msgs
	}
	return agent.CarrierWire(msgs, start, len(msgs)-answer.Keep, answer.Summary, 0, 0)
}

// TestCompactSkipsPendingTurn: the ledger holds c2 pending, so the compact stops at the
// pending turn and the summary rides the tail as a carrier pair.
func TestCompactSkipsPendingTurn(t *testing.T) {
	ledger := agent.NewMemYieldLedger()
	if err := ledger.Register(agent.PendingCall{
		SessionID: "s1", CallID: "c2", ToolName: "probe", DispatchedAt: core.NowISO(),
	}); err != nil {
		t.Fatalf("register pending: %v", err)
	}
	c := summarizingCompactor()
	out := compactWire(t, c, pendingCtx("s1", ledger), pendingHistory())
	if len(out) != 5 {
		t.Fatalf("expected 5 messages (head + carrier pair + pending turn + last), got %d: %s", len(out), messageDump(out))
	}
	if !agent.IsCarrierResult(out[2]) || agent.MessageText(out[2]) != "summary of the compactable middle" {
		t.Fatalf("no summary carrier where the compact boundary sat: %s", messageDump(out))
	}
	as, ok := out[3].(core.Assistant)
	if !ok {
		t.Fatalf("the pending call's assistant turn did not survive the compact: %s", messageDump(out))
	}
	if len(as.ToolCalls) == 0 || as.ToolCalls[0].ID != "c2" {
		t.Fatalf("the surviving turn lost the pending call: %+v", as.ToolCalls)
	}
}

// TestCompactAfterSettle: the same history with the call settled compacts to the kept tail.
func TestCompactAfterSettle(t *testing.T) {
	ledger := agent.NewMemYieldLedger()
	_ = ledger.Register(agent.PendingCall{SessionID: "s1", CallID: "c2", DispatchedAt: core.NowISO()})
	if err := ledger.Settle("s1", "c2"); err != nil {
		t.Fatalf("settle: %v", err)
	}
	c := summarizingCompactor()
	out := compactWire(t, c, pendingCtx("s1", ledger), pendingHistory())
	if len(out) != 4 {
		t.Fatalf("expected 4 messages after settle, got %d: %s", len(out), messageDump(out))
	}
	if !strings.Contains(agent.MessageText(out[3]), "last message") {
		t.Fatalf("the kept tail lost its message: %s", messageDump(out))
	}
}

// TestCompactPendingOtherSession: a pending call belonging to ANOTHER session does not
// fence the compact.
func TestCompactPendingOtherSession(t *testing.T) {
	ledger := agent.NewMemYieldLedger()
	if err := ledger.Register(agent.PendingCall{SessionID: "s2", CallID: "cOther", DispatchedAt: core.NowISO()}); err != nil {
		t.Fatal(err)
	}
	c := summarizingCompactor()
	out := compactWire(t, c, pendingCtx("s1", ledger), pendingHistory())
	if len(out) != 4 {
		t.Fatalf("another session's pending call fenced the compact, got %d: %s", len(out), messageDump(out))
	}
}

// TestCompactFullyPendingHistory: everything beyond the head is under the pending call.
func TestCompactFullyPendingHistory(t *testing.T) {
	ledger := agent.NewMemYieldLedger()
	if err := ledger.Register(agent.PendingCall{SessionID: "s1", CallID: "c2", DispatchedAt: core.NowISO()}); err != nil {
		t.Fatal(err)
	}
	a := pendingAnchor("c2")
	msgs := []core.Message{
		core.NewUser("first message long enough to exceed tiny budget"),
		a,
		core.NewUser("last message also long enough"),
	}
	c := summarizingCompactor()
	out := compactWire(t, c, pendingCtx("s1", ledger), msgs)
	if len(out) != 3 {
		t.Fatalf("a fully-fenced history compacted anyway: %s", messageDump(out))
	}
}

// messageDump renders messages for a failure diagnosis.
func messageDump(msgs []core.Message) string {
	out := ""
	for i, m := range msgs {
		out += fmt.Sprintf("\n  %d: %T %q", i, m, messageContent(m))
	}
	return out
}

func messageContent(m core.Message) string {
	switch v := m.(type) {
	case core.User:
		return v.Content
	case core.Assistant:
		return v.Content
	case core.System:
		return v.Content
	case core.ToolResult:
		return v.Content
	}
	return ""
}
