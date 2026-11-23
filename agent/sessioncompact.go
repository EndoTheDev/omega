package agent

// sessioncompact.go: the context model. A session's context is tiered blocks; a compact
// appends a compress tool call and its result at the TAIL of the session and records the
// block they form. The provider wire is DERIVED from the blocks, so a provider replaying
// the history sees the bytes it already saw before the compact landed - the summary never
// rides a mid-stream system message (the two-carrier law). Both hosts (the CLI loop and
// the HTTP dispatch) derive and compact through this file, so the one thing a human reads
// cannot drift between surfaces.

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/EndoTheDev/omega/core"
)

// ErrNoSessionContext: the store cannot hold a context block.
var ErrNoSessionContext = errors.New("session store has no session-context surface")

// CompactRequest is what a compact is asked to look at: the live wire, where the head ends
// (the first user message and every earlier carrier stay live), and how many trailing
// messages to retain verbatim (-1 = the compactor's configured default).
type CompactRequest struct {
	Wire      []core.Message
	LiveStart int
	Keep      int
}

// CompactPlan is the growth gate's answer for the automatic arm: whether to compact now,
// the tail to retain, and - when it refuses - the reason in one clause.
type CompactPlan struct {
	Fire bool
	Keep int
	Why  string
}

// CompactAnswer is what the summarizer produced: the summary text (empty = there was
// nothing to summarize) and the tail it left verbatim.
type CompactAnswer struct {
	Summary string
	Keep    int
}

// ContextCompactor is the compaction face a host resolves from the mounted capsules: the
// growth-gated plan for the automatic arm, the summary for whichever arm asked.
type ContextCompactor interface {
	PlanCompact(ctx context.Context, req CompactRequest) CompactPlan
	CompactNow(ctx context.Context, req CompactRequest) (CompactAnswer, error)
}

// SessionBlockStore is the context-block surface: the tier structure a session's wire is
// derived from, and the one append that grows it.
type SessionBlockStore interface {
	ContextBlocks(ctx context.Context, sessionID string) ([]core.ContextBlock, error)
	AppendCompact(ctx context.Context, sessionID string, req core.CompactWrite) (core.ContextBlock, error)
}

// ContextPlan is a session's live context with the stored row behind every message (0 for
// a synthesized row) and the block structure it was derived from.
type ContextPlan struct {
	Wire      []core.Message
	IDs       []int64
	Blocks    []core.ContextBlock
	LiveStart int
}

// CompactOutcome is what a compact actually did.
type CompactOutcome struct {
	Compacted bool
	Block     string // the block the compact recorded
	Carrier   string // the compress tool call that carries its summary
	Rows      int    // messages the compact removed from the live context
	Kept      int    // messages the next context keeps verbatim
	Why       string // the refusal, when Compacted is false
}

// PlanContext derives a session's provider-facing context together with the store rows
// behind it. A store that cannot hold blocks serves the legacy boundary shape, and a
// session with neither serves its whole active path.
func PlanContext(ctx context.Context, store StoreProvider, sessionID string) (ContextPlan, error) {
	sess, err := store.GetSession(ctx, sessionID)
	if err != nil {
		return ContextPlan{}, err
	}
	view, err := store.GetActiveView(ctx, sessionID)
	if err != nil {
		return ContextPlan{}, err
	}
	return planFromRows(view.Path, contextBlocksOf(ctx, store, sessionID), sess), nil
}

// SessionContext assembles a session's provider-facing history. A host that sends history
// itself (the CLI loop) must send THIS.
func SessionContext(ctx context.Context, store StoreProvider, sessionID string) ([]core.Message, error) {
	plan, err := PlanContext(ctx, store, sessionID)
	if err != nil {
		return nil, err
	}
	return plan.Wire, nil
}

