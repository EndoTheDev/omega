package core

import (
	"context"
)

// Captioner describes images for models that cannot see them. Implementations own the
// caching, and a nil captioner sends images to the main provider unchanged - which
// fails loudly when that provider has no vision.
type Captioner interface {
	Caption(ctx context.Context, img ImageContent) (string, error)
}