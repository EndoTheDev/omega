package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/ai"
	"github.com/EndoTheDev/omega/core"
)

// captionPrompt is the deterministic instruction the vision model gets
// once per image. Cheap, factual, plain text - the main model (glm-5.3)
// reasons over the description, never over pixels.
const captionPrompt = "Describe this image factually and completely, plain text."

// captionTimeout caps one vision-model call. The shared HTTP timeout is
// a ceiling for hung requests, but a wedged vision model should fail
// the turn fast instead of holding the run slot (the caption runs
// inside the run's wall clock).
const captionTimeout = 2 * time.Minute

// Captioner describes image content with the vision_model. It is the
// core.Captioner implementation for the vision-model knob: one pinned
// provider copy serves the vision model, the process-wide memo plus the
// store sink (agent.CaptionStore) make repeat appearances of the same
// image a cache hit - the caption is computed ONCE per image and
// persisted, never re-run for old images.
type Captioner struct {
	prov  ai.Provider // pinned copy serving the vision model
	sink  agent.CaptionStore
	cache sync.Map // image id -> caption string
	log   core.LoggerProvider
}

// NewCaptioner builds the captioner from the BASE provider: a WithModel
// copy pinned at construction serves the vision model, so /model
// switches (which mutate the base) and per-request copies never bleed
// into caption calls. model empty makes Caption fail loudly - but the
// host only wires the captioner when vision_model is set, so that is a
// misconfiguration guard, not a mode.
func NewCaptioner(base ai.Provider, model string, sink agent.CaptionStore, log core.LoggerProvider) *Captioner {
	if base == nil {
		return &Captioner{sink: sink, log: log}
	}
	prov := base
	if model != "" {
		prov = base.WithModel(model)
	}
	return &Captioner{prov: prov, sink: sink, log: log}
}

// Caption returns the caption for one image: process memo -> store ->
// one vision-model call (persisted on success). Implements
// core.Captioner.
func (c *Captioner) Caption(ctx context.Context, img core.ImageContent) (string, error) {
	id := ImageID(img)
	if v, ok := c.cache.Load(id); ok {
		return v.(string), nil
	}
	if c.sink != nil {
		if caption, ok, err := c.sink.GetImageCaption(ctx, id); err != nil {
			c.logf("caption store read %s: %v", shortID(id), err)
		} else if ok {
			c.cache.Store(id, caption)
			return caption, nil
		}
	}
	if c.prov == nil {
		return "", fmt.Errorf("no vision model configured")
	}
	start := time.Now()
	caption, err := c.describe(ctx, img)
	if err != nil {
		return "", err
	}
	s, ok := c.cache.LoadOrStore(id, caption)
	if ok {
		// A concurrent caption beat this one; the winner is
		// identical by construction (same prompt, same model),
		// keep the loser's work out of the sink.
		return s.(string), nil
	}
	if c.sink != nil {
		if err := c.sink.SaveImageCaption(ctx, id, caption); err != nil {
			c.logf("caption store write %s: %v", shortID(id), err)
		}
	}
	if c.log != nil {
		c.logf("captioned %s with %s in %s", shortID(id), c.prov.ModelName(), time.Since(start).Round(time.Millisecond))
	}
	return caption, nil
}

// describe runs ONE vision-model call for one image and collects the
// streamed response.
func (c *Captioner) describe(ctx context.Context, img core.ImageContent) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, captionTimeout)
	defer cancel()
	user := core.User{
		Content:   captionPrompt,
		Images:    []core.ImageContent{img},
		Timestamp: core.NowISO(),
	}
	var sb strings.Builder
	for event := range c.prov.Stream(ctx, []core.Message{user}, nil) {
		switch e := event.(type) {
		case core.ResponseChunk:
			sb.WriteString(e.Content)
		case core.StreamEnd:
			if e.Error != "" {
				return "", fmt.Errorf("vision model %s: %s", c.prov.ModelName(), e.Error)
			}
		}
	}
	caption := strings.TrimSpace(sb.String())
	if caption == "" {
		return "", fmt.Errorf("vision model %s returned an empty caption", c.prov.ModelName())
	}
	return caption, nil
}

// ImageID is the content-addressed caption key: media type plus the
// sha256 of the base64 payload. The same bytes in any session, message,
// or process share one caption.
func ImageID(img core.ImageContent) string {
	sum := sha256.Sum256([]byte(img.Base64))
	return img.MediaType + "-" + hex.EncodeToString(sum[:])
}

// shortID trims the image id to its leading hex run for log lines.
func shortID(id string) string {
	if i := strings.IndexByte(id, '-'); i >= 0 && i <= 10 {
		return id[:i+5]
	}
	if len(id) > 16 {
		return id[:16]
	}
	return id
}

func (c *Captioner) logf(format string, args ...any) {
	if c.log != nil {
		c.log.Printf("captioner: "+format, args...)
	}
}
