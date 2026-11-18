package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/EndoTheDev/omega/core"
)

// visionCapability is the registry flag a model carries when it accepts image input.
const visionCapability = "vision"

// captionSwitch is the consumer-owned face a captioner MAY satisfy to report whether.
// The host installs the captioner from boot so the vision_model knob can flip live,
// which makes "off" a state rather than a missing pointer: a captioner that answers
// false is skipped exactly like a nil one (images ride to the main provider and a blind
// model fails loudly - the documented fallback).
type captionSwitch interface {
	CaptioningEnabled() bool
}

// captionerActive reports whether the installed captioner should run.
func captionerActive(c core.Captioner) bool {
	if c == nil {
		return false
	}
	if sw, ok := c.(captionSwitch); ok {
		return sw.CaptioningEnabled()
	}
	return true
}

// modelSeesImages reports whether the run's SELECTED model declares the vision.
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

// captionProviderKey is the context key carrying the provider the run itself streams.
// An auxiliary lane stored under the "default" sentinel reads it per call, so the
// choice never freezes to whatever model (or provider instance) happened to be selected
// when the knob was saved.
type captionProviderKey struct{}

// WithCaptionProvider stamps the run's provider onto the context for the auxiliary.
func WithCaptionProvider(ctx context.Context, provider Provider) context.Context {
	if provider == nil {
		return ctx
	}
	return context.WithValue(ctx, captionProviderKey{}, provider)
}

// CaptionProvider returns the run provider WithCaptionProvider recorded (nil for a.
func CaptionProvider(ctx context.Context) Provider {
	provider, _ := ctx.Value(captionProviderKey{}).(Provider)
	return provider
}

// applyCaptions rewrites image-carrying user messages for the MAIN model: each image is
// described once (the Captioner owns the cache) and the loop-visible copy carries the
// caption as a text block with the binary stripped, so a caption-only main model never
// receives image content. A main model the registry marks vision-capable is left alone
// - the pixels ride to it directly and the auxiliary hop is skipped (no duplicate image
// processing).
func (a *Agent) applyCaptions(ctx context.Context, provider Provider, messages []core.Message) ([]core.Message, error) {
	if !captionerActive(a.captioner) {
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
	// The aux hop exists for a BLIND main model. When the selected model declares vision
	// in its registry, the pixels go to it directly: captioning would duplicate the image
	// work (two provider calls, two image passes) for a model that can already see.
	if modelSeesImages(provider) {
		if a.logger != nil {
			a.logger.Printf("captioner: %s sees images - auxiliary vision hop skipped", provider.ModelName())
		}
		return messages, nil
	}
	// The default-sentinel aux lane resolves its provider per call.
	ctx = WithCaptionProvider(ctx, provider)
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

// Captioner moved to core with the agent (arc 1b).

// CaptionStore persists computed captions keyed by image content. Implemented by the
// store extension as a separate narrow interface, so StoreProvider consumers never need
// to know captions exist.
type CaptionStore interface {
	// GetImageCaption returns the persisted caption for the image id (sha256 of the.
	GetImageCaption(ctx context.Context, imageID string) (caption string, ok bool, err error)
	// SaveImageCaption persists one caption. Failure is logged and ignored by the
	// captioner - an unpersisted caption only costs a later re-caption, never the turn.
	SaveImageCaption(ctx context.Context, imageID, caption string) error
}
