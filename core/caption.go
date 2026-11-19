package core

import (
	"context"
)

// Captioner describes images for models that cannot see them. Implementations own the
// caching; nil sends images on unchanged, which fails loudly if the provider has no vision.
type Captioner interface {
	Caption(ctx context.Context, img ImageContent) (string, error)
}