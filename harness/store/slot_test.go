package store

import (
	"context"
	"testing"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/agent"
)

// The summarizer's model slot (card t_fc0b2f2d): an "instance:model"
// value routes the hygiene consult through the named provider
// instance; a bare value (or an unconfigured prefix) stays on the
// default instance. The summary text identifies the routed provider.

// newSlotSession seeds ONE settled aged session so the pass has one
// candidate.
func newSlotSession(t *testing.T) (context.Context, *Store) {
	t.Helper()
	s := newFileStore(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, "slot-s1", "", "the fibonacci task"); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMessage(ctx, "slot-s1", core.NewUser("compute the first ten fibonacci numbers")); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMessage(ctx, "slot-s1", core.Assistant{Content: "1, 1, 2, 3, 5, 8, 13, 21, 34, 55"}); err != nil {
		t.Fatal(err)
	}
	ageSession(t, s, "slot-s1", 2) // two days idle: past the 24h floor
	return ctx, s
}

// slotProviders builds the default summarizer with one named instance;
// the "big:<model>" slot routes there, everything else stays.
func slotProviders() *agent.FakeProvider {
	base := fakeSummary("base-consult")
	base.SetInstances(map[string]agent.Provider{"big": fakeSummary("instance-consult")})
	return base
}

func slotSummary(t *testing.T, s *Store) string {
	t.Helper()
	ctx := context.Background()
	sess, err := s.GetSession(ctx, "slot-s1")
	if err != nil {
		t.Fatal(err)
	}
	return sess.Summary
}

func TestSummarizerSlotRoutesInstance(t *testing.T) {
	ctx, s := newSlotSession(t)
	prov := slotProviders()
	rep, err := s.RunSummarizer(ctx, prov, "big:x-model", 24)
	if err != nil {
		t.Fatalf("RunSummarizer: %v (skipped: %v)", err, rep.Skipped)
	}
	if rep.Summarized != 1 {
		t.Fatalf("summarized %d, want 1 (skipped: %v)", rep.Summarized, rep.Skipped)
	}
	if got := slotSummary(t, s); got != "instance-consult" {
		t.Errorf("slot %q routed wrong provider: summary = %q", "big:x-model", got)
	}
}

// A bare slot (and an unconfigured instance prefix over it) stays on
// the default instance.
func TestSummarizerSlotBareStays(t *testing.T) {
	ctx, s := newSlotSession(t)
	prov := slotProviders()
	rep, err := s.RunSummarizer(ctx, prov, "gemma3:27b", 24)
	if err != nil {
		t.Fatalf("RunSummarizer: %v (skipped: %v)", err, rep.Skipped)
	}
	if got := slotSummary(t, s); got != "base-consult" {
		t.Errorf("bare slot must stay on the default instance: summary = %q", got)
	}
}

// An empty slot keeps today's consult (the default instance, main
// model).
func TestSummarizerSlotEmpty(t *testing.T) {
	ctx, s := newSlotSession(t)
	prov := slotProviders()
	rep, err := s.RunSummarizer(ctx, prov, "", 24)
	if err != nil {
		t.Fatalf("RunSummarizer: %v (skipped: %v)", err, rep.Skipped)
	}
	if got := slotSummary(t, s); got != "base-consult" {
		t.Errorf("empty slot: summary = %q", got)
	}
}
