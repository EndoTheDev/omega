package continuous

// plugin.go: the capsule adapter. MountWithAPI registers every face:
// the config section (continuous:), the evidence-ledger spine
// subscription, and the module REST routes (the v1 cross-home read
// allow-list: roster + canonical chat, ruling 5, plus the create and
// wake POSTs and the evidence read ruling 11 carries). The capsule is
// toggleable (ruling 14): toggled off, schedules stay and wakes go
// inert - cron still fires ticks into an empty spine.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/engine/agent"
	"github.com/EndoTheDev/omega/modules/datadir"
)

// Plugin adapts the continuous machinery into omega's plugin system.
// It provides no exclusive seam - its faces are the routes, the spine
// subscription and the config section (additive by nature).
type Plugin struct {
	home    string   // the ROOT omega home; "" = datadir.Home() fallback
	cfg     Config   // the mounted config section
	machine *Machine // nil until MountWithAPI
}

// Compile-time assertion that *Plugin implements agent.Plugin.
var _ agent.Plugin = (*Plugin)(nil)

// NewPlugin creates the plugin. Mount resolves the home.
func NewPlugin() *Plugin { return &Plugin{} }

// NewPluginWithHome creates the plugin against an explicit root home
// (tests, embedders, and the cmd/omega host that owns exe-dir
// resolution).
func NewPluginWithHome(home string) *Plugin { return &Plugin{home: home} }

// Name returns the plugin name shown in the /extensions listing.
func (p *Plugin) Name() string { return "continuous" }

// Description implements agent.Describer.
func (p *Plugin) Description() string {
	return "continuous: agent homes, wake subprocesses, canonical chats, the evidence ledger"
}

// ConfigKey names the config section this plugin reads from ctx.Configs.
func (p *Plugin) ConfigKey() string { return "continuous" }

// Provides lists the seams this plugin mounts: none exclusive.
func (p *Plugin) Provides() []string { return nil }

// Requires: store. The create flow opens an agent store to seed the
// canonical chat; nothing else this capsule does needs a seam.
func (p *Plugin) Requires() []string { return []string{"store"} }

// MountWithAPI wires the capsule: config, home, the ledger spine
// subscription, and the REST routes.
func (p *Plugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	cfg := Default()
	if c, ok := ctx.Configs["continuous"].(Config); ok {
		cfg = c
	}
	p.cfg = cfg
	// The ROOT home, never the agent layer's: even a wake child
	// receives the root via OMEGA_HOME (the spawn contract), and the
	// roster reads agents/ relative to it. The env fallback mirrors
	// datadir.Home; cmd/omega passes the exe-aware answer explicitly
	// (NewPluginWithHome) because that resolution is host business.
	home := p.home
	if home == "" {
		home = datadir.Home()
	}
	if home == "" {
		return fmt.Errorf("continuous: no home resolvable (set OMEGA_HOME)")
	}
	machine := NewMachine(home, cfg)
	p.machine = machine
	// The evidence ledger (ruling 11): runtime-owned, spine-fed, and
	// NEVER tool-writable - it subscribes through AddEventObserver, a
	// surface no tool registry reaches. Per-agent stores fall out of
	// the data isolation: this process's rows land in the data dir
	// datadir.AgentHome names (the wake child carries OMEGA_AGENT).
	// AgentHome answers a HOME; the ledger is a data-dir consumer.
	ledgerData, err := datadir.AgentHome()
	if err != nil {
		return fmt.Errorf("continuous: ledger home: %w", err)
	}
	ledger, err := OpenLedger(datadir.Dir(ledgerData))
	if err != nil {
		return fmt.Errorf("continuous: ledger: %w", err)
	}
	ctx.AddEventObserver(ledger.observer())
	ctx.AddEventObserver(machine.tickObserver())

	for _, row := range p.routes() {
		if err := api.RegisterRoute(row.pattern, row.handler); err != nil {
			ledger.Close()
			return err
		}
	}
	return nil
}

// Toggleable marks this capsule as a module capsule (v0.12 E1, the
// tier law): the omega.json disable list may name it. Off = schedules
// stay, agent wakes go inert (ruling 14).
func (p *Plugin) Toggleable() bool { return true }

