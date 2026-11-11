package continuous

// agentsrest.go: the roster-management REST face (the shell-ring
// shrink wave 2, ADR Workshop/omega-shell-ring-shrink.md - the agents
// tab is the continuous capsule's face, "multi-agent IS the continuous
// capsule's design"). Moved whole from agent/api/server_agents.go:
//
//	GET  /agents                     profile inventory (the roster list)
//	POST /agents                     scaffold a new agent directory
//	GET  /agents/{name}              one agent's detail
//	PUT  /agents/{name}              the roster's edit affordance
//	GET  /agents/{name}/sessions     sessions bound to this agent
//	POST /agents/{name}/sessions     create a session AS this agent
//
// THE FLOOR VERDICT (the task's classification rule): no handler here
// serves the chat floor - the chat page never fetches agent profiles
// (the only /agents consumer in the webui is the roster panel), and
// the canonical-chat cross-home reads (GET /continuous/{name}/chat)
// were born on this capsule's own route list. So ALL of the old file
// moved; nothing stayed base. The chat floor the proof checks is a
// single agent running and chatting - which touches none of these
// routes.
//
// The home is the ROOT home the plugin already resolved (NewPluginWithHome
// / datadir fallback) - the roster reads agents/ relative to it, the
// same surface the old env-scan answered. The store resolves per
// request (the store seam's live re-wire law): the session counts and
// the agent-session reads die with a disabled store, the profile
// reads never needed one.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/store"
)

// agentRoutes is the roster REST face, built on the plugin's root home
// and mount context. One value per mount; every handler reads the
// same pair.
type agentRoutes struct {
	home string
	ctx  *core.Context
}

// mountedStore resolves the daemon's store seam per request (the
// store seam's live re-wire law). Nil = no store capsule mounted: the
// roster keeps its zero rows and the session routes answer 501.
func (ar *agentRoutes) mountedStore() agent.StoreProvider {
	st, _ := ar.ctx.Services().Resolve("store").(agent.StoreProvider)
	return st
}

// readAgentSessions reads ONE agent's sessions from the store that
// OWNS them (card t_81109e1c). Per-agent data isolation means main's
// sessions live in the daemon's store and every agent home's sessions
// live in its own data/omega.db - the same file the capsule's
// cross-home reads and the wake child open. Reading the daemon's store
// for an agent home answers zero rows after the boot migration moved
// them, which would strand the roster's counts and the tab's chat
// binding. A home that never ran has no store file: an empty list,
// never a created database.
func (ar *agentRoutes) readAgentSessions(ctx context.Context, name string) ([]core.Session, error) {
	st := ar.mountedStore()
	if st == nil {
		return nil, nil
	}
	if name == agent.DefaultAgentName {
		as, _ := st.(agent.AgentSessionStore)
		if as == nil {
			return nil, nil
		}
		return as.ListAgentSessions(ctx, name)
	}
	path := filepath.Join(agent.AgentDir(ar.home, name), "data", "omega.db")
	if _, err := os.Stat(path); err != nil {
		return nil, nil // never ran: no store, no sessions
	}
	ast, err := store.Open(path)
	if err != nil {
		return nil, err
	}
	defer ast.Close()
	cctx, cancel := context.WithTimeout(ctx, storeTimeout)
	defer cancel()
	return ast.ListAgentSessions(cctx, name)
}

// writeAgentSession creates a session in the store that will OWN it:
// the daemon's store for main, the agent home's own data/omega.db for
// an agent (created on first write - a session needs a home). The
// release func closes the agent store; main's is a no-op.
func (ar *agentRoutes) writeAgentSession(ctx context.Context, name, id string) error {
	st := ar.mountedStore()
	if st == nil {
		return errNoStoreSeam
	}
	if name == agent.DefaultAgentName {
		as, _ := st.(agent.AgentSessionStore)
		if as == nil {
			return errNoStoreSeam
		}
		return as.CreateAgentSession(ctx, id, "", "", name)
	}
	ast, err := store.Open(filepath.Join(agent.AgentDir(ar.home, name), "data", "omega.db"))
	if err != nil {
		return err
	}
	defer ast.Close()
	cctx, cancel := context.WithTimeout(ctx, storeTimeout)
	defer cancel()
	return ast.CreateAgentSession(cctx, id, "", "", name)
}

// errNoStoreSeam is the 501 lane: no store capsule mounted, or one
// without the agent-session surface.
var errNoStoreSeam = errors.New("store does not support agent sessions")

// writeJSON renders one 200-family response.
func (ar *agentRoutes) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// httpError renders one failure; detail is operator-facing truth, the
// webui renders what it gets.
func (ar *agentRoutes) httpError(w http.ResponseWriter, code int, format string, args ...any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf(format, args...)})
}

