package main

import (
	"testing"

	glamouransi "github.com/charmbracelet/glamour/ansi"
)

// strVal safely dereferences a *string, returning "" when nil. Keeps the
// table-driven assertions below free of nil-pointer panics.
func strVal(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func TestGlamourStyleForTheme(t *testing.T) {
	// expectedColors maps a top-level StyleConfig field to the color value we
	// expect for each theme. Values come from the Catppuccin palette overrides
	// hardcoded in glamourStyleForTheme (see cmd/omega/tui.go).
	type fieldExpect struct {
		name    string
		get     func(glamouransi.StyleConfig) *string
		dark    string
		light   string
		bgField bool // true for BackgroundColor fields
	}

	cases := []struct {
		name      string
		themeName string
	}{
		{name: "dark", themeName: "dark"},
		{name: "light", themeName: "light"},
		{name: "unknown falls back to dark", themeName: "unknown"},
	}

	// Per-field color expectations. The "unknown" theme must match dark.
	fieldExpects := []fieldExpect{
		{"Document.Color", func(c glamouransi.StyleConfig) *string { return c.Document.Color }, "#cdd6f4", "#4c4f69", false},
		{"Heading.Color", func(c glamouransi.StyleConfig) *string { return c.Heading.Color }, "#89b4fa", "#1e66f5", false},
		{"H1.Color", func(c glamouransi.StyleConfig) *string { return c.H1.Color }, "#cdd6f4", "#4c4f69", false},
		{"H1.BackgroundColor", func(c glamouransi.StyleConfig) *string { return c.H1.BackgroundColor }, "#313244", "#ccd0da", true},
		{"H2.Color", func(c glamouransi.StyleConfig) *string { return c.H2.Color }, "#89b4fa", "#1e66f5", false},
		{"H3.Color", func(c glamouransi.StyleConfig) *string { return c.H3.Color }, "#cba6f7", "#8839ef", false},
		{"Code.Color", func(c glamouransi.StyleConfig) *string { return c.Code.Color }, "#f5c2e7", "#ea76cb", false},
		{"Code.BackgroundColor", func(c glamouransi.StyleConfig) *string { return c.Code.BackgroundColor }, "#313244", "#ccd0da", true},
		{"Link.Color", func(c glamouransi.StyleConfig) *string { return c.Link.Color }, "#74c7ec", "#209fb5", false},
		{"LinkText.Color", func(c glamouransi.StyleConfig) *string { return c.LinkText.Color }, "#89b4fa", "#1e66f5", false},
		{"BlockQuote.Color", func(c glamouransi.StyleConfig) *string { return c.BlockQuote.Color }, "#6c7086", "#8c8fa1", false},
		{"Table.Color", func(c glamouransi.StyleConfig) *string { return c.Table.Color }, "#9399b2", "#7c7f93", false},
		{"HorizontalRule.Color", func(c glamouransi.StyleConfig) *string { return c.HorizontalRule.Color }, "#585b70", "#bcc0cc", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := glamourStyleForTheme(tc.themeName)

			// Resolve which expectation set applies. "light" is the only
			// branch that deviates; everything else (incl. "unknown") must
			// match the dark theme.
			wantTheme := "dark"
			if tc.themeName == "light" {
				wantTheme = "light"
			}

			for _, fe := range fieldExpects {
				t.Run(fe.name, func(t *testing.T) {
					ptr := fe.get(got)
					if ptr == nil {
						t.Fatalf("%s: nil pointer for %s", tc.name, fe.name)
					}
					gotVal := strVal(ptr)
					var want string
					if wantTheme == "light" {
						want = fe.light
					} else {
						want = fe.dark
					}
					// BackgroundColor fields we didn't override are allowed to
					// be whatever the base StyleConfig sets; only assert the
					// ones we explicitly configure (H1 and Code).
					if fe.bgField && want == "" {
						return
					}
					if gotVal != want {
						t.Fatalf("%s: %s = %q, want %q", tc.name, fe.name, gotVal, want)
					}
				})
			}
		})
	}

	// Separate subtest: verify the set of StyleConfig blocks that must have a
	// non-nil Color pointer for both themes. This guards against a future
	// refactor that drops an override and leaves a nil deref downstream.
	nonNilColorBlocks := []struct {
		name string
		get  func(glamouransi.StyleConfig) *string
	}{
		{"Document.Color", func(c glamouransi.StyleConfig) *string { return c.Document.Color }},
		{"Heading.Color", func(c glamouransi.StyleConfig) *string { return c.Heading.Color }},
		{"H1.Color", func(c glamouransi.StyleConfig) *string { return c.H1.Color }},
		{"H2.Color", func(c glamouransi.StyleConfig) *string { return c.H2.Color }},
		{"H3.Color", func(c glamouransi.StyleConfig) *string { return c.H3.Color }},
		{"Code.Color", func(c glamouransi.StyleConfig) *string { return c.Code.Color }},
		{"Link.Color", func(c glamouransi.StyleConfig) *string { return c.Link.Color }},
		{"LinkText.Color", func(c glamouransi.StyleConfig) *string { return c.LinkText.Color }},
		{"BlockQuote.Color", func(c glamouransi.StyleConfig) *string { return c.BlockQuote.Color }},
		{"Table.Color", func(c glamouransi.StyleConfig) *string { return c.Table.Color }},
		{"HorizontalRule.Color", func(c glamouransi.StyleConfig) *string { return c.HorizontalRule.Color }},
	}

	for _, theme := range []string{"dark", "light"} {
		t.Run("non-nil/"+theme, func(t *testing.T) {
			cfg := glamourStyleForTheme(theme)
			for _, blk := range nonNilColorBlocks {
				t.Run(blk.name, func(t *testing.T) {
					if blk.get(cfg) == nil {
						t.Fatalf("%s theme: %s is nil, expected non-nil", theme, blk.name)
					}
				})
			}
		})
	}

	// Confirm "unknown" and "dark" produce identical configs — the fallback
	// must be byte-for-byte the same, not merely "dark-ish".
	t.Run("unknown equals dark", func(t *testing.T) {
		dark := glamourStyleForTheme("dark")
		unknown := glamourStyleForTheme("unknown")
		for _, fe := range fieldExpects {
			d := strVal(fe.get(dark))
			u := strVal(fe.get(unknown))
			if d != u {
				t.Fatalf("unknown vs dark mismatch on %s: unknown=%q dark=%q", fe.name, u, d)
			}
		}
	})
}