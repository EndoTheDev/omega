package store

// blocks_test.go: the context's tier structure. A compact is one append - carrier pair plus
// the block line - and it is never aged out: the tier model has no age-GC, because age-based
// deactivation lost memory in the source design.

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// seedRows appends n user rows and answers their store ids.
func seedRows(t *testing.T, s *Store, sessionID string, n int) []int64 {
	t.Helper()
	ctx := context.Background()
	ids := make([]int64, 0, n)
	for i := 0; i < n; i++ {
		if err := s.AppendMessage(ctx, sessionID, core.NewUser("row")); err != nil {
			t.Fatalf("append row %d: %v", i, err)
		}
		ids = append(ids, 0)
	}
	rows, err := s.GetMessagesWithTimes(ctx, sessionID)
	if err != nil {
		t.Fatalf("rows: %v", err)
	}
	for i := range rows {
		if i < n {
			ids[i] = rows[i].ID
		}
	}
	return ids
}

// TestAppendCompactRoundTrip pins the compact's one append: the carrier pair lands on the
// active path, the block names it with its lineage, and both survive a reopen.
func TestAppendCompactRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/blocks.db"
	s, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ctx := context.Background()
	if err := s.CreateSession(ctx, "s1", "", "work"); err != nil {
		t.Fatalf("create: %v", err)
	}
	ids := seedRows(t, s, "s1", 4)

	block, err := s.AppendCompact(ctx, "s1", core.CompactWrite{
		Summary: "the middle of it", From: ids[1], To: ids[3], Keep: ids[3], Tier: 1,
	})
	if err != nil {
		t.Fatalf("append compact: %v", err)
	}
	if block.ID == "" || block.Call == 0 || block.Carrier == 0 || block.Tier != 1 {
		t.Fatalf("block = %+v, want an id, both carrier ids and tier 1", block)
	}
	view, err := s.GetActiveView(ctx, "s1")
	if err != nil {
		t.Fatalf("view: %v", err)
	}
	if len(view.Path) != len(ids)+2 {
		t.Fatalf("active path = %d rows, want the %d originals plus the carrier pair", len(view.Path), len(ids))
	}
	call, ok := view.Path[len(ids)].Message.(core.Assistant)
	if !ok || len(call.ToolCalls) != 1 || call.ToolCalls[0].Name != core.CompressToolName {
		t.Fatalf("row after the transcript = %#v, want the compress call", view.Path[len(ids)].Message)
	}
	result, ok := view.Path[len(ids)+1].Message.(core.ToolResult)
	if !ok || result.Content != "the middle of it" || result.ToolCallID != call.ToolCalls[0].ID {
		t.Fatalf("carrier = %#v, want the summary answering the call", view.Path[len(ids)+1].Message)
	}

	s.Close()
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	blocks, err := s2.ContextBlocks(ctx, "s1")
	if err != nil {
		t.Fatalf("blocks after reopen: %v", err)
	}
	if len(blocks) != 1 || blocks[0].ID != block.ID || blocks[0].Carrier != block.Carrier {
		t.Fatalf("blocks after reopen = %+v, want the one compact intact", blocks)
	}
	// The derived wire reads back the same shape after a restart.
	wire, err := agent.SessionContext(ctx, s2, "s1")
	if err != nil {
		t.Fatalf("context: %v", err)
	}
	if len(wire) != 4 {
		t.Fatalf("wire = %d messages, want head + carrier pair + kept tail", len(wire))
	}
}

// TestAppendCompactWritesOneGroup pins the atomicity: the carrier pair and the block line
// are one append, so a reader never sees half a compact.
func TestAppendCompactWritesOneGroup(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, "s1", "", "work"); err != nil {
		t.Fatalf("create: %v", err)
	}
	seedRows(t, s, "s1", 2)
	file := s.sessionsDir + "/s1.jsonl"
	before, err := os.Stat(file)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if _, err := s.AppendCompact(ctx, "s1", core.CompactWrite{Summary: "s", From: 1, To: 2, Keep: 2, Tier: 1}); err != nil {
		t.Fatalf("append compact: %v", err)
	}
	after, err := os.Stat(file)
	if err != nil {
		t.Fatalf("stat after: %v", err)
	}
	body, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	lines := strings.Count(string(body[before.Size():after.Size()]), "\n")
	if lines != 3 {
		t.Fatalf("the compact appended %d lines, want 3 (call, result, block)", lines)
	}
}

