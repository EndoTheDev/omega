package store

import (
	"context"
	"testing"
)

// The image_captions roundtrip behind the captioner's store sink:
// miss (not an error), save, hit, and the once-per-image discipline
// (re-saving the same id keeps the first caption).
func TestImageCaptionsRoundtrip(t *testing.T) {
	st, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	ctx := context.Background()

	if caption, ok, err := st.GetImageCaption(ctx, "img-1"); err != nil || ok || caption != "" {
		t.Fatalf("miss: caption=%q ok=%v err=%v", caption, ok, err)
	}
	if err := st.SaveImageCaption(ctx, "img-1", "Red."); err != nil {
		t.Fatalf("save: %v", err)
	}
	caption, ok, err := st.GetImageCaption(ctx, "img-1")
	if err != nil || !ok || caption != "Red." {
		t.Fatalf("hit: caption=%q ok=%v err=%v", caption, ok, err)
	}
	// A cached caption is never rewritten.
	if err := st.SaveImageCaption(ctx, "img-1", "rewritten"); err != nil {
		t.Fatalf("re-save: %v", err)
	}
	caption, ok, err = st.GetImageCaption(ctx, "img-1")
	if err != nil || !ok || caption != "Red." {
		t.Fatalf("after re-save: caption=%q ok=%v err=%v (first caption must stand)", caption, ok, err)
	}
}
