package compactor

import (
	"context"
	"testing"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/engine/agent"
	"github.com/EndoTheDev/omega/engine/ai"
)

// contextWindowProvider wraps a provider with a forced ModelInfo
// result, so the auto-probe path can be asserted against a known
// window (and a known failure).
type contextWindowProvider struct {
	ai.Provider
	info  ai.ModelInfo
	err   error
	calls int
}

func (p *contextWindowProvider) ModelInfo() (ai.ModelInfo, error) {
	p.calls++
	return p.info, p.err
}

func autoTestCompactor(p *contextWindowProvider, threshold float64) *Compactor {
	return &Compactor{
		provider:   p,
		config:     core.CompactionConfig{Enabled: true, Threshold: threshold, KeepFirst: 1, KeepLast: 1},
		autoWindow: 100000,
	}
}

// Auto probe feeds the budget: window 100000, reserve defaults to
// 16384, effective 83680; at threshold 0.6 the budget is 50208. The
// same history stays under a manual 32768 window's budget (9840) and
// triggers compaction against the probed one.
var autoMsgs = []core.Message{
	core.NewUser("first padded message to survive keep_first"),
	core.NewUser(strings_repeat("middle filler ", 700)),
	core.NewUser("middle filler two padded"),
	core.NewUser("last padded message to survive keep_last"),
}

func strings_repeat(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}

func TestAutoWindowDrivesBudget(t *testing.T) {
	// ~2900 chars -> ~725 tokens: under the manual-32768 budget but
	// over the probed-100000-window threshold. Actually this asserts
	// the arithmetic directly through BudgetWith/window below.
	c := autoTestCompactor(&contextWindowProvider{}, 0.6)
	if w := c.window(); w != 100000 {
		t.Fatalf("window() = %d, want 100000 (manual > 0 wins)", w)
	}
	if got := c.config.BudgetWith(c.window()); got != 50169 {
		t.Fatalf("budget against probed window = %d, want 50169", got)
	}
}

func TestManualOverrideWins(t *testing.T) {
	c := autoTestCompactor(&contextWindowProvider{}, 0.6)
	c.config.ContextWindow = 32768
	if w := c.window(); w != 32768 {
		t.Fatalf("window() = %d, want 32768", w)
	}
	if got := c.config.BudgetWith(c.window()); got != 9830 {
		// 32768 - 16384 = 16384; 16384 * 0.6 = 9830.4 -> 9830 (trunc)
		t.Fatalf("budget = %d, want 9830", got)
	}
}

func TestAutoWindowZeroFallsBack(t *testing.T) {
	c := autoTestCompactor(&contextWindowProvider{}, 0.6)
	c.autoWindow = 0 // probe found nothing
	if w := c.window(); w != 0 {
		t.Fatalf("window() = %d, want 0", w)
	}
	// BudgetWith falls back to DefaultContextWindow 8192; reserve
	// 16384 > half, so effective = 4096; 4096 * 0.6 = 2457.
	if got := c.config.BudgetWith(c.window()); got != 2457 {
		t.Fatalf("budget = %d, want 2457", got)
	}
}

func TestSetConfigKeepsAutoWindow(t *testing.T) {
	c := autoTestCompactor(&contextWindowProvider{}, 0.6)
	c.SetConfig(core.CompactionConfig{Enabled: true, Threshold: 1})
	if c.AutoWindow() != 100000 {
		t.Fatalf("AutoWindow changed by SetConfig: %d", c.AutoWindow())
	}
	if w := c.window(); w != 100000 {
		t.Fatalf("window() after SetConfig = %d, want 100000", w)
	}
}

func TestCompactUsesProbedWindow(t *testing.T) {
	// History of ~700*12 chars -> ~2100 tokens. Manual default-config
	// budget (window 0, no probe) would be 4096*0.6=2457 -> under
	// budget, no compaction. Probed window 3000: effective 1500,
	// threshold 1.0 -> budget 1500 -> compaction triggers.
	summary := "summary text"
	provider := &contextWindowProvider{
		Provider: ai.NewFakeProvider("test",
			core.ResponseChunk{Type: "response_chunk", Content: summary},
			core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		),
	}
	c := &Compactor{
		provider:   provider,
		config:     core.CompactionConfig{Enabled: true, Threshold: 1.0, KeepFirst: 1, KeepLast: 1},
		autoWindow: 3000,
	}
	msgs := []core.Message{
		core.NewUser("head message kept"),
		core.NewUser(strings_repeat("filler ", 1200)),
		core.NewUser("middle filler two"),
		core.NewUser("tail message kept"),
	}
	out, err := c.Compact(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 {
		t.Fatalf("expected compacted 3 messages, got %d", len(out))
	}
}

func TestMountProbesAutoWindowOnlyWhenConfigZero(t *testing.T) {
	p := &contextWindowProvider{info: ai.ModelInfo{ContextWindow: 123456}}
	ctx := &core.Context{Provider: p, Configs: map[string]any{"compactor": core.CompactionConfig{ContextWindow: 0}}}

	plugin := &Plugin{}
	// Auto: config 0 -> probe runs, window cached.
	if _, err := agent.MountAllAPI([]agent.Plugin{stubProvides{plugin: plugin, seam: "provider"}, plugin}, ctx, nil, nil); err != nil {
		t.Fatal(err)
	}
	c, ok := ctx.Compactor.(*Compactor)
	if !ok {
		t.Fatal("Mount did not install the compactor")
	}
	if c.AutoWindow() != 123456 || p.calls != 1 {
		t.Fatalf("auto probe: AutoWindow=%d calls=%d, want 123456/1", c.AutoWindow(), p.calls)
	}

	// Manual: config > 0 -> no probe at all.
	p2 := &contextWindowProvider{info: ai.ModelInfo{ContextWindow: 999999}}
	ctx2 := &core.Context{Provider: p2, Configs: map[string]any{"compactor": core.CompactionConfig{ContextWindow: 32768}}}
	if _, err := agent.MountAllAPI([]agent.Plugin{stubProvides{plugin: &Plugin{}, seam: "provider"}, &Plugin{}}, ctx2, nil, nil); err != nil {
		t.Fatal(err)
	}
	c2 := ctx2.Compactor.(*Compactor)
	if c2.AutoWindow() != 0 || p2.calls != 0 {
		t.Fatalf("manual config probed: AutoWindow=%d calls=%d, want 0/0", c2.AutoWindow(), p2.calls)
	}
}

func TestMountProbeFailureStillBoots(t *testing.T) {
	p := &contextWindowProvider{err: context.DeadlineExceeded}
	ctx := &core.Context{Provider: p, Configs: map[string]any{"compactor": core.CompactionConfig{ContextWindow: 0}}}
	if _, err := agent.MountAllAPI([]agent.Plugin{stubProvides{plugin: &Plugin{}, seam: "provider"}, &Plugin{}}, ctx, nil, nil); err != nil {
		t.Fatalf("Mount must not fail on probe error: %v", err)
	}
	c := ctx.Compactor.(*Compactor)
	if c.AutoWindow() != 0 {
		t.Fatalf("AutoWindow = %d, want 0", c.AutoWindow())
	}
	// Budget resolves through the DefaultContextWindow fallback:
	// threshold 0 -> budget 0 regardless of window.
	if got := c.config.BudgetWith(c.window()); got != 0 {
		t.Fatalf("budget = %d, want 0", got)
	}
}
