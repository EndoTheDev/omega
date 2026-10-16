package main

import (
	"runtime/debug"
	"testing"
)

// versionFromInfo extracts the module version from a BuildInfo the way
// resolveVersionFromBuildInfo does - kept as a pure helper for the test.
func versionFromInfo(bi *debug.BuildInfo, ok bool) string {
	if !ok {
		return "dev"
	}
	if v := bi.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	return "dev"
}

func TestVersionFromInfo(t *testing.T) {
	cases := []struct {
		name string
		bi   *debug.BuildInfo
		ok   bool
		want string
	}{
		// go install path: Go stamps the module tag into module builds
		{"module stamp", &debug.BuildInfo{Main: debug.Module{Version: "v0.6.1"}}, true, "v0.6.1"},
		// bare go build in a git checkout: (devel) + vcs stamps
		{"devel build", &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, true, "dev"},
		{"empty version", &debug.BuildInfo{Main: debug.Module{Version: ""}}, true, "dev"},
		// ReadBuildInfo failure
		{"no build info", nil, false, "dev"},
	}
	for _, c := range cases {
		if got := versionFromInfo(c.bi, c.ok); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}