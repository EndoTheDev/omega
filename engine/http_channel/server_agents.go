package http_channel

// server_agents.go: the multi-agent REST surface (card t_7e2b7002).
//
//	GET  /agents                     profile inventory (list + detail data)
//	POST /agents                     scaffold a new agent directory
//	GET  /agents/{name}              one agent's detail
//	GET  /agents/{name}/sessions     sessions bound to this agent
//	POST /agents/{name}/sessions     create a session AS this agent
//
// SAME-FILE RULING (card t_7e2b7002): server.go is owned by card
// t_14f73742 while it runs. The three mount lines this file needs are
// recorded in the card comment and deferred to the review pass:
//
//	mux.HandleFunc("/agents", s.handleAgents)
//	mux.HandleFunc("/agents/{name}", s.handleAgent)
//	mux.HandleFunc("/agents/{name}/sessions", s.handleAgentSessions)
//
// until then this file compiles, is exercised by its own tests via a
// locally-built mux, and serves nothing (the deferred mount is the
// same pattern server_insights.go rode in on).

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/engine/agent"
)

// agentsHome is the omega home the profiles scan. main sets OMEGA_HOME
// for every process early; an unset var degrades to ~ (the same
// default the TUI path helpers used before the env became universal).
func agentsHome() string {
	if home := os.Getenv("OMEGA_HOME"); home != "" {
		return home
	}
	userHome, _ := os.UserHomeDir()
	return filepath.Join(userHome, ".omega")
}

// fillAgentSessions writes session counts and last activity into
// profiles from the optional agent session surface. A store without
// the surface (or nil) leaves the zeros - an agent with zero
// ATTRIBUTABLE sessions is the honest row.
func (s *Server) fillAgentSessions(profiles []agent.Profile) {
	if s.store == nil {
		return
	}
	as, ok := s.store.(core.AgentSessionStore)
	if !ok {
		return
	}
	ctx := context.Background()
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
// (scaffold a new agent directory from the template - no config form,
// creation v1 is this scaffold plus a config.yaml the file tree can
// edit, per the card scope).
func (s *Server) handleAgents(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		profiles := agent.LoadProfiles(agentsHome())
		s.fillAgentSessions(profiles)
		writeJSON(w, http.StatusOK, profiles)
	case http.MethodPost:
		var req struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := agent.ScaffoldAgent(agentsHome(), strings.TrimSpace(req.Name), req.Description); err != nil {
			http.Error(w, "scaffold agent: "+err.Error(), http.StatusConflict)
			return
		}
		p, err := agent.LoadProfile(agentsHome(), strings.TrimSpace(req.Name))
		if err != nil {
			http.Error(w, "load agent: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusCreated, p)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleAgent serves GET /agents/{name}: one agent, detail payload.
func (s *Server) handleAgent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := r.PathValue("name")
	p, err := agent.LoadProfile(agentsHome(), name)
	if err != nil {
		http.Error(w, "unknown agent", http.StatusNotFound)
		return
	}
	s.fillAgentSessions([]agent.Profile{p})
	writeJSON(w, http.StatusOK, p)
}

// handleAgentSessions serves GET /agents/{name}/sessions (the agent's
// own session list - the default list never shows these rows) and
// POST /agents/{name}/sessions: create a session bound to this agent.
// Sessions are bound at creation and never switch agents, so there is
// no PATCH - a re-binding is a new session.
func (s *Server) handleAgentSessions(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if _, err := agent.LoadProfile(agentsHome(), name); err != nil {
		http.Error(w, "unknown agent", http.StatusNotFound)
		return
	}
	if s.store == nil {
		http.Error(w, "session store not configured", http.StatusNotImplemented)
		return
	}
	as, ok := s.store.(core.AgentSessionStore)
	if !ok {
		http.Error(w, "store does not support agent sessions", http.StatusNotImplemented)
		return
	}
	switch r.Method {
	case http.MethodGet:
		sessions, err := as.ListAgentSessions(r.Context(), name)
		if err != nil {
			http.Error(w, "list sessions: "+err.Error(), http.StatusInternalServerError)
			return
		}
		// List contract: list endpoints emit arrays, never null. A zero-
		// session agent must render 200 [] or the webui's session read
		// crashes on a null .length (card t_619832ee).
		if sessions == nil {
			sessions = []core.Session{}
		}
		writeJSON(w, http.StatusOK, sessions)
	case http.MethodPost:
		var req struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
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
		// serve (the eph- lane is in-memory only).
		if isEphemeralID(req.ID) {
			http.Error(w, "invalid request: reserved session id prefix "+ephemeralPrefix, http.StatusBadRequest)
			return
		}
		if err := as.CreateAgentSession(r.Context(), req.ID, "", "", name); err != nil {
			http.Error(w, "create session: "+err.Error(), http.StatusConflict)
			return
		}
		sess, err := s.store.GetSession(r.Context(), req.ID)
		if err != nil {
			http.Error(w, "get session: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusCreated, sess)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
