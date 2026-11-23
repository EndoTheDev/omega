package api

import (
	"context"
	_ "embed" // the example theme rides in the binary (go:embed string)
	"encoding/json"
	"fmt"
	"log"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// User themes: token overrides for the webui theme picker.

const themesDirName = "themes"

// defaultThemeFileName is the example theme seeded into a fresh themes folder. The
// embedded copy is written once - an existing file is an operator's edit and is never
// overwritten.
const defaultThemeFileName = "default.yaml"

// defaultThemeSeed is the example theme file shipped with the binary.
//
//go:embed themes/default.yaml
var defaultThemeSeed string

// seedDefaultTheme copies the embedded example into dir once. Errors log loudly and
// never block startup.
func seedDefaultTheme(dir string) {
	if dir == "" {
		return
	}
	dst := filepath.Join(dir, defaultThemeFileName)
	if _, err := os.Stat(dst); err == nil {
		return
	} else if !os.IsNotExist(err) {
		log.Printf("[themes] seed stat %s: %s", dst, err)
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("[themes] seed mkdir %s: %s", dir, err)
		return
	}
	if err := os.WriteFile(dst, []byte(defaultThemeSeed), 0o644); err != nil {
		log.Printf("[themes] seed write %s: %s", dst, err)
		return
	}
	log.Printf("[themes] seeded example theme %s (copy + rename a theme file to make your own)", dst)
}

// themesPollInterval is the folder-watch tick.
var themesPollInterval = 2 * time.Second

// themeTokenKeys is the allowlist of CSS custom properties a theme may override.
var themeTokenKeys = map[string]bool{
	"accent": true, "unread": true,
	"z0": true, "z1": true, "z1-raised": true, "z2": true,
	"stroke": true, "text": true, "text-dim": true,
	"ok": true, "err": true,
	"shadow-1": true, "shadow-2": true,
}

// Theme is one user theme as served over GET /themes.
type Theme struct {
	ID string `json:"id"`
	Name string `json:"name"`
	Base string `json:"base,omitempty"`
	Tokens map[string]string `json:"tokens,omitempty"`
	Error string `json:"error,omitempty"`
}

// themeFile is the yaml envelope of one theme file.
type themeFile struct {
	Name   string         `yaml:"name"`
	Base   string         `yaml:"base"`
	Tokens map[string]any `yaml:"tokens"`
}

// loadThemes parses every *.yaml/*.yml in dir sorted by ID. One broken file never
// poisons its neighbours.
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

// themeRegistry holds the live theme set behind GET /themes and watches the folder.
// Reloads replace the whole set: a failed parse keeps the previous good set in effect
// (the cron watcher's contract), the broken file still shows up flagged with its error.
type themeRegistry struct {
	mu       sync.Mutex
	dir      string
	themes   []Theme
	fp       map[string]int64 // last-seen file-name -> mtime fingerprint
	interval time.Duration
	stop     chan struct{}
}

// resolveThemesDir mirrors the skills dir resolution.
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
		seedDefaultTheme(dir)
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

// snapshot returns the current set.
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

// watch polls the folder until ctx is done or Stop fires, reloading on any.
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

// Stop ends the watch loop (tests, embedders).
func (r *themeRegistry) Stop() {
	if r != nil {
		select {
		case <-r.stop:
		default:
			close(r.stop)
		}
	}
}

// handleThemes serves GET /themes. Broken themes ride along flagged with Error; the
// picker shows only the good ones and the server never falls.
func (s *Server) handleThemes(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// no-store: opening the settings pane must see fresh folder state
	// (a theme edited a second ago), never a cached list.
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{"themes": s.themes.snapshot()})
}

// fingerprintEqual compares two mtime maps.
func fingerprintEqual(a, b map[string]int64) bool {
	return maps.Equal(a, b)
}

// themeFingerprint maps file name -> mtime (unix nanos) for every theme file in the
// dir. A read error yields nil: every file counts as changed on the next healthy poll,
// which reloads. Nil maps compare equal to each other and unequal to any populated map.
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
