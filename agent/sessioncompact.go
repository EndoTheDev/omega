package agent

// sessioncompact.go: the ONE manual compact, and the ONE wording of its answer. A capsule
// owns the summarizer and no store - it only declares the intent (`core.CmdRunCompact`) -
// so the host holding the session runs the compact. Both hosts (the CLI loop and the HTTP
// dispatch) call this file, so the line a human reads cannot drift between surfaces and
// no surface can answer with silence.

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/EndoTheDev/omega/core"
)

// ManualCompactor is the on-demand compact face a host resolves from the mounted capsules.
type ManualCompactor interface {
	CompactNow(ctx context.Context, messages []core.Message) ([]core.Message, error)
}

// ErrNoSessionContext: the store cannot hold a context boundary.
var ErrNoSessionContext = errors.New("session store has no session-context surface")

// CompactOutcome is what a manual compact actually did.
type CompactOutcome struct {
	Compacted bool
	Start     string // the message the next context begins at
	Rows      int    // messages the compact removed from the live context
	Kept      int    // messages the next context keeps verbatim
	Why       string // the refusal, when Compacted is false
}

// CompactSession compacts a session because a human asked (/compact). The session PERSISTS
// - a compact moves the context boundary, it never starts a new conversation. Nothing is
// written when the compactor has no summary to offer: a boundary that drops the middle and
// keeps nothing in its place is worse than no compact at all. Every refusal carries its own
// reason, so a caller always has an honest line to print.
func CompactSession(ctx context.Context, store StoreProvider, compactor ManualCompactor, sessionID string) (CompactOutcome, error) {
	switch {
	case sessionID == "":
		return CompactOutcome{Why: "no session is bound to this chat yet"}, nil
	case IsEphemeralID(sessionID):
		return CompactOutcome{Why: "ephemeral sessions keep no history"}, nil
	}
	if store == nil {
		return CompactOutcome{}, ErrNoSessionContext
	}
	scx, ok := store.(SessionContextStore)
	if !ok {
		return CompactOutcome{}, ErrNoSessionContext
	}
	view, err := store.GetActiveView(ctx, sessionID)
	if err != nil {
		return CompactOutcome{}, err
	}
	path := StoredMessages(view.Path)
	if len(path) == 0 {
		return CompactOutcome{Why: "this conversation has no messages yet"}, nil
	}
	if compactor == nil {
		return CompactOutcome{Why: "the compaction module is not mounted"}, nil
	}
	compacted, err := compactor.CompactNow(ctx, path)
	if err != nil {
		return CompactOutcome{}, err
	}
	if len(compacted) == 0 {
		return CompactOutcome{Why: "the compactor had nothing to summarize (a short conversation, or compaction is off)"}, nil
	}
	// CompactSummary reads the head the compactor wrote; the rows after it are the tail the
	// compact kept verbatim, and they are what the boundary has to sit on.
	summary := CompactSummary(compacted)
	kept := len(compacted) - 1
	start := len(path) - kept
	if summary == "" {
		return CompactOutcome{}, errors.New("the compactor answered a compact without a summary")
	}
	if start <= 0 || start >= len(path) {
		return CompactOutcome{}, fmt.Errorf("compaction kept %d of %d rows - no boundary to write", kept, len(path))
	}
	if err := scx.SetSessionContextStart(ctx, sessionID, core.MsgID(view.Path[start].ID), summary); err != nil {
		return CompactOutcome{}, err
	}
	return CompactOutcome{Compacted: true, Start: core.MsgID(view.Path[start].ID), Rows: start, Kept: kept}, nil
}

// CompactLine is the one wording of a manual compact's answer: the compact's real numbers,
// or the real refusal. Hosts print this verbatim - never a hand-rolled variant.
func CompactLine(out CompactOutcome) string {
	if !out.Compacted {
		return "nothing compacted: " + out.Why
	}
	return fmt.Sprintf("compacted %d messages into a summary - the context now starts at message %s, and the last %d stay verbatim",
		out.Rows, out.Start, out.Kept)
}

// CompactContext assembles a session's provider-facing history: the active path cut at the
// compact's context boundary and headed by its summary. A host that sends history itself
// (the CLI loop) must send THIS - a compact it just reported only reaches the next run when
// the boundary and its summary are applied here.
func CompactContext(ctx context.Context, store StoreProvider, sessionID string) ([]core.Message, error) {
	row, err := store.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	view, err := store.GetActiveView(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	msgs := make([]core.Message, 0, len(view.Path)+1)
	msgs = append(msgs, CompactSummaryPrefix(row.ContextSummary)...)
	return append(msgs, StoredMessages(CompactPath(view.Path, row.ContextStart))...), nil
}

// CompactPath cuts the active path at a compacted conversation's context boundary.
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

// CompactSummaryPrefix heads a compacted conversation's context with its summary. No summary
// (a compact too small to earn one, or a compactor that was not mounted) leaves the context
// exactly as the boundary left it.
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

// CompactSummary pulls the summary out of a compacted message list.
func CompactSummary(compacted []core.Message) string {
	for _, m := range compacted {
		sys, ok := m.(core.System)
		if !ok {
			continue
		}
		if s, found := strings.CutPrefix(sys.Content, core.ContextCompactPrefix); found {
			if s, found := strings.CutSuffix(s, core.ContextCompactSuffix); found {
				return s
			}
		}
	}
	return ""
}