// TestAppendCompactNeedsASession pins the refusal: a compact on a session that does not
// exist writes nothing and says so.
func TestAppendCompactNeedsASession(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.AppendCompact(context.Background(), "ghost", core.CompactWrite{Summary: "s"}); err == nil {
		t.Fatal("a compact on a missing session answered nil, want an error")
	}
}

// TestAncientBlockIsNeverAgedOut is the no-age-GC law: a block written years ago still
// governs the wire. Age-based deactivation lost memory in the source design, so the tier
// model never retires a block by its timestamp.
func TestAncientBlockIsNeverAgedOut(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, "old", "", "old work"); err != nil {
		t.Fatalf("create: %v", err)
	}
	ids := seedRows(t, s, "old", 4)
	if _, err := s.AppendCompact(ctx, "old", core.CompactWrite{
		Summary: "ancient context", From: ids[1], To: ids[3], Keep: ids[3], Tier: 1,
	}); err != nil {
		t.Fatalf("append compact: %v", err)
	}
	// Rewrite the block's stamp to the past: the store must carry it as data, never judge it.
	blocks, err := s.ContextBlocks(ctx, "old")
	if err != nil || len(blocks) != 1 {
		t.Fatalf("blocks = %+v, err %v", blocks, err)
	}
	blocks[0].At = "2001-01-01T00:00:00Z"
	wire := agent.DeriveWire(mustRows(t, s, "old"), blocks, core.Session{})
	if len(wire) != 4 {
		t.Fatalf("an ancient block was dropped from the wire: %d messages", len(wire))
	}
	if msg := agent.MessageText(wire[2]); msg != "ancient context" {
		t.Fatalf("carrier = %q, want the ancient block's summary", msg)
	}
}

// mustRows reads a session's stored rows.
func mustRows(t *testing.T, s *Store, sessionID string) []core.StoredMessage {
	t.Helper()
	rows, err := s.GetMessagesWithTimes(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("rows: %v", err)
	}
	return rows
}

// TestStoreTurnSinkInstallsTheCompactRecorder pins the run's durable compact seam: a
// store-backed turn sink installs the recorder, and what it records is the span the store's
// own derivation drops - so an in-run compact leaves no divergence behind.
func TestStoreTurnSinkInstallsTheCompactRecorder(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, "s1", "", "work"); err != nil {
		t.Fatalf("create: %v", err)
	}
	ids := seedRows(t, s, "s1", 5)

	runCtx := agent.WithTurnSink(ctx, agent.NewStoreTurnSink(s, "s1", ""))
	rec := agent.CompactRecorderFromContext(runCtx)
	if rec == nil {
		t.Fatal("a store-backed turn sink did not install the compact recorder")
	}
	block, err := rec.RecordCompact(runCtx, "the middle", 1)
	if err != nil {
		t.Fatalf("record compact: %v", err)
	}
	if block.From != ids[1] || block.To != ids[4] || block.Keep != ids[4] {
		t.Fatalf("block = %+v, want the span %d..%d keeping %d", block, ids[1], ids[4], ids[4])
	}
	wire, err := agent.SessionContext(ctx, s, "s1")
	if err != nil {
		t.Fatalf("context: %v", err)
	}
	if len(wire) != 4 {
		t.Fatalf("wire = %d messages, want head + carrier pair + kept tail", len(wire))
	}
	if !agent.IsCarrierResult(wire[2]) || agent.MessageText(wire[2]) != "the middle" {
		t.Fatalf("carrier = %q, want the recorded summary", agent.MessageText(wire[2]))
	}
}

// TestCompactRecorderRefusesAnEmptySpan pins the guard: a run whose store wire moved on
// since the compactor decided gets an error it can ignore, never a block that misnames its
// span.
func TestCompactRecorderRefusesAnEmptySpan(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, "s1", "", "work"); err != nil {
		t.Fatalf("create: %v", err)
	}
	seedRows(t, s, "s1", 2)
	runCtx := agent.WithTurnSink(ctx, agent.NewStoreTurnSink(s, "s1", ""))
	rec := agent.CompactRecorderFromContext(runCtx)
	if _, err := rec.RecordCompact(runCtx, "s", 1); err == nil {
		t.Fatal("a keep of 1 over a 2-row wire has no compactable span, want an error")
	}
	blocks, err := s.ContextBlocks(ctx, "s1")
	if err != nil {
		t.Fatalf("blocks: %v", err)
	}
	if len(blocks) != 0 {
		t.Fatalf("a refused record wrote %d blocks", len(blocks))
	}
}
