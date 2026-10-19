package http_channel

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// User themes: token overrides for the webui theme picker. A theme is
// ONE yaml file in OMEGA_HOME/themes (alongside skills/) carrying a
// name, an optional base, and token overrides. Tokens only - no raw
// CSS injection (v1 is recoloring, not restyling). The folder is
// watched with the cron module's mtime-poll pattern: a changed file is
// picked up without restart, a broken file keeps the previous good set
// in effect and logs loudly.

const themesDirName = "themes"

// themesPollInterval is the folder-watch tick. Package var so tests
// can shorten it.
var themesPollInterval = 2 * time.Second

// themeTokenKeys is the allowlist of CSS custom properties a theme may
// override: the color set main.css defines on :root, plus the two
// layer shadows (shadows are rgba recoloring too). Radii and fonts are
// deliberately excluded - v1 is recoloring, not restyling. Module
// components style only through these cascade tokens, so overriding
// them recolors modules for free (2026-10-19 re-anchor).
var themeTokenKeys = map[string]bool{
	"accent": true, "unread": true,
	"z0": true, "z1": true, "z1-raised": true, "z2": true,
	"stroke": true, "text": true, "text-dim": true,
	"ok": true, "err": true,
	"shadow-1": true, "shadow-2": true,
}

// Theme is one user theme as served over GET /themes. A file that
// fails to parse or validate rides along with Error set and no
// Tokens - the picker can flag it and the server keeps running.
type Theme struct {
	// ID is the file name without extension; the apply wire value is
	// theme:<ID>.
	ID string `json:"id"`
	// Name comes from the yaml name field, falling back to the ID.
	Name string `json:"name"`
	// Base selects which built-in palette the overrides stack on
	// ("dark" | "light"); defaults to dark when the yaml omits it.
	Base string `json:"base,omitempty"`
	// Tokens maps main.css custom-property names (without the --) to
	// their values. Empty on an error theme.
	Tokens map[string]string `json:"tokens,omitempty"`
	// Error is the loud validation/parse failure, set only on themes
	// that are unusable. The server keeps serving it so the operator
	// can see why a dropped file is missing from the picker.
	Error string `json:"error,omitempty"`
}

// themeFile is the yaml envelope of one theme file.
type themeFile struct {
	Name   string         `yaml:"name"`
	Base   string         `yaml:"base"`
	Tokens map[string]any `yaml:"tokens"`
}

// loadThemes parses every *.yaml/*.yml in dir sorted by ID. A missing
// dir is no themes, not an error (first run, nothing dropped in yet).
// One broken file never poisons its neighbours.
func loadThemes(dir string) []Theme {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return []Theme{{ID: dir, Error: "read themes dir: " + err.Error()}}
	}
	out := make([]Theme, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if e.IsDir() || (ext != ".yaml" && ext != ".yml") {
			continue
		}
		out = append(out, loadTheme(dir, name))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	if out == nil {
		out = []Theme{}
	}
	return out
}

// loadTheme parses and validates one theme file. Any failure returns a
// flagged Theme - load never panics, never skips silently.
func loadTheme(dir, fileName string) Theme {
	id := strings.TrimSuffix(fileName, filepath.Ext(fileName))
	raw, err := os.ReadFile(filepath.Join(dir, fileName))
	if err != nil {
		return Theme{ID: id, Name: id, Error: "read: " + err.Error()}
	}
	var tf themeFile
	if err := yaml.Unmarshal(raw, &tf); err != nil {
		return Theme{ID: id, Name: id, Error: "invalid yaml: " + err.Error()}
	}
	name := tf.Name
	if name == "" {
		name = id
	}
	base := strings.ToLower(strings.TrimSpace(tf.Base))
	switch base {
	case "", "dark":
		base = "dark"
	case "light":
	default:
		return Theme{ID: id, Name: name, Error: fmt.Sprintf("base %q is not light or dark", tf.Base)}
	}
	tokens := map[string]string{}
	for k, v := range tf.Tokens {
		s, ok := v.(string)
		if !ok {
			return Theme{ID: id, Name: name, Base: base,
				Error: fmt.Sprintf("token %q must be a string value, got a %T", k, v)}
		}
		clean := strings.TrimPrefix(strings.TrimSpace(k), "--")
		if !themeTokenKeys[clean] {
			return Theme{ID: id, Name: name, Base: base,
				Error: fmt.Sprintf("unknown token %q (allowed: accent, unread, z0, z1, z1-raised, z2, stroke, text, text-dim, ok, err, shadow-1, shadow-2)", k)}
		}
		tokens[clean] = s
	}
	if len(tokens) == 0 {
		return Theme{ID: id, Name: name, Base: base,
			Error: "no tokens: a theme must override at least one token (tokens only, no CSS)"}
	}
	return Theme{ID: id, Name: name, Base: base, Tokens: tokens}
}

