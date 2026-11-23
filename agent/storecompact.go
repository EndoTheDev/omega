package agent

// storecompact.go: the durable half of an in-run compact. A run compacts its own in-memory
// wire; the store must learn the block, or the next run derives the uncompacted history
// and pays for a second summary. The run context carries the recorder, installed wherever
// the run already carries a store-backed turn sink.

import (
	"context"
	"errors"
	"fmt"

	"github.com/EndoTheDev/omega/core"
)

// CompactRecorder is the run's durable compact seam.
type CompactRecorder interface {
	// RecordCompact appends a compact's block for the span the summarizer just replaced,
	// retaining the last keep live messages verbatim. It answers the block it wrote.
	RecordCompact(ctx context.Context, summary string, keep int) (core.ContextBlock, error)
}

type compactRecorderKey struct{}

// WithCompactRecorder returns a child context carrying the run's recorder.
func WithCompactRecorder(ctx context.Context, rec CompactRecorder) context.Context {
	if rec == nil {
		return ctx
	}
	return context.WithValue(ctx, compactRecorderKey{}, rec)
}

// CompactRecorderFromContext returns the run's recorder, or nil when the run cannot
// persist a compact (title runs, one-shot CLI, an unmounted store).
func CompactRecorderFromContext(ctx context.Context) CompactRecorder {
	rec, _ := ctx.Value(compactRecorderKey{}).(CompactRecorder)
	return rec
}

type compactForcedKey struct{}

// WithCompactForced marks a compact the emergency arm asks for: a provider already
// answered "context overflow", so the pacing gate stands down - the guarantee is the
// request that must go out, not the growth number.
func WithCompactForced(ctx context.Context) context.Context {
	return context.WithValue(ctx, compactForcedKey{}, true)
}

// CompactForced reports whether the compact was asked for by the emergency arm.
func CompactForced(ctx context.Context) bool {
	forced, _ := ctx.Value(compactForcedKey{}).(bool)
	return forced
}

// storeCompactRecorder writes blocks into the session the run persists into.
type storeCompactRecorder struct {
	store     StoreProvider
	sessionID string
}

func (r *storeCompactRecorder) RecordCompact(ctx context.Context, summary string, keep int) (core.ContextBlock, error) {
	if summary == "" {
		return core.ContextBlock{}, errors.New("compact recorder: no summary to record")
	}
	blocks, ok := r.store.(SessionBlockStore)
	if !ok {
		return core.ContextBlock{}, ErrNoSessionContext
	}
	// The plan is re-derived from the truth, never taken from the caller's snapshot: the
	// span this block names is the one the store's own wire would drop.
	plan, err := PlanContext(ctx, r.store, r.sessionID)
	if err != nil {
		return core.ContextBlock{}, err
	}
	from, to, keepID, _, ok := CompactSpan(plan, keep)
	if !ok {
		return core.ContextBlock{}, fmt.Errorf("compact recorder: no compactable span at keep=%d", keep)
	}
	return blocks.AppendCompact(ctx, r.sessionID, core.CompactWrite{
		Summary: summary,
		From:    from,
		To:      to,
		Keep:    keepID,
		Tier:    1,
	})
}
