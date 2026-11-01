// Package browser is the capsule behind the webui's general web
// browser pane (card t_95c91f9c, Endo ruling 2026-11-01: "the browser
// should be like a normal browser which also allows me to visit other
// websites"). The pane itself is pure client side - a URL bar and an
// iframe embed, any origin, no allowlist, no origin gate; sites that
// refuse embedding (X-Frame-Options) get the SPA's fallback panel.
// Never a proxy: the module owns exactly one honest server surface -
// the last-browsed URL, persisted so the tab restores where the user
// left off across page reloads.
//
// No seam, no tools, no commands: the browser is frontend vocabulary
// (the dock tab) plus this one persistence route. Removal proof is
// plain deletion - remove modules/browser/ and the SPA's
// components/browser/, drop the registry + mount lines (the card body
// carries the recipe) and the feature is gone from source and binary.
package browser

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// State is the on-disk shape: <home>/data/browser.json. One field on
// purpose - v1 stores nothing but the resume point; no history, no
// bookmarks (later only if asked).
type State struct {
	Last string `json:"last"`
}

// Store is the last-URL persistence. In-memory when built without a
// path (exotic embeds with no resolvable home): every route still
// answers, persistence is a no-op - the route surface stays stable.
type Store struct {
	mu   sync.Mutex
	path string
	last string
}

// StoreName is the fixed file name inside the home's data directory.
const StoreName = "browser.json"

// OpenStore loads (or resets) the store at path. A path of "" builds
// the memory-only shape. A missing file is a zero state, never an
// error; a corrupt file is a zero state too (a browser memory file is
// the cheapest loss in the home).
func OpenStore(path string) (*Store, error) {
	s := &Store{path: path}
	if path == "" {
		return s, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, fmt.Errorf("browser store: %w", err)
	}
	var st State
	if err := json.Unmarshal(b, &st); err != nil || st.Last == "" {
		return s, nil // zero state, same as missing
	}
	if !ValidURL(st.Last) {
		return s, nil // a stale/wrong-typed file carries no URL
	}
	s.last = st.Last
	return s, nil
}

// Last returns the persisted resume URL ("" when none).
func (s *Store) Last() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last
}

// SetLast records the URL and persists it. Unreachable only when the
// path is empty (memory-only) or the home vanished mid-run.
func (s *Store) SetLast(u string) error {
	if !ValidURL(u) {
		return fmt.Errorf("browser: not a browsable URL: %q", u)
	}
	s.mu.Lock()
	s.last = u
	path := s.path
	s.mu.Unlock()
	if path == "" {
		return nil
	}
	b, err := json.MarshalIndent(State{Last: u}, "", "  ")
	if err != nil {
		return fmt.Errorf("browser: encode state: %w", err)
	}
	return writeFileAtomic(path, append(b, '\n'), 0o644)
}

// Clear zeroes the resume memory and deletes the backing file. The
// REST face's reset ({"url": ""}) and the hygiene paths use it; the
// resume memory is the cheapest loss in the home.
func (s *Store) Clear() {
	s.mu.Lock()
	s.last = ""
	path := s.path
	s.mu.Unlock()
	if path == "" {
		return
	}
	_ = os.Remove(path) // a failed removal is next boot's problem at most
}

// probeClient is the frameability probe's only dialer. Bounded: a 6s
// total timeout and the body DISCARDED after 8KiB - the probe reads
// headers, never content; it is not a proxy. A probed URL is exactly
// what the user asked the pane to visit, so the probe's reach is the
// pane's own.
var probeClient = &http.Client{
	Timeout: 6 * time.Second,
}

// Probe answers the frameability verdict for u as the SPA at spaOrigin
// would see it. A transport failure is the honest null (the tab keeps
// the iframe path) - the probe never guesses.
func Probe(u, spaOrigin string) FrameVerdict {
	if !ValidURL(u) {
		return FrameVerdict{Reason: "not a browsable URL"}
	}
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return FrameVerdict{}
	}
	req.Header.Set("User-Agent", "omega browser pane (frameability probe)")
	resp, err := probeClient.Do(req)
	if err != nil {
		// unreachable: the probe cannot say - null verdict
		return FrameVerdict{}
	}
	defer resp.Body.Close()
	_, _ = io.CopyN(io.Discard, resp.Body, 8192)
	fv, reason := FrameableHeaders(resp.StatusCode, resp.Header.Values("X-Frame-Options"),
		resp.Header.Values("Content-Security-Policy"), spaOrigin)
	if !*fv {
		return FrameVerdict{Frameable: fv, Reason: reason, Status: resp.StatusCode}
	}
	return FrameVerdict{Frameable: fv, Status: resp.StatusCode}
}

