package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/EndoTheDev/omega/core"
)

// visionCapability is the registry flag a model carries when it accepts
// image input (the ollama-family /api/tags "capabilities" array, passed
// through by the keypool mirror: ["completion","tools","thinking",
// "vision","decision"]).
const visionCapability = "vision"

// modelSeesImages reports whether the run's SELECTED model declares the
// vision capability in its provider's model registry
// (agent.ModelCapabilitiesProvider). The upgrade is probed at runtime:
// a name-only provider (openai/anthropic id lists), a model missing
// from the listing, and a listing failure all answer false - the aux
// hop stays, which is the pre-existing behavior.
func modelSeesImages(p Provider) bool {
	lp, ok := p.(ModelCapabilitiesProvider)
	if !ok {
		return false
	}
	entries, err := lp.ListModelEntries()
	if err != nil {
		return false
	}
	name := p.ModelName()
	for _, e := range entries {
		if e.Name != name {
			continue
		}
		for _, c := range e.Capabilities {
			if c == visionCapability {
				return true
			}
		}
		return false
	}
	return false
}

// applyCaptions rewrites image-carrying user messages for the MAIN
// model: each image is described once (the Captioner owns the cache)
// and the loop-visible copy carries the caption as a text block with
// the binary stripped, so a caption-only main model never receives
// image content. A main model the registry marks vision-capable is
// left alone - the pixels ride to it directly and the auxiliary hop is
// skipped (no duplicate image processing).
// The input slice and its messages are left untouched -
// channels persist and re-render the originals.
//
// A captioning failure is loud and attributed: the run ends with an
// error naming the captioner, not a provider 400 masquerading as a
// model refusal. Moved with the Agent machinery (part 3, wave 2) - the
// agent consumes the captioner; core keeps the face (core.Captioner).
func (a *Agent) applyCaptions(ctx context.Context, provider Provider, messages []core.Message) ([]core.Message, error) {
	if a.captioner == nil {
		return messages, nil
	}
	hasImages := false
	for _, m := range messages {
		if u, ok := m.(core.User); ok && len(u.Images) > 0 {
			hasImages = true
			break
		}
	}
	if !hasImages {
		return messages, nil
	}
	// The aux hop exists for a BLIND main model. When the selected model
	// declares vision in its registry, the pixels go to it directly:
	// captioning would duplicate the image work (two provider calls, two
	// image passes) for a model that can already see.
	if modelSeesImages(provider) {
		if a.logger != nil {
			a.logger.Printf("captioner: %s sees images - auxiliary vision hop skipped", provider.ModelName())
		}
		return messages, nil
	}
	out := make([]core.Message, len(messages))
	copy(out, messages)
	for i, m := range out {
		u, ok := m.(core.User)
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

// Captioner moved to core with the agent (arc 1b); the agent's
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