// SetDisabledAgents swaps the disabled.agents soft-toggle list live
// (v0.14.1 registry-browser: the host's registry-toggle seam and hot
// config reload route here). Nil before Mount is recorded - the mount
// reads it first, but a toggle that early never happens (the seam only
// exists on a wired channel).
func (p *Plugin) SetDisabledAgents(names []string) {
	if p.machine != nil {
		p.machine.SetDisabledAgents(names)
	}
}

// routeRow is one REST registration: natural wire shape, tenant-checked
// at the registry.
type routeRow struct {
	pattern string
	handler func(http.ResponseWriter, *http.Request)
}

// routes builds the capsule's REST face. The v1 cross-home reads are
// the roster, the canonical chat view and the ledger rows (rulings 5
// and 11); the writes are the create flow, the human wake and the
// sleep button - nothing else crosses a home boundary.
func (p *Plugin) routes() []routeRow {
	m := p.machine
	return []routeRow{
		{"GET /continuous/roster", rosterHandler(m)},
		{"POST /continuous", createHandler(m)},
		{"GET /continuous/{name}/chat", chatHandler(m)},
		{"POST /continuous/{name}/chat", wakeHandler(m)},
		{"GET /continuous/{name}/evidence", evidenceHandler(m)},
		{"POST /continuous/{name}/sleep", sleepHandler(m)},
	}
}

// rosterHandler answers the webui roster pane (ruling 18).
func rosterHandler(m *Machine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, m.Roster())
	}
}

// createHandler runs the create flow: name + directive + schedule ->
// the home folder + canonical chat (ruling 18).
func createHandler(m *Machine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name      string `json:"name"`
			Directive string `json:"directive"`
			Schedule  string `json:"schedule"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			httpError(w, http.StatusBadRequest, "create: decode: %v", err)
			return
		}
		if err := m.CreateHome(body.Name, body.Directive, body.Schedule); err != nil {
			httpError(w, http.StatusBadRequest, "create: %v", err)
			return
		}
		writeJSON(w, map[string]string{"created": body.Name})
	}
}

// chatHandler answers the canonical chat view (ruling 5), marking the
// session read - the roster's unread flag clears when the human opens
// the chat.
func chatHandler(m *Machine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		view, err := m.CanonicalChat(r.PathValue("name"))
		if err != nil {
			httpError(w, http.StatusNotFound, "%v", err)
			return
		}
		writeJSON(w, view)
	}
}

// wakeHandler is the human typing into the canonical chat (ruling 8):
// the message wakes the agent; the child persists it as the input and
// lands its report in the session after.
func wakeHandler(m *Machine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		var body struct {
			Message string `json:"message"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			httpError(w, http.StatusBadRequest, "wake: decode: %v", err)
			return
		}
		if strings.TrimSpace(body.Message) == "" {
			body.Message = "" // a bare wake rides the standing prompt
		}
		if err := m.Wake(name, body.Message); err != nil {
			if err == ErrAwake {
				httpError(w, http.StatusConflict, "%v", err)
				return
			}
			httpError(w, http.StatusBadRequest, "%v", err)
			return
		}
		writeJSON(w, map[string]string{"status": "awake"})
	}
}

// evidenceHandler answers the detail strip's evidence rows (ruling 11).
func evidenceHandler(m *Machine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := m.Evidence(r.PathValue("name"))
		if err != nil {
			httpError(w, http.StatusBadRequest, "%v", err)
			return
		}
		writeJSON(w, rows)
	}
}

// sleepHandler is the roster's sleep button: nothing to sleep when no
// wake runs (ruling 7 - asleep between wakes is the default state).
func sleepHandler(m *Machine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m.Sleep(r.PathValue("name"))
		writeJSON(w, map[string]string{"status": "asleep"})
	}
}

// writeJSON renders one 200.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	_ = enc.Encode(v)
}

// httpError renders one failure; detail is operator-facing truth, the
// webui renders what it gets.
func httpError(w http.ResponseWriter, code int, format string, args ...any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf(format, args...)})
}