// registrationRows returns the pattern/handler rows the mount
// registers - exactly the mount lines agent/api carried pre-move.
func (ar *agentRoutes) registrationRows() []struct {
	pattern string
	handler func(http.ResponseWriter, *http.Request)
} {
	return []struct {
		pattern string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{"/agents", ar.handleAgents},
		{"/agents/{name}", ar.handleAgent},
		{"PUT /agents/{name}", ar.handleAgentUpdate},
		{"/agents/{name}/sessions", ar.handleAgentSessions},
	}
}

// agentRow is one roster row on the wire: the profile facts plus the
// agent's most-recent session id - the id the Agents tab binds a chat
// tab to when its row is clicked. The embedded Profile flattens into
// the SAME flat object the endpoint always emitted, so the extra field
// is additive to every existing reader (the webui decodes the union;
// nothing nests).
type agentRow struct {
	agent.Profile
	LastSessionID string `json:"last_session_id,omitempty"`
}

// fillAgentSessions writes session counts and last activity into
// profiles from the store that owns each agent's sessions, and returns
// each agent's MOST RECENT session id (the last_session_id column). A
// missing store leaves the zero row - an agent with zero ATTRIBUTABLE
// sessions is the honest row.
func (ar *agentRoutes) fillAgentSessions(ctx context.Context, profiles []agent.Profile) map[string]string {
	if ar.mountedStore() == nil {
		return nil
	}
	last := make(map[string]string, len(profiles))
	for i := range profiles {
		sessions, err := ar.readAgentSessions(ctx, profiles[i].Name)
		if err != nil {
			continue
		}
		profiles[i].SessionCount = len(sessions)
		bestAt := ""
		for _, sess := range sessions {
			if sess.UpdatedAt > profiles[i].LastActivity {
				profiles[i].LastActivity = sess.UpdatedAt
			}
			// The store orders by created_at, so ">=" leaves a tie in
			// updated_at on the LATER-created session - the newest one
			// an operator would call "most recent".
			if bestAt == "" || sess.UpdatedAt >= bestAt {
				bestAt = sess.UpdatedAt
				last[profiles[i].Name] = sess.ID
			}
		}
	}
	return last
}

// rosterRows pairs each profile with its most-recent session id. No
// session map (no store) = the profiles alone, the field omitted.
func rosterRows(profiles []agent.Profile, last map[string]string) []agentRow {
	rows := make([]agentRow, 0, len(profiles))
	for _, p := range profiles {
		rows = append(rows, agentRow{Profile: p, LastSessionID: last[p.Name]})
	}
	return rows
}

// handleAgents serves GET /agents (the inventory) and POST /agents
// (scaffold a new agent directory from the template).
func (ar *agentRoutes) handleAgents(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		profiles := agent.LoadProfiles(ar.home)
		last := ar.fillAgentSessions(r.Context(), profiles)
		ar.writeJSON(w, http.StatusOK, rosterRows(profiles, last))
	case http.MethodPost:
		var req struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			ar.httpError(w, http.StatusBadRequest, "invalid request: %v", err)
			return
		}
		if err := agent.ScaffoldAgent(ar.home, strings.TrimSpace(req.Name), req.Description); err != nil {
			ar.httpError(w, http.StatusConflict, "scaffold agent: %v", err)
			return
		}
		p, err := agent.LoadProfile(ar.home, strings.TrimSpace(req.Name))
		if err != nil {
			ar.httpError(w, http.StatusInternalServerError, "load agent: %v", err)
			return
		}
		ar.writeJSON(w, http.StatusCreated, p)
	default:
		ar.httpError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleAgent serves GET /agents/{name}: one agent, detail payload.
func (ar *agentRoutes) handleAgent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		ar.httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	name := r.PathValue("name")
	p, err := agent.LoadProfile(ar.home, name)
	if err != nil {
		ar.httpError(w, http.StatusNotFound, "unknown agent")
		return
	}
	profiles := []agent.Profile{p}
	last := ar.fillAgentSessions(r.Context(), profiles)
	ar.writeJSON(w, http.StatusOK, rosterRows(profiles, last)[0])
}

