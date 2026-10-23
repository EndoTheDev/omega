package main

import (
	"testing"
)

// TestCompareVersions pins the semver precedence the update gates rely
// on: nightly < stable with the same base, numeric (not string) core
// field ordering, and prerelease identifier rules.
func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int // compareVersions(a, b)
		ok   bool
	}{
		// Nightly sorts BELOW stable with the same base version.
		{"v0.9.1-nightly.20261021", "v0.9.1", -1, true},
		{"v0.9.1", "v0.9.1-nightly.20261021", 1, true},
		// A nightly user must be offer-able a newer stable...
		{"v0.9.1", "v0.9.0-n.b", 1, true},
		{"v0.9.2-nightly.20261021", "v0.9.2", -1, true},
		// Numeric core compare, not string: 0.9.2 < 0.9.10.
		{"v0.9.2", "v0.9.10", -1, true},
		{"v0.9.10", "v0.9.2", 1, true},
		{"v0.9.1", "v0.9.1", 0, true},
		{"v0.10.0", "v0.9.1", 1, true},
		{"v1.0.0", "v0.99.9", 1, true},
		// Nightly dates compare numerically.
		{"v0.9.1-nightly.20261020", "v0.9.1-nightly.20261021", -1, true},
		{"v0.9.1-nightly.20261104", "v0.9.1-nightly.20261021", 1, true},
		// Cross-base: a newer base outranks any older-base prerelease.
		{"v0.9.2-nightly.20261020", "v0.9.1", 1, true},
		// Prerelease identifier rules.
		{"v0.9.1-alpha", "v0.9.1-beta", -1, true},
		{"v0.9.1-nightly.9", "v0.9.1-nightly.10", -1, true}, // numeric, not lexicographic
		{"v0.9.1-alpha", "v0.9.1-alpha.1", -1, true},        // fewer ids sort below more
		{"v0.9.1-1", "v0.9.1-alpha", -1, true},              // numeric ids below alphanumeric
		// Unparseable inputs must not guess a direction.
		{"dev", "v0.9.1", 0, false},
		{"v0.9.1", "dev", 0, false},
		{"v0.9", "v0.9.1", 0, false},     // two-field core
		{"v0.9.1.0", "v0.9.1", 0, false}, // four-field core
		{"v0.9.1-", "v0.9.1", 0, false},  // empty prerelease
		{"va.b.c", "v0.9.1", 0, false},   // non-numeric core
	}
	for _, tt := range tests {
		got, ok := compareVersions(tt.a, tt.b)
		if got != tt.want || ok != tt.ok {
			t.Errorf("compareVersions(%q, %q) = (%d, %v), want (%d, %v)", tt.a, tt.b, got, ok, tt.want, tt.ok)
		}
	}
}

func TestIsNightlyTag(t *testing.T) {
	yes := []string{"v0.9.1-nightly.20261021", "v1.0.0-nightly.1", "v2.0.0-nightly.20261231"}
	for _, tag := range yes {
		if !isNightlyTag(tag) {
			t.Errorf("isNightlyTag(%q) = false, want true", tag)
		}
	}
	no := []string{"v0.9.1", "v0.9.2-rc.1", "v0.9.2-beta.1", "nightly", "v0.9.1-prenightly.2", "v0.9.1-NIGHTLY.x"}
	for _, tag := range no {
		if isNightlyTag(tag) {
			t.Errorf("isNightlyTag(%q) = true, want false", tag)
		}
	}
}

// TestUpdateGate pins the downgrade-refusal contract: equal skips,
// newer installs, older refuses unless explicitly allowed, unparseable
// current versions never gate.
func TestUpdateGate(t *testing.T) {
	decide := func(offered, current string, allow bool) string {
		gate, err := updateGate(offered, current, allow)
		if err != nil {
			return "error:" + err.Error()
		}
		return gate
	}
	if got := decide("v0.9.2", "v0.9.1", false); got != "install" {
		t.Errorf("newer offered: %q, want install", got)
	}
	if got := decide("v0.9.1", "v0.9.1", false); got != "skip" {
		t.Errorf("same offered: %q, want skip", got)
	}
	if got := decide("v0.9.1", "v0.9.2", false); got == "install" {
		t.Error("downgrade offered: installed, want refuse")
	}
	// Nightly → stable with the same base is an upgrade by semver.
	if got := decide("v0.9.2", "v0.9.2-nightly.20261021", false); got != "install" {
		t.Errorf("nightly->stable same base: %q, want install", got)
	}
	if got := decide("v0.9.1", "v0.9.2-nightly.20261021", false); got == "install" {
		t.Error("stable user offered older nightly: installed, want refuse")
	}
	got, err := updateGate("v0.9.1", "v0.9.2", false)
	if got != "refuse" || err == nil {
		t.Errorf("refusal must carry an explanatory error, got (%q, %v)", got, err)
	}
	if got := decide("v0.9.1", "v0.9.2", true); got != "install" {
		t.Errorf("downgrade with --allow-downgrade: %q, want install", got)
	}
	// Dev builds (unparseable current) install without gating.
	if got := decide("v0.9.1", "dev", false); got != "install" {
		t.Errorf("dev current offered newer: %q, want install", got)
	}
	if got := decide("v0.1.0", "dev", false); got != "install" {
		t.Errorf("dev current offered older: %q, want install (no gate)", got)
	}
	// Short-stamped running versions (tag-hash, card t_8acdfe17): the
	// hash is build metadata, invisible to the gate. A same-tag nightly
	// offered is "skip", the next nightly installs, an older one still
	// refuses.
	if got := decide("v0.9.1-nightly.20261022", "v0.9.1-nightly.20261022-c268097e", false); got != "skip" {
		t.Errorf("offered the running build's own tag: %q, want skip", got)
	}
	if got := decide("v0.9.1-nightly.20261023", "v0.9.1-nightly.20261022-c268097e", false); got != "install" {
		t.Errorf("next nightly offered: %q, want install (hash must not read as downgrade)", got)
	}
	if got := decide("v0.9.1-nightly.20261021", "v0.9.1-nightly.20261022-c268097e", false); got == "install" {
		t.Error("older nightly offered: installed, want refuse")
	}
	if got := decide("v0.9.2", "v0.9.1-c268097e", false); got != "install" {
		t.Errorf("stable offered to short-stamped nightly running version: %q, want install", got)
	}
}

func TestParseUpdateFlags(t *testing.T) {
	opts, err := parseUpdateFlags([]string{"--nightly", "--allow-downgrade"})
	if err != nil {
		t.Fatalf("flags: %v", err)
	}
	if !opts.nightly || !opts.allowDowngrade || opts.rollback {
		t.Errorf("opts = %+v", opts)
	}
	if _, err := parseUpdateFlags([]string{"--nighty"}); err == nil {
		t.Error("typo'd --nighty must error, not fall through to stable")
	}
	if _, err := parseUpdateFlags(nil); err != nil {
		t.Errorf("no flags: %v", err)
	}
	opts, err = parseUpdateFlags([]string{"arg.txt"})
	if err != nil || opts.nightly {
		t.Errorf("positional arg ignored: %+v, %v", opts, err)
	}
}
