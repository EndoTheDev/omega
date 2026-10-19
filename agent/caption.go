package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/EndoTheDev/omega/ai"
)

// Captioner describes images for models that cannot see them. When the
// vision_model knob is set, the agent runs it once per image on a user
// message before the loop (the captioner seam lives HERE, not in any
// channel - webui, TUI, and cron all inherit through Run/RunWithProvider).
// glm-5.3 (the main model) reasons over captions, never over pixels.
//
// Implementations own the caching: captioning the same image twice is a
// cache hit, never a second provider call. A nil captioner changes
// nothing - images ride to the main provider as today, and a provider
// that cannot see them fails loudly (the intentional fallback when
// vision_model is empty).
type Captioner interface {
	// Caption returns a factual description of one image.
	Caption(ctx context.Context, img ai.ImageContent) (string, error)
}

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

// applyCaptions rewrites image-carrying user messages for the MAIN
// model: each image is described once (the Captioner owns the cache)
// and the loop-visible copy carries the caption as a text block with
// the binary stripped, so a caption-only main model never receives
// image content. The input slice and its messages are left untouched -
// channels persist and re-render the originals.
//
// A captioning failure is loud and attributed: the run ends with an
// error naming the captioner, not a provider 400 masquerading as a
// model refusal.
func (a *Agent) applyCaptions(ctx context.Context, messages []ai.Message) ([]ai.Message, error) {
	if a.captioner == nil {
		return messages, nil
	}
	hasImages := false
	for _, m := range messages {
		if u, ok := m.(ai.User); ok && len(u.Images) > 0 {
			hasImages = true
			break
		}
	}
	if !hasImages {
		return messages, nil
	}
	out := make([]ai.Message, len(messages))
	copy(out, messages)
	for i, m := range out {
		u, ok := m.(ai.User)
		if !ok || len(u.Images) == 0 {
			continue
		}
		blocks := make([]string, 0, len(u.Images)+1)
		if text := strings.TrimRight(u.Content, "\n"); text != "" {
			blocks = append(blocks, text)
		}
		for _, img := range u.Images {
			caption, err := a.captioner.Caption(ctx, img)
			if err != nil {
				return nil, fmt.Errorf("image captioner: %w", err)
			}
			blocks = append(blocks, "[image: "+caption+"]")
		}
		captioned := u
		captioned.Content = strings.Join(blocks, "\n")
		captioned.Images = nil
		out[i] = captioned
	}
	return out, nil
}