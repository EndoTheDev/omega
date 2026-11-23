package agent

import (
	"context"

	"github.com/EndoTheDev/omega/core"
)

// The steer seam: input entering a LIVE turn. The loop drains it at each turn boundary -
// after the previous turn's tool batch, before the next provider call, never mid-stream.

// SteerDelivery is one queued input handed to a live turn. ID names the source queue row
// so the surface retires it on delivery; it never reaches the transcript.
type SteerDelivery struct {
	ID      string
	Message core.Message
}

// SteerFeed drains the session's queued input for the live turn (run goroutine only).
type SteerFeed func() []SteerDelivery

type steerFeedKey struct{}

// WithSteerFeed installs the run's queued-input drain.
func WithSteerFeed(ctx context.Context, feed SteerFeed) context.Context {
	if feed == nil {
		return ctx
	}
	return context.WithValue(ctx, steerFeedKey{}, feed)
}

// SteerFeedFromContext returns the run's queued-input drain; nil means the host has no
// queue (the documented floor: a run takes no mid-turn input).
func SteerFeedFromContext(ctx context.Context) SteerFeed {
	feed, _ := ctx.Value(steerFeedKey{}).(SteerFeed)
	return feed
}
