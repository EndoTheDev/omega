package agent

// sessioncompact_test.go: the context model's pins. A compact appends a compress call and
// its result at the TAIL; the wire is derived from the blocks; nothing before the carrier
// changes bytes. One implementation, one wording, both hosts.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// compactStore is the session shape a compact needs: a session row, an active path, the
// blocks already recorded, and a recorder for the append. A refusal must write NOTHING, so
// every test can prove that from writes.
type compactStore struct {
	fakeStore
	row    core.Session
	path   []core.StoredMessage
	blocks []core.ContextBlock
	writes []core.CompactWrite
}

func (s *compactStore) GetSession(context.Context, string) (core.Session, error) { return s.row, nil }
func (s *compactStore) GetActiveView(context.Context, string) (core.ActiveView, error) {
	return core.ActiveView{Path: s.path}, nil
}
func (s *compactStore) ContextBlocks(context.Context, string) ([]core.ContextBlock, error) {
	return s.blocks, nil
}
func (s *compactStore) AppendCompact(_ context.Context, _ string, req core.CompactWrite) (core.ContextBlock, error) {
	call := int64(100 + 2*len(s.writes))
	s.writes = append(s.writes, req)
	return core.ContextBlock{
		ID: fmt.Sprintf("blk-%d", call), Tier: req.Tier, From: req.From, To: req.To,
		Keep: req.Keep, Call: call, Carrier: call + 1,
	}, nil
}

// compactFace is the capsule's compact face: a fixed answer, and the request it was handed.
type compactFace struct {
	answer CompactAnswer
	err    error
	got    CompactRequest
	calls  int
}

func (f *compactFace) PlanCompact(context.Context, CompactRequest) CompactPlan {
	return CompactPlan{Fire: true, Keep: f.answer.Keep}
}

func (f *compactFace) CompactNow(_ context.Context, req CompactRequest) (CompactAnswer, error) {
	f.got = req
	f.calls++
	return f.answer, f.err
}

// userRows seeds n user rows with store ids 1..n.
func userRows(n int) []core.StoredMessage {
	out := make([]core.StoredMessage, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, core.StoredMessage{ID: int64(i), Message: core.NewUser(fmt.Sprintf("row %d", i))})
	}
	return out
}

// carrierRows appends a compact's carrier pair to rows, keeping ids in sequence.
func carrierRows(rows []core.StoredMessage, summary string) ([]core.StoredMessage, int64, int64) {
	var maxID int64
	for _, sm := range rows {
		if sm.ID > maxID {
			maxID = sm.ID
		}
	}
	callID := maxID + 1
	call, result := CarrierMessages(summary, 2, maxID)
	call.ParentID = core.MsgID(maxID)
	result.ParentID = core.MsgID(callID)
	return append(rows,
		core.StoredMessage{ID: callID, Message: call},
		core.StoredMessage{ID: callID + 1, Message: result},
	), callID, callID + 1
}

// TestCompactSessionAppendsTheCarrier pins the compact: the summary rides a compress call +
// result at the tail, the block names the span, and the outcome carries the real numbers.
func TestCompactSessionAppendsTheCarrier(t *testing.T) {
	st := &compactStore{path: userRows(5)}
	c := &compactFace{answer: CompactAnswer{Summary: "the middle", Keep: 1}}
	out, err := CompactSession(context.Background(), st, c, "s-1")
	if err != nil {
		t.Fatalf("CompactSession: %v", err)
	}
	if !out.Compacted || out.Rows != 3 || out.Kept != 1 {
		t.Fatalf("outcome = %+v, want compacted rows=3 kept=1", out)
	}
	if len(st.writes) != 1 {
		t.Fatalf("writes = %d, want one block", len(st.writes))
	}
	w := st.writes[0]
	if w.From != 2 || w.To != 5 || w.Keep != 5 || w.Tier != 1 || w.Summary != "the middle" {
		t.Fatalf("block = %+v, want from=2 to=5 keep=5 tier=1", w)
	}
	if len(c.got.Wire) != 5 || c.got.LiveStart != 1 {
		t.Fatalf("compactor handed %d messages (live start %d), want the 5-row wire starting at 1", len(c.got.Wire), c.got.LiveStart)
	}
	want := "compacted 3 messages into a summary carried by compress-100 - the last 1 stays verbatim"
	if got := CompactLine(out); got != want {
		t.Fatalf("line = %q, want %q", got, want)
	}
}

