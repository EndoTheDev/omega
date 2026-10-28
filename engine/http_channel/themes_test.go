package http_channel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeTheme drops a theme file into a temp dir and returns its path.
func writeTheme(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const validThemeYaml = `name: Midnight Palm
base: light
tokens:
  accent: "#2f9e6e"
  z0: "#001208"
  z1-raised: "#0a1a12"
`

func TestLoadThemesValid(t *testing.T) {
	dir := t.TempDir()
	writeTheme(t, dir, "palm.yaml", validThemeYaml)
	got := loadThemes(dir)
	if len(got) != 1 {
		t.Fatalf("loadThemes: got %d themes, want 1", len(got))
	}
	th := got[0]
	if th.ID != "palm" || th.Name != "Midnight Palm" || th.Base != "light" || th.Error != "" {
		t.Fatalf("loadThemes header wrong: %+v", th)
	}
	if th.Tokens["accent"] != "#2f9e6e" || th.Tokens["z1-raised"] != "#0a1a12" {
		t.Fatalf("tokens wrong: %v", th.Tokens)
	}
}

func TestLoadThemesMissingDir(t *testing.T) {
	got := loadThemes(filepath.Join(t.TempDir(), "nothing-here"))
	if got != nil {
		t.Fatalf("missing dir = %v, want nil", got)
	}
}

func TestLoadThemesBrokenFiles(t *testing.T) {
	cases := []struct {
		name    string
		file    string
		wantSub string
	}{
		{"bad yaml", "broken.yaml", "invalid yaml"},
		{"unknown token", "unknown.yaml", `unknown token "mood"`},
		{"non-string token", "nonstring.yaml", "must be a string value"},
		{"bad base", "badbase.yaml", `base "sepia" is not light or dark`},
		{"no tokens", "empty.yaml", "no tokens"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			switch tc.file {
			case "broken.yaml":
				writeTheme(t, dir, tc.file, "name: [oops\n  this is not: yaml")
			case "unknown.yaml":
				writeTheme(t, dir, tc.file, "name: X\ntokens:\n  mood: happy\n")
			case "nonstring.yaml":
				writeTheme(t, dir, tc.file, "name: X\ntokens:\n  accent: 42\n")
			case "badbase.yaml":
				writeTheme(t, dir, tc.file, "name: X\nbase: sepia\ntokens:\n  accent: red\n")
			case "empty.yaml":
				writeTheme(t, dir, tc.file, "name: Hollow\n")
			}
			got := loadThemes(dir)
			if len(got) != 1 {
				t.Fatalf("got %d themes, want 1", len(got))
			}
			if got[0].Error == "" || !strings.Contains(got[0].Error, tc.wantSub) {
				t.Fatalf("error %q, want substring %q", got[0].Error, tc.wantSub)
			}
			// a broken theme never carries tokens
			if len(got[0].Tokens) != 0 {
				t.Fatalf("broken theme carries tokens: %v", got[0].Tokens)
			}
		})
	}
}

func TestLoadThemesIgnoresStraysAndSorts(t *testing.T) {
	dir := t.TempDir()
	writeTheme(t, dir, "zz-last.yaml", "name: Z\ntokens:\n  accent: red\n")
	writeTheme(t, dir, "aa-first.yml", "name: A\ntokens:\n  accent: blue\n")
	writeTheme(t, dir, "readme.txt", "not a theme")
	writeTheme(t, dir, "noext", "also not a theme")
	sub := filepath.Join(dir, "folder.yaml")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	got := loadThemes(dir)
	if len(got) != 2 {
		t.Fatalf("got %d themes %+v, want 2", len(got), got)
	}
	if got[0].Name != "A" || got[1].Name != "Z" {
		t.Fatalf("not sorted by id: %v / %v", got[0].ID, got[1].ID)
	}
}

func TestLoadThemesDashPrefixToken(t *testing.T) {
	dir := t.TempDir()
	// someone pastes the css name as-is: the -- prefix is tolerated
	writeTheme(t, dir, "t.yaml", "name: T\ntokens:\n  --accent: red\n")
	got := loadThemes(dir)
	if got[0].Error != "" {
		t.Fatalf("unexpected error: %s", got[0].Error)
	}
	if got[0].Tokens["accent"] != "red" {
		t.Fatalf("token not normalized: %v", got[0].Tokens)
	}
}