// handleAgentUpdate serves PUT /agents/{name} (card t_2d8deb8c,
// ruling 3: the agents tab's expansion carries an edit affordance).
// The write set is the scaffold's field set: description,
// provider.type / provider.model_name / provider.host /
// provider.api_key, system_prompt - plus settings tier 3b's two
// additive fields (card t_53523d5b): max_turns (a yaml int node) and
// unset (the dotted reset vocabulary, which DELETES the named keys so
// the scope falls back to main instead of pinning a phantom
// override). A nil field is untouched. The DEFAULT agent answers 409:
// its config IS the home config (the daemon's brain) and the agents
// tab renders it read-only.
func (ar *agentRoutes) handleAgentUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		ar.httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	name := r.PathValue("name")
	if name == agent.DefaultAgentName {
		ar.httpError(w, http.StatusConflict, "%s", agent.ErrAgentConfigLocked.Error())
		return
	}
	if _, err := agent.LoadProfile(ar.home, name); err != nil {
		ar.httpError(w, http.StatusNotFound, "unknown agent")
		return
	}
	var req struct {
		Description  *string  `json:"description"`
		ProviderType *string  `json:"provider_type"`
		ModelName    *string  `json:"model_name"`
		Host         *string  `json:"host"`
		APIKey       *string  `json:"api_key"`
		SystemPrompt *string  `json:"system_prompt"`
		MaxTurns     *int     `json:"max_turns"`
		Unset        []string `json:"unset"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		ar.httpError(w, http.StatusBadRequest, "invalid request: %v", err)
		return
	}
	updates := agent.AgentConfigUpdates{
		Description:  req.Description,
		ProviderType: req.ProviderType,
		ModelName:    req.ModelName,
		Host:         req.Host,
		APIKey:       req.APIKey,
		SystemPrompt: req.SystemPrompt,
		MaxTurns:     req.MaxTurns,
		Unset:        req.Unset,
	}
	if err := agent.UpdateAgentConfigLayer(ar.home, name, updates); err != nil {
		// A scaffold-law refusal (unknown provider type, the locked
		// default) is the caller's mistake - 400/409, not a 500. A
		// config file that will not parse is the layer's business.
		var scaffold *agent.AgentConfigScaffoldError
		switch {
		case errors.Is(agent.ErrAgentConfigLocked, err):
			ar.httpError(w, http.StatusConflict, "%s", err.Error())
			return
		case errors.As(err, &scaffold):
			ar.httpError(w, http.StatusBadRequest, "invalid request: %v", err)
			return
		}
		ar.httpError(w, http.StatusConflict, "update agent config: %v", err)
		return
	}
	p, err := agent.LoadProfile(ar.home, name)
	if err != nil {
		ar.httpError(w, http.StatusInternalServerError, "load agent: %v", err)
		return
	}
	ar.writeJSON(w, http.StatusOK, p)
}

// handleAgentSessions serves GET /agents/{name}/sessions (the agent's
// own session list - the default list never shows these rows) and
// POST /agents/{name}/sessions: create a session bound to this agent.
// Sessions are bound at creation and never switch agents, so there is
// no PATCH - a re-binding is a new session.
func (ar *agentRoutes) handleAgentSessions(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if _, err := agent.LoadProfile(ar.home, name); err != nil {
		ar.httpError(w, http.StatusNotFound, "unknown agent")
		return
	}
	// The 501 lane: no store capsule mounted at all. A mounted store
	// without the agent-session surface answers the same way on the
	// write path (errNoStoreSeam) - the read path finds no sessions.
	if ar.mountedStore() == nil {
		ar.httpError(w, http.StatusNotImplemented, "%s", errNoStoreSeam.Error())
		return
	}

	switch r.Method {
	case http.MethodGet:
		sessions, err := ar.readAgentSessions(r.Context(), name)
		if err != nil {
			ar.httpError(w, http.StatusInternalServerError, "list sessions: %v", err)
			return
		}
		// List contract: list endpoints emit arrays, never null. A zero-
		// session agent must render 200 [] or the webui's session read
		// crashes on a null .length (card t_619832ee).
		if sessions == nil {
			sessions = []core.Session{}
		}
		ar.writeJSON(w, http.StatusOK, sessions)
	case http.MethodPost:
		var req struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			ar.httpError(w, http.StatusBadRequest, "invalid request: %v", err)
			return
		}
		if req.ID == "" {
			id, err := agent.NewSessionID()
			if err != nil {
				// same fallback POST /sessions uses - crypto/rand
				// never fails on supported platforms.
				id = fmt.Sprintf("sess-%d", time.Now().UnixNano())
			}
			req.ID = id
		}
		// Same reserved-namespace guard as POST /sessions: an `eph-` id
		// would create a durable row the store-read endpoints can never
		// serve (the eph- lane is in-memory only). The predicate is the
		// agent package's (agent.IsEphemeralID) - one namespace, one
		// guard.
		if agent.IsEphemeralID(req.ID) {
			ar.httpError(w, http.StatusBadRequest, "invalid request: reserved session id prefix "+agent.EphemeralIDPrefix)
			return
		}
		// card t_81109e1c: the row is written to the store that will OWN
		// it - the agent home's own data/omega.db, never the daemon's
		// (the boot migration would move it there later, and the roster
		// would read zero sessions in the meantime).
		if err := ar.writeAgentSession(r.Context(), name, req.ID); err != nil {
			if errors.Is(err, errNoStoreSeam) {
				ar.httpError(w, http.StatusNotImplemented, "%s", errNoStoreSeam.Error())
				return
			}
			ar.httpError(w, http.StatusConflict, "create session: %v", err)
			return
		}
		sess := core.Session{ID: req.ID, Agent: name}
		if rows, err := ar.readAgentSessions(r.Context(), name); err == nil {
			for _, row := range rows {
				if row.ID == req.ID {
					sess = row
					break
				}
			}
		}
		ar.writeJSON(w, http.StatusCreated, sess)
	default:
		ar.httpError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}
