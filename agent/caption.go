package agent

import (
	"context"
)

// Captioner moved to core with the Agent engine (arc 1b); the agent's
// RunWithProvider caption path references it. Consumers import it from
// core.Captioner (no re-exports at intermediate layers).

// CaptionStore persists computed captions keyed by image content.
// Optional: the captioner's in-process cache covers one process's
// lifetime; the sink is what makes turn 2 (and any other session that
// carries the same image) a cache hit after a restart. Implemented by
// the store extension as a separate narrow interface, so StoreProvider
// consumers never need to know captions exist.
type CaptionStore interface {
	// GetImageCaption returns the persisted caption for the image id
	// (sha256 of the payload, see CaptionStore's implementor), or
	// ok=false when none is stored. A read failure is an error, not a
	// miss.
	GetImageCaption(ctx context.Context, imageID string) (caption string, ok bool, err error)
	// SaveImageCaption persists one caption. Failure is logged and
	// ignored by the captioner - an unpersisted caption only costs a
	// later re-caption, never the turn.
	SaveImageCaption(ctx context.Context, imageID, caption string) error
}