// TestDeriveWireKeepsThePrefix is the carrier law: a compact never rewrites a byte the
// provider already saw. The wire before the compact and after it share the pre-carrier
// prefix verbatim, and the carrier rides the tail.
func TestDeriveWireKeepsThePrefix(t *testing.T) {
	rows := userRows(5)
	before := DeriveWire(rows, nil, core.Session{})
	rows, callID, carrierID := carrierRows(rows, "the middle")
	block := core.ContextBlock{ID: "blk-1", Tier: 1, From: 2, To: 5, Keep: 5, Call: callID, Carrier: carrierID}
	after := DeriveWire(rows, []core.ContextBlock{block}, core.Session{})

	if len(after) != 4 {
		t.Fatalf("wire after compact = %d messages, want 4 (first user + carrier pair + kept tail)", len(after))
	}
	if !sameMessage(before[0], after[0]) {
		t.Fatalf("the pre-carrier prefix changed: %#v -> %#v", before[0], after[0])
	}
	call, ok := after[1].(core.Assistant)
	if !ok || len(call.ToolCalls) != 1 || call.ToolCalls[0].Name != core.CompressToolName {
		t.Fatalf("second message = %#v, want the compress tool call", after[1])
	}
	result, ok := after[2].(core.ToolResult)
	if !ok || result.Content != "the middle" {
		t.Fatalf("third message = %#v, want the summary carrier", after[2])
	}
	if MessageText(after[3]) != "row 5" {
		t.Fatalf("tail = %q, want the retained row", MessageText(after[3]))
	}
	// The first user message is never pruned: it is the wire's legal head.
	if MessageText(after[0]) != "row 1" {
		t.Fatalf("head = %q, want the first user message", MessageText(after[0]))
	}
}

// TestDeriveWireForkBeforeTheCompact pins the truncated-path law: a fork or retry hands a
// prefix of the active path, and a block whose span reaches past it (or whose carrier rows
// are absent) is never applied - the branch replays its own rows instead of collapsing to
// the bare head.
func TestDeriveWireForkBeforeTheCompact(t *testing.T) {
	rows := userRows(6)
	rows, call, carrier := carrierRows(rows, "the summary")
	rows = append(rows, core.StoredMessage{ID: 9, Message: core.NewAssistant("later answer")})
	blocks := []core.ContextBlock{{ID: "blk-1", Tier: 1, From: 1, To: 6, Call: call, Carrier: carrier}}

	full := DeriveWire(rows, blocks, core.Session{})
	if len(full) != 4 {
		t.Fatalf("full wire = %d messages, want 4 (head + carrier pair + tail)", len(full))
	}
	forked := DeriveWire(rows[:3], blocks, core.Session{})
	if len(forked) != 3 {
		t.Fatalf("fork wire = %d messages, want the branch's 3 rows", len(forked))
	}
	for i, want := range []string{"row 1", "row 2", "row 3"} {
		if got := MessageText(forked[i]); got != want {
			t.Fatalf("fork wire[%d] = %q, want %q", i, got, want)
		}
	}
	plan := planFromRows(rows[:3], blocks, core.Session{})
	if len(plan.Blocks) != 0 {
		t.Fatalf("truncated plan carries %d blocks, want none applied", len(plan.Blocks))
	}
}

