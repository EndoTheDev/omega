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
	"fmt"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// agentRoutes is the roster REST face, built on the plugin's root home
// and mount context. One value per mount; every handler reads the
// same pair.
type agentRoutes struct {
	home string
	ctx  *core.Context
}

// agentSessionStore resolves the store seam per request, narrowed to
// the agent-session surface. Nil (no store, or a store without the
// surface) leaves the zero row - an agent with zero ATTRIBUTABLE
// sessions is the honest row.
func (ar *agentRoutes) agentSessionStore() (agent.StoreProvider, agent.AgentSessionStore) {
	st, _ := ar.ctx.Services().Resolve("store").(agent.StoreProvider)
	as, _ := st.(agent.AgentSessionStore)
	return st, as
}

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

// fillAgentSessions writes session counts and last activity into
// profiles from the store's agent-session surface.
func (ar *agentRoutes) fillAgentSessions(ctx context.Context, profiles []agent.Profile) {
	_, as := ar.agentSessionStore()
	if as == nil {
		return
	}
	for i := range profiles {
		sessions, err := as.ListAgentSessions(ctx, profiles[i].Name)
		if err != nil {
			continue
		}
		profiles[i].SessionCount = len(sessions)
		for _, sess := range sessions {
			if sess.UpdatedAt > profiles[i].LastActivity {
				profiles[i].LastActivity = sess.UpdatedAt
			}
		}
	}
}

// handleAgents serves GET /agents (the inventory) and POST /agents
// (scaffold a new agent directory from the template).
func (ar *agentRoutes) handleAgents(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		profiles := agent.LoadProfiles(ar.home)
		ar.fillAgentSessions(r.Context(), profiles)
		ar.writeJSON(w, http.StatusOK, profiles)
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
	ar.fillAgentSessions(r.Context(), []agent.Profile{p})
	ar.writeJSON(w, http.StatusOK, p)
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
	st, as := ar.agentSessionStore()
	if as == nil {
		ar.httpError(w, http.StatusNotImplemented, "store does not support agent sessions")
		return
	}

	switch r.Method {
	case http.MethodGet:
		sessions, err := as.ListAgentSessions(r.Context(), name)
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
		if err := as.CreateAgentSession(r.Context(), req.ID, "", "", name); err != nil {
			ar.httpError(w, http.StatusConflict, "create session: %v", err)
			return
		}
		sess, err := st.GetSession(r.Context(), req.ID)
		if err != nil {
			ar.httpError(w, http.StatusInternalServerError, "get session: %v", err)
			return
		}
		ar.writeJSON(w, http.StatusCreated, sess)
	default:
		ar.httpError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}