package http_channel

// server_registry.go: the registry-browser toggle surface (v0.14.1).
//
//	GET  /disabled             the three disabled lists (skills/tools/agents)
//	PUT  /disabled/skills      {"name": x, "enabled": bool} -> prompt-build exclusion
//	PUT  /disabled/tools       {"name": x, "enabled": bool} -> registry exclusion
//	PUT  /disabled/agents      {"name": x, "enabled": bool} -> roster + wake exclusion
//
// The settings pane renders the module seam (/modules); this is its
// sibling for the file/folder REGISTRIES: the host owns config.yaml's
// `disabled:` fold through core.RegistryToggles - this file renders
// nothing but the wire (the server-toggle small law, same shape the
// module surface rode in on). A host without the seam (nil) answers
// 501 so the tabs can hide their toggles. The channel's own agent
// re-reads the fresh lists in the toggle's response - the same
// Context-stays-source-of-truth law RefreshToolProviders carries, one
// response carries the state so no second GET is needed.

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/EndoTheDev/omega/core"
)

// disabledLists is the /disabled wire shape: always three lists - a
// namespace with no entries is [] on the wire, never absent or null
// (a null .length read crashes the tab, card t_619832ee precedent).
type disabledLists struct {
	Skills []string `json:"skills"`
	Tools  []string `json:"tools"`
	Agents []string `json:"agents"`
}

// handleDisabled serves GET /disabled.
func (s *Server) handleDisabled(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.registries == nil {
		http.Error(w, "registry toggles not configured", http.StatusNotImplemented)
		return
	}
	writeSettingsJSON(w, disabledPayload(s.registries.Disabled()))
}

// handleDisabledSkills PUTs one disabled.skills entry.
func (s *Server) handleDisabledSkills(w http.ResponseWriter, r *http.Request) {
	s.handleDisabledToggle(w, r, s.registries.ToggleSkill)
}

// handleDisabledTools PUTs one disabled.tools entry. The seam carries
// the built-in-wins merge law: a built-in tool cannot be disabled and
// names no seat - the request errors instead of writing a dead key.
func (s *Server) handleDisabledTools(w http.ResponseWriter, r *http.Request) {
	s.handleDisabledToggle(w, r, s.registries.ToggleTool)
}

// handleDisabledAgents PUTs one disabled.agents entry (the soft
// toggle: registration untouched, roster + wakes excluded).
func (s *Server) handleDisabledAgents(w http.ResponseWriter, r *http.Request) {
	s.handleDisabledToggle(w, r, s.registries.ToggleAgent)
}

// handleDisabledToggle is the one PUT body all three namespaces share:
// decode, dispatch through the seam, answer the fresh lists (a
// toggle's response always carries the state it landed).
func (s *Server) handleDisabledToggle(w http.ResponseWriter, r *http.Request, toggle func(name string, enabled bool) error) {
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.registries == nil {
		http.Error(w, "registry toggles not configured", http.StatusNotImplemented)
		return
	}
	var req struct {
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		http.Error(w, "invalid request: name is required", http.StatusBadRequest)
		return
	}
	if err := toggle(req.Name, req.Enabled); err != nil {
		http.Error(w, err.Error(), statusForToggleRefusal(err))
		return
	}
	// The live half of the toggle: the seam wrote the fold and updated
	// the module registries + the Context; the serving agent re-reads
	// so the next run and the /tools inventory honor it without a
	// restart (core.LiveSeams law, one re-read per toggle).
	if s.agent != nil {
		s.agent.SetDisabledTools(s.registries.Disabled().Tools)
	}
	writeSettingsJSON(w, disabledPayload(s.registries.Disabled()))
}

// disabledPayload renders the wire shape: nil lists become [].
func disabledPayload(out core.DisabledNames) disabledLists {
	return disabledLists{Skills: orEmpty(out.Skills), Tools: orEmpty(out.Tools), Agents: orEmpty(out.Agents)}
}

// orEmpty never lets null ride the wire: a nil list is [].
func orEmpty(names []string) []string {
	if names == nil {
		return []string{}
	}
	return names
}

// statusForToggleRefusal maps a toggle error to its status: unknown
// registry entries answer 404 (loud refusal - a silently persisted
// unknown name is invisible forever), everything else 409.
func statusForToggleRefusal(err error) int {
	var unknown interface{ Unknown() bool }
	if errors.As(err, &unknown) && unknown.Unknown() {
		return http.StatusNotFound
	}
	return http.StatusConflict
}