// TestDeriveWireForkAfterTheCompactKeepsTheCarrier pins the other half: a fork whose prefix
// reaches the carrier rows still derives the compacted shape, head and carrier included.
func TestDeriveWireForkAfterTheCompactKeepsTheCarrier(t *testing.T) {
	rows := userRows(6)
	rows, call, carrier := carrierRows(rows, "the summary")
	rows = append(rows,
		core.StoredMessage{ID: 9, Message: core.NewUser("row 9")},
		core.StoredMessage{ID: 10, Message: core.NewUser("row 10")},
	)
	blocks := []core.ContextBlock{{ID: "blk-1", Tier: 1, From: 1, To: 6, Call: call, Carrier: carrier}}

	forked := DeriveWire(rows[:9], blocks, core.Session{})
	if len(forked) != 4 {
		t.Fatalf("fork wire = %d messages, want 4 (head + carrier pair + fork row)", len(forked))
	}
	if !IsCarrierResult(forked[2]) || MessageText(forked[2]) != "the summary" {
		t.Fatalf("fork wire[2] = %q, want the summary carrier", MessageText(forked[2]))
	}
	if got := MessageText(forked[3]); got != "row 9" {
		t.Fatalf("fork wire[3] = %q, want the fork row", got)
	}
	// A prefix that ends between a carrier's call and its result applies no block at all:
	// a half-carrier is never a wire.
	half := DeriveWire(rows[:call], blocks, core.Session{})
	if len(half) != int(call) {
		t.Fatalf("half-carrier wire = %d messages, want the %d whole rows", len(half), call)
	}
	for _, m := range half {
		if IsCarrierResult(m) {
			t.Fatalf("half-carrier wire carries a summary result: %#v", m)
		}
	}
}

// TestDeriveWireTruncatedKeepsOnlyCoveredBlocks pins the stacked case: a prefix that stops
// before the second compact keeps the first block applied and the second one out.
func TestDeriveWireTruncatedKeepsOnlyCoveredBlocks(t *testing.T) {
	rows := userRows(5)
	rows, call1, carrier1 := carrierRows(rows, "first summary")
	for i := 8; i <= 10; i++ {
		rows = append(rows, core.StoredMessage{ID: int64(i), Message: core.NewUser(fmt.Sprintf("row %d", i))})
	}
	rows, call2, carrier2 := carrierRows(rows, "second summary")
	blocks := []core.ContextBlock{
		{ID: "blk-1", Tier: 1, From: 2, To: 5, Call: call1, Carrier: carrier1},
		{ID: "blk-2", Tier: 1, From: 8, To: 10, Call: call2, Carrier: carrier2},
	}

	// Prefix through row 9: the first compact applies, the second one's carriers are absent.
	wire := DeriveWire(rows[:9], blocks, core.Session{})
	if len(wire) != 5 {
		t.Fatalf("wire = %d messages, want 5 (head + first carrier + rows 8-9)", len(wire))
	}
	if MessageText(wire[2]) != "first summary" {
		t.Fatalf("wire[2] = %q, want the first carrier", MessageText(wire[2]))
	}
	if got := MessageText(wire[len(wire)-1]); got != "row 9" {
		t.Fatalf("wire tail = %q, want row 9", got)
	}
	for _, m := range wire {
		if IsCarrierResult(m) && MessageText(m) == "second summary" {
			t.Fatal("a block the prefix cannot carry was applied")
		}
	}
}

// sameMessage compares two messages by their encoded shape.
func sameMessage(a, b core.Message) bool {
	ra, pa, errA := core.EncodeMessage(a)
	rb, pb, errB := core.EncodeMessage(b)
	return errA == nil && errB == nil && ra == rb && string(pa) == string(pb)
}