// DeriveWire is the provider-facing context of an active path. With blocks the wire is the
// first user message, every compact carrier in order, then the live rows after the last
// compact - nothing before a carrier ever changes bytes. Without blocks a session keeps
// the legacy shape (its boundary plus a summary head), so old conversations read exactly
// as they always did until a carrier replaces them.
func DeriveWire(rows []core.StoredMessage, blocks []core.ContextBlock, sess core.Session) []core.Message {
	return planFromRows(rows, blocks, sess).Wire
}

// contextBlocksOf is the block read, tolerant of a store without the surface.
func contextBlocksOf(ctx context.Context, store StoreProvider, sessionID string) []core.ContextBlock {
	blocks, ok := store.(SessionBlockStore)
	if !ok {
		return nil
	}
	out, err := blocks.ContextBlocks(ctx, sessionID)
	if err != nil {
		return nil
	}
	return out
}

// planFromRows is DeriveWire with the ids and the head boundary a compact needs.
func planFromRows(rows []core.StoredMessage, blocks []core.ContextBlock, sess core.Session) ContextPlan {
	if len(blocks) == 0 && sess.ContextSummary == "" && sess.ContextStart == "" {
		return ContextPlan{Wire: StoredMessages(rows), IDs: rowIDs(rows), LiveStart: 0}.withLiveStart()
	}
	if len(blocks) == 0 {
		plan := ContextPlan{Blocks: blocks}
		plan.Wire = append(plan.Wire, CompactSummaryPrefix(sess.ContextSummary)...)
		for range plan.Wire {
			plan.IDs = append(plan.IDs, 0)
		}
		for _, sm := range CompactPath(rows, sess.ContextStart) {
			plan.Wire = append(plan.Wire, sm.Message)
			plan.IDs = append(plan.IDs, sm.ID)
		}
		return plan.withLiveStart()
	}
	wire, ids := deriveFromBlocks(rows, blocks)
	return ContextPlan{Wire: wire, IDs: ids, Blocks: blocks}.withLiveStart()
}

// deriveFromBlocks is the block derivation: first user message, then every carrier in
// compact order, then the rows the last compact left live.
func deriveFromBlocks(rows []core.StoredMessage, blocks []core.ContextBlock) ([]core.Message, []int64) {
	carrier := make(map[int64]bool, 2*len(blocks))
	for _, b := range blocks {
		if b.Call != 0 {
			carrier[b.Call] = true
		}
		if b.Carrier != 0 {
			carrier[b.Carrier] = true
		}
	}
	byID := make(map[int64]core.StoredMessage, len(rows))
	for _, sm := range rows {
		byID[sm.ID] = sm
	}
	var wire []core.Message
	var ids []int64
	add := func(id int64, msg core.Message) {
		wire = append(wire, msg)
		ids = append(ids, id)
	}
	var firstUser int64
	for _, sm := range rows {
		if _, ok := sm.Message.(core.User); ok {
			firstUser = sm.ID
			add(sm.ID, sm.Message)
			break
		}
	}
	for _, b := range blocks {
		for _, id := range []int64{b.Call, b.Carrier} {
			if id == 0 {
				continue
			}
			if sm, ok := byID[id]; ok {
				add(id, sm.Message)
			}
		}
	}
	last := blocks[len(blocks)-1]
	for _, sm := range rows {
		if carrier[sm.ID] || (firstUser != 0 && sm.ID == firstUser) {
			continue
		}
		if last.Keep > 0 {
			if sm.ID < last.Keep {
				continue
			}
		} else if sm.ID <= last.To {
			continue
		}
		add(sm.ID, sm.Message)
	}
	return wire, ids
}

// withLiveStart stamps where the head ends: the first user message and every compress
// carrier that follows it are never raw material.
func (p ContextPlan) withLiveStart() ContextPlan {
	p.LiveStart = liveStart(p.Wire)
	return p
}

// LiveStart is the index in a provider wire where a compact's raw material may begin.
func LiveStart(wire []core.Message) int { return liveStart(wire) }