// ValidURL accepts exactly what an end user pastes into a browser's
// address bar: an absolute http(s) URL with a host. ftp:, javascript:,
// data:, bare hosts without a scheme - all refused (the SPA's URL
// bar normalizes the https:// prefix before putting the address on
// the wire; the route double-checks at the server, not as a gate -
// there is no origin allowlist, only shape validation).
func ValidURL(u string) bool {
	if u == "" {
		return false
	}
	p, err := url.Parse(u)
	if err != nil {
		return false
	}
	if (p.Scheme != "http" && p.Scheme != "https") || p.Host == "" {
		return false
	}
	// URL.Path with control characters is shell/terminal poison if it
	// ever round-trips into a config write; refuse the exotic range.
	for _, r := range u {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// FrameVerdict is the frameability probe's answer for one URL. The
// boolean is THREE-state on the wire: true/false/null. null (the probed
// host is unreachable / the probe failed) means "the probe cannot say"
// - the tab keeps the iframe path and its late-load window instead of
// trusting a guess.
type FrameVerdict struct {
	Frameable *bool  `json:"frameable"`
	Reason    string `json:"reason,omitempty"`
	Status    int    `json:"status,omitempty"`
}

// FrameableHeaders classifies a probed response's headers from the
// SPA's point of view (the SPA's origin arrives as the query's origin
// parameter - the frame's ancestor IS the SPA, whatever lane port it
// serves on). Verdict:
//
//	X-Frame-Options deny / sameorigin / allow-from -> refuse (the tab
//	  is never the site's own origin).
//	CSP frame-ancestors: * -> allow; a specific list that includes the
//	  SPA's origin -> allow; any other list ('none', 'self', other
//	  hosts) -> refuse.
//	an HTTP error status (4xx/5xx) -> refuse "http-status": the frame
//	  would render the browser's own error surface.
//	otherwise -> allow.
func FrameableHeaders(status int, xfo, csp []string, spaOrigin string) (*bool, string) {
	for _, v := range xfo {
		switch strings.TrimSpace(strings.ToLower(v)) {
		case "deny", "sameorigin", "allow-from":
			return boolPtr(false), "x-frame-options"
		}
	}
	for _, v := range csp {
		for _, directive := range strings.Split(v, ";") {
			d := strings.TrimSpace(directive)
			if !strings.HasPrefix(strings.ToLower(d), "frame-ancestors") {
				continue
			}
			toks := strings.Fields(d[len("frame-ancestors"):])
			if len(toks) == 0 {
				continue
			}
			for _, t := range toks {
				tl := strings.ToLower(strings.Trim(t, `'"`))
				if tl == "*" {
					return boolPtr(true), ""
				}
				if spaOrigin != "" && AncestorHost(tl) == AncestorHost(spaOrigin) {
					return boolPtr(true), ""
				}
			}
			// a frame-ancestors list without * and without the SPA's
			// origin: the embed refuses ('none', 'self', other hosts)
			return boolPtr(false), "csp-frame-ancestors"
		}
	}
	if status >= 400 {
		return boolPtr(false), "http-status"
	}
	return boolPtr(true), ""
}

// AncestorHost reduces an origin to what frame-ancestors host tokens
// compare against ("https://example.com:8098" -> "example.com:8098";
// the default ports stay as written in the ancestors value - the
// comparison is a plain string match, and a missing match refuses,
// which is the safe direction).
func AncestorHost(origin string) string {
	o := strings.TrimPrefix(strings.TrimPrefix(origin, "https://"), "http://")
	o = strings.ToLower(o)
	if i := strings.IndexAny(o, "/?"); i >= 0 {
		o = o[:i]
	}
	return o
}

func boolPtr(b bool) *bool { return &b }

// writeFileAtomic writes via a temp file in the same directory +
// rename (v0.14.1 atomic-write law). The capsule keeps its own copy:
// module law keeps import surfaces minimal and the helper is small.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("browser: create dir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("browser: temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close() //nolint:errcheck // best effort, tmp already removed
		return fmt.Errorf("browser: write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("browser: close temp: %w", err)
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		return fmt.Errorf("browser: chmod temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("browser: rename into place: %w", err)
	}
	return nil
}