// TestDeriveWireStacksCarriers pins the tiered shape: every compact's carrier stays in the
// head in order, so a second compact does not rewrite the first one's bytes.
func TestDeriveWireStacksCarriers(t *testing.T) {
	rows := userRows(5)
	rows, call1, carrier1 := carrierRows(rows, "first summary")
	blocks := []core.ContextBlock{{ID: "blk-1", Tier: 1, From: 2, To: 5, Keep: 5, Call: call1, Carrier: carrier1}}
	for i := 8; i <= 10; i++ {
		rows = append(rows, core.StoredMessage{ID: int64(i), Message: core.NewUser(fmt.Sprintf("row %d", i))})
	}
	rows, call2, carrier2 := carrierRows(rows, "second summary")
	blocks = append(blocks, core.ContextBlock{ID: "blk-2", Tier: 1, From: 8, To: 10, Keep: 10, Call: call2, Carrier: carrier2})

	wire := DeriveWire(rows, blocks, core.Session{})
	if len(wire) != 6 {
		t.Fatalf("wire = %d messages, want 6 (first user + two carriers + kept tail)", len(wire))
	}
	if MessageText(wire[0]) != "row 1" || MessageText(wire[2]) != "first summary" || MessageText(wire[4]) != "second summary" {
		t.Fatalf("wire = %#v, want the head and both carriers in order", wire)
	}
	if got := MessageText(wire[5]); got != "row 10" {
		t.Fatalf("wire[5] = %q, want the retained tail row", got)
	}
}

// TestCompactSessionRefusals pins every honest no-op: each names its own reason and writes
// no block - a compact that drops the middle and keeps nothing in its place is worse than
// no compact at all.
func TestCompactSessionRefusals(t *testing.T) {
	cases := []struct {
		name string
		id   string
		st   *compactStore
		c    ContextCompactor
		want string
	}{
		{"no session", "", &compactStore{path: userRows(3)}, &compactFace{}, "nothing compacted: no session is bound to this chat yet"},
		{"ephemeral", "eph-live", &compactStore{path: userRows(3)}, &compactFace{}, "nothing compacted: ephemeral sessions keep no history"},
		{"empty conversation", "s-1", &compactStore{}, &compactFace{}, "nothing compacted: this conversation has no messages yet"},
		{"no compactor", "s-1", &compactStore{path: userRows(3)}, nil, "nothing compacted: the compaction module is not mounted"},
		{"nothing to summarize", "s-1", &compactStore{path: userRows(3)}, &compactFace{}, "nothing compacted: the compactor had nothing to summarize (a short conversation, or compaction is off)"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			out, err := CompactSession(context.Background(), tt.st, tt.c, tt.id)
			if err != nil {
				t.Fatalf("CompactSession: %v", err)
			}
			if out.Compacted {
				t.Fatalf("outcome = %+v, want a refusal", out)
			}
			if got := CompactLine(out); got != tt.want {
				t.Fatalf("line = %q, want %q", got, tt.want)
			}
			if len(tt.st.writes) != 0 {
				t.Fatalf("a refused compact wrote %d blocks", len(tt.st.writes))
			}
		})
	}
}

// TestCompactSessionNeedsABlockSurface pins the capability gap as a typed sentinel: the
// HTTP dispatch answers 501 on it, the CLI prints its error - neither invents a result.
func TestCompactSessionNeedsABlockSurface(t *testing.T) {
	_, err := CompactSession(context.Background(), fakeStore{}, &compactFace{}, "s-1")
	if !errors.Is(err, ErrNoSessionContext) {
		t.Fatalf("err = %v, want ErrNoSessionContext", err)
	}
}

// TestCompactSessionSurfacesTheCompactorError pins the failure lane: a dead upstream is the
// caller's error to name, and it writes no block.
func TestCompactSessionSurfacesTheCompactorError(t *testing.T) {
	st := &compactStore{path: userRows(3)}
	_, err := CompactSession(context.Background(), st, &compactFace{err: errors.New("compactor: upstream is down")}, "s-1")
	if err == nil || !strings.Contains(err.Error(), "upstream is down") {
		t.Fatalf("err = %v, want the upstream's own failure", err)
	}
	if len(st.writes) != 0 {
		t.Fatalf("a failed compact wrote %d blocks", len(st.writes))
	}
}

