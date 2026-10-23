package core

import (
	"context"
	"fmt"
	"strings"
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
	Caption(ctx context.Context, img ImageContent) (string, error)
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
func (a *Agent) applyCaptions(ctx context.Context, messages []Message) ([]Message, error) {
	if a.captioner == nil {
		return messages, nil
	}
	hasImages := false
	for _, m := range messages {
		if u, ok := m.(User); ok && len(u.Images) > 0 {
			hasImages = true
			break
		}
	}
	if !hasImages {
		return messages, nil
	}
	out := make([]Message, len(messages))
	copy(out, messages)
	for i, m := range out {
		u, ok := m.(User)
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