// liveStart is the index of the first message a compact may take: right after the first
// user message and every carrier pair that follows it.
func liveStart(wire []core.Message) int {
	head := -1
	for i, m := range wire {
		if _, ok := m.(core.User); ok {
			head = i
			break
		}
	}
	i := 0
	if head >= 0 {
		i = head + 1
	}
	for i+1 < len(wire) && IsCarrierCall(wire[i]) && IsCarrierResult(wire[i+1]) {
		i += 2
	}
	return i
}

// IsCarrierCall reports a compact's compress tool call.
func IsCarrierCall(msg core.Message) bool {
	a, ok := msg.(core.Assistant)
	if !ok || a.Content != "" {
		return false
	}
	return len(a.ToolCalls) == 1 && a.ToolCalls[0].Name == core.CompressToolName
}

// IsCarrierResult reports a compact's summary carrier: the tool result answering the
// compress call.
func IsCarrierResult(msg core.Message) bool {
	tr, ok := msg.(core.ToolResult)
	if !ok || tr.IsError {
		return false
	}
	return strings.HasPrefix(tr.ToolCallID, core.CompressToolName+"-")
}

// CarrierMessages builds the pair a compact appends: the compress call and its result.
func CarrierMessages(summary string, from, to int64) (core.Assistant, core.ToolResult) {
	callID := fmt.Sprintf("%s-%s-%s", core.CompressToolName, core.MsgID(from), core.MsgID(to))
	call := core.NewAssistant("")
	call.ToolCalls = []core.ToolCall{{
		ID:   callID,
		Name: core.CompressToolName,
		Arguments: map[string]any{
			"from":     core.MsgID(from),
			"to":       core.MsgID(to),
			"messages": to - from + 1,
		},
	}}
	return call, core.NewToolResult(summary, callID, false)
}

// CarrierWire is the wire after a compact: the head, the carrier pair, then the retained
// tail. It is what an in-run compact hands back to the loop; the store's derivation lands
// on the same shape.
func CarrierWire(wire []core.Message, start, end int, summary string, from, to int64) []core.Message {
	call, result := CarrierMessages(summary, from, to)
	out := make([]core.Message, 0, len(wire)-(end-start)+2)
	out = append(out, wire[:start]...)
	out = append(out, call, result)
	return append(out, wire[end:]...)
}

// CompactSpan maps "retain the last keep live messages" onto stored row ids: From is the
// first row the summary replaces, To the last row that existed when the compact landed,
// Keep the first row the context keeps verbatim (0 = it keeps no tail).
func CompactSpan(plan ContextPlan, keep int) (from, to, keepID int64, rows int, ok bool) {
	n := len(plan.Wire)
	if keep < 0 {
		keep = 0
	}
	if keep > n {
		keep = n
	}
	end := n - keep
	start := plan.LiveStart
	if end <= start {
		return 0, 0, 0, 0, false
	}
	for _, id := range plan.IDs[start:end] {
		if id > 0 {
			from = id
			break
		}
	}
	for i := len(plan.IDs) - 1; i >= 0; i-- {
		if plan.IDs[i] > 0 {
			to = plan.IDs[i]
			break
		}
	}
	if from == 0 || to == 0 || to < from {
		return 0, 0, 0, 0, false
	}
	if keep > 0 {
		for _, id := range plan.IDs[end:] {
			if id > 0 {
				keepID = id
				break
			}
		}
		if keepID == 0 || keepID <= from {
			return 0, 0, 0, 0, false
		}
	}
	return from, to, keepID, end - start, true
}

// CompactSession compacts a session because a human asked (/compact). The session
// PERSISTS - a compact moves the context, it never starts a new conversation. Nothing is
// written when there is no summary to write: a boundary that drops the middle and keeps
// nothing in its place is worse than no compact at all.
func CompactSession(ctx context.Context, store StoreProvider, compactor ContextCompactor, sessionID string) (CompactOutcome, error) {
	return CompactSessionKeep(ctx, store, compactor, sessionID, -1)
}