func TestThemeRegistryWatchPicksUpChanges(t *testing.T) {
	dir := t.TempDir()
	reg := newThemeRegistry(dir)
	t.Cleanup(reg.Stop)
	reg.interval = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go reg.watch(ctx)

	if n := len(reg.snapshot()); n != 0 {
		t.Fatalf("fresh dir has %d themes, want 0", n)
	}
	// (a) drop a file -> appears without restart
	writeTheme(t, dir, "palm.yaml", validThemeYaml)
	themeWaitFor(t, func() bool { return len(reg.snapshot()) == 1 })

	// (c) edit -> content refreshes without restart
	writeTheme(t, dir, "palm.yaml", "name: Edited\ntokens:\n  accent: \"#123456\"\n")
	themeWaitFor(t, func() bool {
		s := reg.snapshot()
		return len(s) == 1 && s[0].Name == "Edited"
	})

	// (d) a file that now fails validation: previous GOOD set stays in
	// effect AND the broken file is flagged, not dropped or crashy
	writeTheme(t, dir, "junk.yaml", "tokens:\n  nonsense: value\n")
	themeWaitFor(t, func() bool {
		s := reg.snapshot()
		return len(s) == 2 && (s[1].Error != "" || s[0].Error != "")
	})
	s := reg.snapshot()
	good := 0
	broken := 0
	for _, th := range s {
		if th.Error == "" {
			good++
			if th.ID == "junk" {
				t.Fatal("junk theme marked good")
			}
		} else {
			broken++
			if th.ID != "junk" {
				t.Fatalf("wrong file flagged: %s (%s)", th.ID, th.Error)
			}
		}
	}
	if good != 1 || broken != 1 {
		t.Fatalf("good=%d broken=%d, want 1/1", good, broken)
	}

	// delete -> gone without restart
	if err := os.Remove(filepath.Join(dir, "junk.yaml")); err != nil {
		t.Fatal(err)
	}
	themeWaitFor(t, func() bool { return len(reg.snapshot()) == 1 })
}

func TestThemeRegistryUnparseableKeepsPrevious(t *testing.T) {
	dir := t.TempDir()
	writeTheme(t, dir, "palm.yaml", validThemeYaml)
	reg := newThemeRegistry(dir)
	t.Cleanup(reg.Stop)
	reg.interval = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go reg.watch(ctx)

	if n := len(reg.snapshot()); n != 1 {
		t.Fatalf("startup load has %d themes, want 1", n)
	}
	writeTheme(t, dir, "palm.yaml", "name: [not: closed\n  yaml: {{{")
	// the broken edit keeps the previous good set in effect
	themeWaitFor(t, func() bool {
		s := reg.snapshot()
		return len(s) == 1 && s[0].Error != "" // flagged entry replaces it
	})
	s := reg.snapshot()
	if s[0].ID != "palm" || s[0].Tokens != nil {
		t.Fatalf("broken reload leaks tokens: %+v", s[0])
	}
}

func themeWaitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition never held")
}

func TestHandleThemes(t *testing.T) {
	dir := t.TempDir()
	writeTheme(t, dir, "palm.yaml", validThemeYaml)
	writeTheme(t, dir, "junk.yaml", "tokens:\n  nonsense: value\n")
	srv := NewServer(nil, nil, nil, nil, nil, nil)
	srv.themes = newThemeRegistry(dir)

	// GET: list + content, including the flagged broken theme
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/themes", nil))
	if rec.Code != 200 {
		t.Fatalf("GET /themes -> %d", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control %q, want no-store", got)
	}
	var body struct {
		Themes []Theme `json:"themes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Themes) != 2 {
		t.Fatalf("payload has %d themes, want 2", len(body.Themes))
	}
	var palm Theme
	for _, th := range body.Themes {
		if th.ID == "palm" {
			palm = th
		} else if th.Error == "" {
			t.Fatalf("theme %s served without its error flag", th.ID)
		}
	}
	if palm.Tokens["accent"] != "#2f9e6e" {
		t.Fatalf("palm tokens missing: %+v", palm)
	}

	// POST: method not allowed, like every other read endpoint here
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/themes", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /themes -> %d, want 405", rec.Code)
	}
}

func TestResolveThemesDir(t *testing.T) {
	t.Setenv("OMEGA_THEMES_DIR", "")
	t.Setenv("OMEGA_HOME", "")
	if d := resolveThemesDir(); d != "" {
		t.Fatalf("no home -> %q, want empty", d)
	}
	t.Setenv("OMEGA_HOME", "/home/omega")
	if d := resolveThemesDir(); d != filepath.Join("/home/omega", themesDirName) {
		t.Fatalf("home-only -> %q", d)
	}
	t.Setenv("OMEGA_THEMES_DIR", "/elsewhere")
	if d := resolveThemesDir(); d != "/elsewhere" {
		t.Fatalf("override ignored: %q", d)
	}
}