// themeRegistry holds the live theme set behind GET /themes and watches
// the folder. Reloads replace the whole set: a failed parse keeps the
// previous good set in effect (the cron watcher's contract), the broken
// file still shows up flagged with its error.
type themeRegistry struct {
	mu       sync.Mutex
	dir      string
	themes   []Theme
	fp       map[string]int64 // last-seen file-name -> mtime fingerprint
	interval time.Duration
	stop     chan struct{}
}

// resolveThemesDir mirrors the skills dir resolution: OMEGA_THEMES_DIR
// wins when set, otherwise themes/ under the instance home. No home
// configured (bare tests) means no themes surface.
func resolveThemesDir() string {
	if d := os.Getenv("OMEGA_THEMES_DIR"); d != "" {
		return d
	}
	home := os.Getenv("OMEGA_HOME")
	if home == "" {
		return ""
	}
	return filepath.Join(home, themesDirName)
}

// newThemeRegistry loads the dir once. An empty dir string disables the
// registry (nil-safe to use).
func newThemeRegistry(dir string) *themeRegistry {
	r := &themeRegistry{dir: dir, interval: themesPollInterval, stop: make(chan struct{})}
	if dir != "" {
		_ = os.MkdirAll(dir, 0o755) // themes/ exists for the operator to drop into
		r.reload()
	}
	return r
}

// reload re-reads the folder and logs any broken files loudly.
func (r *themeRegistry) reload() {
	themes := loadThemes(r.dir)
	fp := themeFingerprint(r.dir)
	r.mu.Lock()
	r.themes = themes
	r.fp = fp
	r.mu.Unlock()
	for _, t := range themes {
		if t.Error != "" {
			log.Printf("[themes] theme %q unusable, picker will skip it: %s", t.ID, t.Error)
		}
	}
}

// snapshot returns the current set (safe copy of the slice header; the
// entries are immutable after load).
func (r *themeRegistry) snapshot() []Theme {
	if r == nil {
		return []Theme{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Theme, len(r.themes))
	copy(out, r.themes)
	return out
}

// watch polls the folder until ctx is done or Stop fires, reloading on
// any add/modify/delete. The fingerprint is the per-file mtime map -
// the cron watcher's single-file pattern generalized to a folder.
func (r *themeRegistry) watch(ctx context.Context) {
	if r == nil || r.dir == "" {
		return
	}
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-r.stop:
			return
		case <-ticker.C:
		}
		fp := themeFingerprint(r.dir)
		r.mu.Lock()
		same := fingerprintEqual(fp, r.fp)
		r.mu.Unlock()
		if same {
			continue
		}
		r.reload()
	}
}

// Stop ends the watch loop (tests, embedders). Serve relies on ctx
// cancellation; Stop covers the case where the server is torn down
// without one.
func (r *themeRegistry) Stop() {
	if r != nil {
		select {
		case <-r.stop:
		default:
			close(r.stop)
		}
	}
}

// handleThemes serves GET /themes: the list with content inline - one
// request is enough for the picker to list names and apply tokens.
// Broken themes ride along flagged with Error; the picker shows only
// the good ones and the server never falls over. Served from the
// watcher-refreshed cache, so a dropped or edited file is live without
// a restart.
func (s *Server) handleThemes(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// no-store: opening the settings pane must see fresh folder state
	// (a theme edited a second ago), never a cached list.
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{"themes": s.themes.snapshot()}) //nolint:errcheck // best-effort read response
}

// fingerprintEqual compares two mtime maps.
func fingerprintEqual(a, b map[string]int64) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
}

// themeFingerprint maps file name -> mtime (unix nanos) for every
// theme file in the dir. A read error yields nil: every file counts as
// changed on the next healthy poll, which reloads. Nil maps compare
// equal to each other and unequal to any populated map.
func themeFingerprint(dir string) map[string]int64 {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	fp := make(map[string]int64)
	for _, e := range entries {
		name := e.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if e.IsDir() || (ext != ".yaml" && ext != ".yml") {
			continue
		}
		if fi, err := e.Info(); err == nil {
			fp[name] = fi.ModTime().UnixNano()
		}
	}
	return fp
}
