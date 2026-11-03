package browser

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/harness/datadir"
)

// Plugin adapts the browser capsule into omega's plugin system. It
// claims no seam: the browser pane is webui vocabulary (the dock tab,
// the shell registry in webui), and the module's whole surface
// is the persistence route pair the tab drives. Toggleable like every
// capsule - disabling it dies the routes (the live Context re-read),
// the tab keeps working with an empty resume point.
type Plugin struct {
	store *Store
}

var _ agent.Plugin = (*Plugin)(nil)

// NewPlugin returns a browser Plugin instance.
func NewPlugin() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string { return "browser" }

// Description implements agent.Describer.
func (p *Plugin) Description() string {
	return "browser: the general web browser pane (URL bar + iframe, any origin; last-URL memory)"
}

// ConfigKey names the config section this plugin reads from
// ctx.Configs (see agent.Describer). Empty = reads no section.
func (p *Plugin) ConfigKey() string { return "" }

// Provides lists the seams this plugin mounts: none - the tab is
// shell vocabulary, the routes are the surface.
func (p *Plugin) Provides() []string { return nil }

// Requires lists seams that must be mounted before this plugin.
func (p *Plugin) Requires() []string { return nil }

// Toggleable marks the browser capsule (the config.yaml disabled:
// list may name it).
func (p *Plugin) Toggleable() bool { return true }

// MountWithAPI implements agent.APIPlugin. The store resolves through
// the home (datadir, the notifications pattern); a home that cannot
// resolve builds the memory-only store and the routes still mount -
// an exotic embed answers 200 with an empty resume point, it never
// 404s the pane's first read.
func (p *Plugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	var path string
	home, aerr := datadir.AgentHome()
	if aerr != nil {
		home = ""
	}
	if home != "" {
		path = datadir.DBPath(home, StoreName)
	}
	s, err := OpenStore(path)
	if err != nil {
		// The store is the cheapest loss in the home: boot with the
		// memory-only shape instead of failing the host's boot over it.
		fmt.Printf("omega browser: %v (continuing without persistence)\n", err)
		s = &Store{path: ""}
	}
	p.store = s
	if err := api.RegisterRoute("GET /browser/last", p.handleGet); err != nil {
		return err
	}
	if err := api.RegisterRoute("PUT /browser/last", p.handlePut); err != nil {
		return err
	}
	return api.RegisterRoute("GET /browser/frameable", p.handleFrameable)
}

// handleGet serves GET /browser/last: the resume URL ("" when none).
func (p *Plugin) handleGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"last": p.store.Last()})
}

// handlePut serves PUT /browser/last: {"url": "..."} - the tab
// persists after a successful embed load; {"url": ""} RESETS the
// resume point (Clear, 200 with an empty last). A bad body or URL is a
// 400 (the pane keeps browsing either way).
func (p *Plugin) handlePut(w http.ResponseWriter, r *http.Request) {
	b, err := io.ReadAll(io.LimitReader(r.Body, 4096))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, failure("read body"))
		return
	}
	var body struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(b, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, failure("bad json"))
		return
	}
	if body.URL == "" {
		p.store.Clear()
		writeJSON(w, http.StatusOK, map[string]string{"last": ""})
		return
	}
	if !ValidURL(body.URL) {
		writeJSON(w, http.StatusBadRequest, failure("not a browsable URL"))
		return
	}
	if err := p.store.SetLast(body.URL); err != nil {
		writeJSON(w, http.StatusInternalServerError, failure("persist"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"last": p.store.Last()})
}

// handleFrameable serves GET /browser/frameable: the frameability
// probe (url required, origin optional - the SPA's own origin) over the
// URL the pane is about to embed. A probe that cannot answer is a 200
// with frameable null - the tab keeps the iframe path.
func (p *Plugin) handleFrameable(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("url")
	if !ValidURL(raw) {
		writeJSON(w, http.StatusBadRequest, failure("not a browsable URL"))
		return
	}
	writeJSON(w, http.StatusOK, Probe(raw, r.URL.Query().Get("origin")))
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func failure(msg string) map[string]string {
	return map[string]string{"error": msg}
}