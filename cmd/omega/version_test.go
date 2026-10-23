package main

import (
	"runtime/debug"
	"testing"
)

// deriveVersion extracts the module version the way
// resolveVersionFromBuildInfo does - kept as a pure helper for the test.
func deriveVersion(bi *debug.BuildInfo, ok bool) string {
	if !ok {
		return "dev"
	}
	if v := bi.Main.Version; v != "" && v != "(devel)" {
		return shortBuildVersion(v)
	}
	return "dev"
}

// TestVersionShape pins the one shape a version string may take
// (card t_8acdfe17): tag + optional -nightly.DATE + optional -8hexhash,
// or "dev" for builds that carry no version at all.
func TestVersionShape(t *testing.T) {
	shaped := []string{
		"v0.9.1",
		"v1.0.0",
		"v0.9.1-nightly.20261022",
		"v0.9.1-c268097e",
		"v0.9.1-nightly.20261022-c268097e",
	}
	for _, v := range shaped {
		t.Run(v, func(t *testing.T) {
			if !versionShapeRe.MatchString(v) {
				t.Errorf("version %q does not match the version shape", v)
			}
		})
	}
	if versionShapeRe.MatchString("dev") {
		t.Error("dev must not match the version shape")
	}
}

// TestVersionFromInfo pins the VCS-derived fallback: a go stamped pseudo
// version collapses to tag + "-" + 8 hash chars, a plain tag passes
// through, and builds with no module stamp report "dev".
func TestVersionFromInfo(t *testing.T) {
	cases := []struct {
		name string
		bi   *debug.BuildInfo
		ok   bool
		want string
	}{
		// go install path: Go stamps the module tag into module builds
		{"module stamp", &debug.BuildInfo{Main: debug.Module{Version: "v0.6.1"}}, true, "v0.6.1"},
		// nightly pseudo-version: base tag + first 8 hash chars
		{"nightly pseudo", &debug.BuildInfo{Main: debug.Module{Version: "v0.9.1-nightly.20261022.0.20261023012859-c268097e2689"}}, true, "v0.9.1-nightly.20261022-c268097e"},
		// stable pseudo-version (no prerelease base): same collapse
		{"stable pseudo", &debug.BuildInfo{Main: debug.Module{Version: "v0.9.1.0.20261023012859-abcd1234ef01"}}, true, "v0.9.1-abcd1234"},
		// bare go build in a git checkout: (devel) + vcs stamps
		{"devel build", &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, true, "dev"},
		{"empty version", &debug.BuildInfo{Main: debug.Module{Version: ""}}, true, "dev"},
		// ReadBuildInfo failure
		{"no build info", nil, false, "dev"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := deriveVersion(c.bi, c.ok)
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
			// Every derivation is either the shape or the dev fallback.
			if got != "dev" && !versionShapeRe.MatchString(got) {
				t.Errorf("derived %q matches neither the version shape nor dev", got)
			}
		})
	}
}