// CompactSessionKeep is CompactSession with the retained tail forced (keep >= 0): /new on
// a machinery-owned conversation keeps one row as a foothold, never zero.
func CompactSessionKeep(ctx context.Context, store StoreProvider, compactor ContextCompactor, sessionID string, keep int) (CompactOutcome, error) {
	switch {
	case sessionID == "":
		return CompactOutcome{Why: "no session is bound to this chat yet"}, nil
	case IsEphemeralID(sessionID):
		return CompactOutcome{Why: "ephemeral sessions keep no history"}, nil
	}
	if store == nil {
		return CompactOutcome{}, ErrNoSessionContext
	}
	blocks, ok := store.(SessionBlockStore)
	if !ok {
		return CompactOutcome{}, ErrNoSessionContext
	}
	plan, err := PlanContext(ctx, store, sessionID)
	if err != nil {
		return CompactOutcome{}, err
	}
	if len(plan.Wire) == 0 {
		return CompactOutcome{Why: "this conversation has no messages yet"}, nil
	}
	if compactor == nil {
		return CompactOutcome{Why: "the compaction module is not mounted"}, nil
	}
	answer, err := compactor.CompactNow(ctx, CompactRequest{Wire: plan.Wire, LiveStart: plan.LiveStart, Keep: keep})
	if err != nil {
		return CompactOutcome{}, err
	}
	if answer.Summary == "" {
		return CompactOutcome{Why: "the compactor had nothing to summarize (a short conversation, or compaction is off)"}, nil
	}
	if keep < 0 {
		keep = answer.Keep
	}
	from, to, keepID, rows, ok := CompactSpan(plan, keep)
	if !ok {
		return CompactOutcome{Why: "nothing between the head and the retained tail to summarize"}, nil
	}
	block, err := blocks.AppendCompact(ctx, sessionID, core.CompactWrite{
		Summary: answer.Summary,
		From:    from,
		To:      to,
		Keep:    keepID,
		Tier:    1,
	})
	if err != nil {
		return CompactOutcome{}, err
	}
	return CompactOutcome{
		Compacted: true,
		Block:     block.ID,
		Carrier:   fmt.Sprintf("%s-%d", core.CompressToolName, block.Call),
		Rows:      rows,
		Kept:      len(plan.Wire) - plan.LiveStart - rows,
	}, nil
}

// CompactLine is the one wording of a compact's answer: the compact's real numbers, or the
// real refusal. Hosts print this verbatim - never a hand-rolled variant.
func CompactLine(out CompactOutcome) string {
	if !out.Compacted {
		return "nothing compacted: " + out.Why
	}
	tail := fmt.Sprintf("the last %d stay verbatim", out.Kept)
	switch out.Kept {
	case 0:
		tail = "nothing is kept verbatim"
	case 1:
		tail = "the last 1 stays verbatim"
	}
	return fmt.Sprintf("compacted %d messages into a summary carried by %s - %s", out.Rows, out.Carrier, tail)
}

// CompactPath cuts the active path at a legacy compacted conversation's context boundary.
func CompactPath(path []core.StoredMessage, start string) []core.StoredMessage {
	if start == "" {
		return path
	}
	for i, sm := range path {
		if core.MsgID(sm.ID) == start {
			return path[i:]
		}
	}
	return path
}

// CompactSummaryPrefix heads a legacy compacted conversation's context with its summary.
func CompactSummaryPrefix(summary string) []core.Message {
	if summary == "" {
		return nil
	}
	return []core.Message{core.NewSystem(core.ContextCompactPrefix + summary + core.ContextCompactSuffix)}
}

// StoredMessages strips a tree view down to plain provider context.
func StoredMessages(path []core.StoredMessage) []core.Message {
	out := make([]core.Message, 0, len(path))
	for _, sm := range path {
		out = append(out, sm.Message)
	}
	return out
}

// rowIDs is the store row behind each message, in the same order.
func rowIDs(path []core.StoredMessage) []int64 {
	out := make([]int64, 0, len(path))
	for _, sm := range path {
		out = append(out, sm.ID)
	}
	return out
}
