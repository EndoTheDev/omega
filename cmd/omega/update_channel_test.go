package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// fixtureReleases serves a releases-list fixture: one GitHub API base
// with /releases/latest (stable-only) and /releases (the full list).
// List order is newest-first, exactly like the real endpoint.
func fixtureReleases(t *testing.T, latest githubRelease, list []githubRelease) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases/latest"):
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(latest)
		case strings.HasSuffix(r.URL.Path, "/releases"):
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(list)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func rel(tag string) githubRelease {
	return githubRelease{TagName: tag, Assets: []githubAsset{
		{Name: "omega_" + tag + "_linux_amd64.tar.gz", BrowserDownloadURL: "https://example.invalid/" + tag + ".tar.gz"},
	}}
}

// TestFetchStableSkipsPrereleaseTags: the stable channel must resolve
// the latest stable tag while nightly (and other prerelease) tags float
// around it — the fixture list of refs carries both, and /releases/latest
// serves only the stable one.
func TestFetchStableSkipsPrereleaseTags(t *testing.T) {
	old := githubAPIBase
	t.Cleanup(func() { githubAPIBase = old })
	githubAPIBase = fixtureReleases(t,
		rel("v0.9.1"),
		[]githubRelease{
			rel("v0.9.2-nightly.20261022"),
			rel("v0.9.2-nightly.20261021"),
			rel("v0.9.1"),
			rel("v0.9.1-nightly.20261020"),
		})

	// The stable resolver hits the stable-only endpoint.
	got, err := fetchStable()
	if err != nil {
		t.Fatalf("fetchStable: %v", err)
	}
	if got.TagName != "v0.9.1" {
		t.Errorf("stable resolved %q, want v0.9.1 — nightlies must be invisible to the stable channel", got.TagName)
	}
}

// TestFetchNightlyPicksNewestPrerelease pins the nightly channel: the
// newest -nightly.* tag by semver, ignoring stable and rc tags and
// list order.
func TestFetchNightlyPicksNewestPrerelease(t *testing.T) {
	old := githubAPIBase
	t.Cleanup(func() { githubAPIBase = old })
	githubAPIBase = fixtureReleases(t, rel("v0.9.1"),
		[]githubRelease{
			rel("v0.9.1"),                   // stable, wrong channel
			rel("v0.9.2-nightly.20261021"),  // older nightly
			rel("v0.9.2-rc.1"),              // prerelease but not nightly channel
			rel("v0.9.10-nightly.20261022"), // newest by numeric core (0.9.10 > 0.9.2)
			rel("v0.9.2-nightly.20261101"),  // newer nightly by date but older core
			rel("v0.9.2-nightly.20261022"),
		})

	got, err := fetchNightly()
	if err != nil {
		t.Fatalf("fetchNightly: %v", err)
	}
	if got.TagName != "v0.9.10-nightly.20261022" {
		t.Errorf("nightly resolved %q, want v0.9.10-nightly.20261022 (numeric core beats later date on a smaller core)", got.TagName)
	}
}

func TestFetchNightlyNoNightlies(t *testing.T) {
	old := githubAPIBase
	t.Cleanup(func() { githubAPIBase = old })
	githubAPIBase = fixtureReleases(t, rel("v0.9.1"), []githubRelease{rel("v0.9.1"), rel("v0.9.2-rc.1")})
	if _, err := fetchNightly(); err == nil {
		t.Error("no nightly tags in the list: fetchNightly must error, not silently install a stable")
	}
}

// TestCompareTagNewer pins that unparseable tags can never win the
// nightly pick over a valid semver tag.
func TestCompareTagNewer(t *testing.T) {
	if compareTagNewer("dev", "v0.9.1") || compareTagNewer("zzz-unknown", "v0.1.0") {
		t.Error("unparseable tags must never sort newer")
	}
	if !compareTagNewer("v0.9.2-nightly.20261022", "v0.9.2-nightly.20261021") {
		t.Error("newer nightly must win")
	}
}

// writeExe creates a fake executable file with the given content.
func writeExe(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}