// TestSessionContextReadsTheBlocks proves the derivation both hosts share: after a compact
// the session reads back with its tiers, lineage and carrier, never as the raw path.
func TestSessionContextReadsTheBlocks(t *testing.T) {
	rows, call, carrier := carrierRows(userRows(4), "the middle")
	st := &compactStore{path: rows, blocks: []core.ContextBlock{
		{ID: "blk-1", Tier: 1, From: 2, To: 4, Keep: 4, Call: call, Carrier: carrier},
	}}
	msgs, err := SessionContext(context.Background(), st, "s-1")
	if err != nil {
		t.Fatalf("SessionContext: %v", err)
	}
	if len(msgs) != 4 {
		t.Fatalf("context = %d messages, want 4 (head + carrier + kept tail)", len(msgs))
	}
	if _, ok := msgs[1].(core.Assistant); !ok {
		t.Fatalf("msgs[1] = %T, want the carrier's call", msgs[1])
	}
	if MessageText(msgs[2]) != "the middle" {
		t.Fatalf("msgs[2] = %q, want the summary", MessageText(msgs[2]))
	}
}

// TestSessionContextKeepsTheLegacyShape pins the pre-block session: no blocks, but a
// boundary and a summary, reads exactly as it always did - the migration is not a rewrite.
func TestSessionContextKeepsTheLegacyShape(t *testing.T) {
	st := &compactStore{
		row:  core.Session{ContextStart: "3", ContextSummary: "the middle"},
		path: userRows(4),
	}
	msgs, err := SessionContext(context.Background(), st, "s-1")
	if err != nil {
		t.Fatalf("SessionContext: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("context = %d messages, want the summary plus the kept tail (3)", len(msgs))
	}
	sys, ok := msgs[0].(core.System)
	if !ok || sys.Content != core.ContextCompactPrefix+"the middle"+core.ContextCompactSuffix {
		t.Fatalf("head = %#v, want the legacy summary", msgs[0])
	}
}

// TestCompactSpanNeedsAMiddle pins the span arithmetic: a keep that swallows everything the
// compact could take answers false, so the caller refuses instead of writing an empty block.
func TestCompactSpanNeedsAMiddle(t *testing.T) {
	plan := ContextPlan{Wire: StoredMessages(userRows(3)), IDs: []int64{1, 2, 3}, LiveStart: 1}
	if _, _, _, _, ok := CompactSpan(plan, 2); ok {
		t.Fatal("a keep of 2 over a 3-row wire left no middle, want ok=false")
	}
	from, to, keepID, rows, ok := CompactSpan(plan, 1)
	if !ok || from != 2 || to != 3 || keepID != 3 || rows != 1 {
		t.Fatalf("span = (%d,%d,%d,%d,%v), want (2,3,3,1,true)", from, to, keepID, rows, ok)
	}
}

// TestCarrierMessagesRoundTrip pins the pair the compact appends: one compress call, its
// result, and the detectors both the derivation and the loop read.
func TestCarrierMessagesRoundTrip(t *testing.T) {
	call, result := CarrierMessages("a summary", 4, 9)
	if !IsCarrierCall(call) {
		t.Fatal("the carrier's call does not read as a compress call")
	}
	if !IsCarrierResult(result) {
		t.Fatal("the carrier's result does not read as a compress result")
	}
	if got := call.ToolCalls[0].Arguments["messages"]; got != int64(6) {
		t.Fatalf("messages argument = %v, want 6", got)
	}
	wire := CarrierWire([]core.Message{core.NewUser("a"), core.NewUser("b"), core.NewUser("c")}, 1, 2, "s", 4, 9)
	if len(wire) != 4 {
		t.Fatalf("carrier wire = %d messages, want 4", len(wire))
	}
	if MessageText(wire[0]) != "a" || MessageText(wire[3]) != "c" {
		t.Fatalf("carrier wire = %#v, want head + carrier + tail", wire)
	}
}
