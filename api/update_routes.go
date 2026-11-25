package api

// The update channel's HTTP face. One host engine (core.UpdateSurface) serves the CLI,
// the webui and the desktop affordances; these routes are the shell's door to it.

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/EndoTheDev/omega/core"
)

// SetUpdateSurface binds the host's update engine. Called from the channel's wire, like
// the store and settings seams; nil leaves /updates answering 501.
func (s *Server) SetUpdateSurface(u core.UpdateSurface) { s.update = u }

// updateSurface returns the bound engine, or writes the seam-absent floor.
func (s *Server) updateSurface(w http.ResponseWriter) core.UpdateSurface {
	if s.update == nil {
		http.Error(w, "updates: no engine mounted", http.StatusNotImplemented)
		return nil
	}
	return s.update
}

// updateRequest is the accepted body of every write route: channel for check/stage, force
// for finalize. An absent body is legal - the config channel and the unforced path.
type updateRequest struct {
	Channel string `json:"channel"`
	Force   bool   `json:"force"`
}

func readUpdateRequest(r *http.Request) (updateRequest, error) {
	var req updateRequest
	if r.Body == nil {
		return req, nil
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<16)).Decode(&req); err != nil {
		if err.Error() == "EOF" {
			return updateRequest{}, nil
		}
		return updateRequest{}, err
	}
	return req, nil
}

// writeUpdateStatus answers with the status, carrying the live active-run count the pane
// needs to explain a refused quiet check. The count rides the wire only: the engine never
// imports the run hub.
func (s *Server) writeUpdateStatus(w http.ResponseWriter, st core.UpdateStatus) {
	st.ActiveRuns = s.ActiveRuns()
	writeJSON(w, http.StatusOK, st)
}

// handleUpdateStatus: GET /updates/status.
func (s *Server) handleUpdateStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	u := s.updateSurface(w)
	if u == nil {
		return
	}
	s.writeUpdateStatus(w, u.Status())
}

// handleUpdateCheck: POST /updates/check {"channel":"stable"|"nightly"}.
func (s *Server) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	u := s.updateSurface(w)
	if u == nil {
		return
	}
	req, err := readUpdateRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	st, err := u.Check(r.Context(), req.Channel)
	s.writeUpdateStatus(w, st)
	if err != nil {
		// The status body already carries the reason; the HTTP code says it failed.
		_ = err
	}
}

// handleUpdateStage: POST /updates/stage {"channel":...}. Returns while the download runs;
// the pane polls /updates/status for progress.
func (s *Server) handleUpdateStage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	u := s.updateSurface(w)
	if u == nil {
		return
	}
	req, err := readUpdateRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	st, err := u.Stage(r.Context(), req.Channel)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error(), "state": st.State})
		return
	}
	s.writeUpdateStatus(w, st)
}

// handleUpdateFinalize: POST /updates/finalize {"force":true}. Accepted, then the process
// restarts itself: the response is the last thing this build serves.
func (s *Server) handleUpdateFinalize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	u := s.updateSurface(w)
	if u == nil {
		return
	}
	req, err := readUpdateRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	st, err := u.Finalize(r.Context(), req.Force)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error(), "state": st.State})
		return
	}
	writeJSON(w, http.StatusAccepted, st)
}

// handleUpdateDiscard: POST /updates/discard - drop the staged artifacts.
func (s *Server) handleUpdateDiscard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	u := s.updateSurface(w)
	if u == nil {
		return
	}
	st, err := u.Discard()
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error(), "state": st.State})
		return
	}
	s.writeUpdateStatus(w, st)
}

// updateChannelValid names the two channels the API accepts; empty means "the config
// channel".
func updateChannelValid(channel string) bool {
	switch strings.ToLower(strings.TrimSpace(channel)) {
	case "", "stable", "nightly":
		return true
	}
	return false
}
