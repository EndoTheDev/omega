package agent

// sessionfold.go: the ONE manual fold, and the ONE wording of its answer. A capsule owns
// the summarizer and no store - it only declares the intent (`core.CmdRunCompact`) - so
// the host holding the session runs the fold. Both hosts (the CLI loop and the HTTP
// dispatch) call this file, so the line a human reads cannot drift between surfaces and
// no surface can answer with silence.

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/EndoTheDev/omega/core"
)

// ManualCompactor is the on-demand fold face a host resolves from the mounted capsules.
type ManualCompactor interface {
	FoldNow(ctx context.Context, messages []core.Message) ([]core.Message, error)
}

// ErrNoSessionContext: the store cannot hold a context boundary.
var ErrNoSessionContext = errors.New("session store has no session-context surface")

// FoldOutcome is what a manual fold actually did.
type FoldOutcome struct {
	Folded bool
	Start  string // the message the next context begins at
	Rows   int    // messages the fold removed from the live context
	Kept   int    // messages the next context keeps verbatim
	Why    string // the refusal, when Folded is false
}

// FoldSession folds a session because a human asked (/compact). The session PERSISTS - a
// fold moves the context boundary, it never starts a new conversation. Nothing is written
// when the compactor has no summary to offer: a boundary that drops the middle and keeps
// nothing in its place is worse than no fold at all. Every refusal carries its own reason,
// so a caller always has an honest line to print.
func FoldSession(ctx context.Context, store StoreProvider, compactor ManualCompactor, sessionID string) (FoldOutcome, error) {
	switch {
	case sessionID == "":
		return FoldOutcome{Why: "no session is bound to this chat yet"}, nil
	case IsEphemeralID(sessionID):
		return FoldOutcome{Why: "ephemeral sessions keep no history"}, nil
	}
	if store == nil {
		return FoldOutcome{}, ErrNoSessionContext
	}
	scx, ok := store.(SessionContextStore)
	if !ok {
		return FoldOutcome{}, ErrNoSessionContext
	}
	view, err := store.GetActiveView(ctx, sessionID)
	if err != nil {
		return FoldOutcome{}, err
	}
	path := StoredMessages(view.Path)
	if len(path) == 0 {
		return FoldOutcome{Why: "this conversation has no messages yet"}, nil
	}
	if compactor == nil {
		return FoldOutcome{Why: "the compaction module is not mounted"}, nil
	}
	folded, err := compactor.FoldNow(ctx, path)
	if err != nil {
		return FoldOutcome{}, err
	}
	if len(folded) == 0 {
		return FoldOutcome{Why: "the compactor had nothing to summarize (a short conversation, or compaction is off)"}, nil
	}
	// FoldSummary reads the head the compactor wrote; the rows after it are the tail the
	// fold kept verbatim, and they are what the boundary has to sit on.
	summary := FoldSummary(folded)
	kept := len(folded) - 1
	start := len(path) - kept
	if summary == "" {
		return FoldOutcome{}, errors.New("the compactor answered a fold without a summary")
	}
	if start <= 0 || start >= len(path) {
		return FoldOutcome{}, fmt.Errorf("fold kept %d of %d rows - no boundary to write", kept, len(path))
	}
	if err := scx.SetSessionContextStart(ctx, sessionID, core.MsgID(view.Path[start].ID), summary); err != nil {
		return FoldOutcome{}, err
	}
	return FoldOutcome{Folded: true, Start: core.MsgID(view.Path[start].ID), Rows: start, Kept: kept}, nil
}

// CompactLine is the one wording of a manual fold's answer: the fold's real numbers, or
// the real refusal. Hosts print this verbatim - never a hand-rolled variant.
func CompactLine(out FoldOutcome) string {
	if !out.Folded {
		return "nothing folded: " + out.Why
	}
	return fmt.Sprintf("folded %d messages into a summary - the context now starts at message %s, and the last %d stay verbatim",
		out.Rows, out.Start, out.Kept)
}

// FoldContext assembles a session's provider-facing history: the active path cut at the
// fold's context boundary and headed by its summary. A host that sends history itself (the
// CLI loop) must send THIS - a fold it just reported only reaches the next run when the
// boundary and its summary are applied here.
func FoldContext(ctx context.Context, store StoreProvider, sessionID string) ([]core.Message, error) {
	row, err := store.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	view, err := store.GetActiveView(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	msgs := make([]core.Message, 0, len(view.Path)+1)
	msgs = append(msgs, FoldSummaryPrefix(row.ContextSummary)...)
	return append(msgs, StoredMessages(FoldPath(view.Path, row.ContextStart))...), nil
}

// FoldPath cuts the active path at a folded conversation's context boundary.
func FoldPath(path []core.StoredMessage, start string) []core.StoredMessage {
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

// FoldSummaryPrefix heads a folded conversation's context with its summary. No summary (a
// fold too small to earn one, or a compactor that was not mounted) leaves the context
// exactly as the boundary left it.
func FoldSummaryPrefix(summary string) []core.Message {
	if summary == "" {
		return nil
	}
	return []core.Message{core.NewSystem(core.ContextFoldPrefix + summary + core.ContextFoldSuffix)}
}

// StoredMessages strips a tree view down to plain provider context.
func StoredMessages(path []core.StoredMessage) []core.Message {
	out := make([]core.Message, 0, len(path))
	for _, sm := range path {
		out = append(out, sm.Message)
	}
	return out
}

// FoldSummary pulls the summary out of a compacted message list.
func FoldSummary(folded []core.Message) string {
	for _, m := range folded {
		sys, ok := m.(core.System)
		if !ok {
			continue
		}
		if s, found := strings.CutPrefix(sys.Content, core.ContextFoldPrefix); found {
			if s, found := strings.CutSuffix(s, core.ContextFoldSuffix); found {
				return s
			}
		}
	}
	return ""
}
