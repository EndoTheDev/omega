package core

import (
	"context"
)

// Captioner describes images for models that cannot see them. When the
// vision_model knob is set, the agent runs it once per image on a user
// message before the loop (the captioner seam lives HERE, not in any
// channel - webui, the CLI loop, and cron all inherit through Run/RunWithProvider).
// glm-5.3 (the main model) reasons over captions, never over pixels.
//
// Implementations own the caching: captioning the same image twice is a
// cache hit, never a second provider call. A nil captioner changes
// nothing - images ride to the main provider as today, and a provider
// that cannot see them fails loudly (the intentional fallback when
// vision_model is empty).
type Captioner interface {
	// Caption returns a factual description of one image.
	Caption(ctx context.Context, img ImageContent) (string, error)
}