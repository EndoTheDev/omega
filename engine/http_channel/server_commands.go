package http_channel

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/EndoTheDev/omega/core"
)

// The command palette's server surface (card t_07aed3f9). The TUI ran
// slash commands locally - "slash commands run locally and never hit
// the agent"; the webui palette dispatches them through the SAME
// registration seams: ctx.Commands + the chain-on-miss
// ctx.CommandHandler (rewired at boot and on every live toggle), and
// the skills list for /<skill-name>. No agent round-trip happens here.

// commandListing is one GET /commands row.
type commandListing struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Args        []string `json:"args,omitempty"`
}

// commandDispatchRequest is one POST /commands/dispatch body.
type commandDispatchRequest struct {
	Name string `json:"name"`
	Args string `json:"args,omitempty"`
}

// commandDispatchResponse announces WHAT ran: an extension command's
// result text, or a skill whose content the client arms as a system
// preamble on its next send (the TUI injected skill content the same
// way - one System message into its history). The skills content DID
// ride this wire by design: the dispatch endpoint is the command
// execution surface, the parity of the TUI's in-process injection.
type commandDispatchResponse struct {
	Kind    string           `json:"kind"` // "command" | "skill"
	Text    string           `json:"text,omitempty"`
	Name    string           `json:"name,omitempty"`
	Content string           `json:"content,omitempty"`
	Actions []core.CmdAction `json:"actions,omitempty"`
}

func (s *Server) handleCommands(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	s.seams.mu.RLock()
	cmds := s.seams.commands
	s.seams.mu.RUnlock()
	out := make([]commandListing, 0, len(cmds))
	for _, c := range cmds {
		out = append(out, commandListing{Name: c.Name, Description: c.Description, Args: c.Args})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCommandDispatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req commandDispatchRequest
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil || json.Unmarshal(body, &req) != nil || req.Name == "" {
		http.Error(w, "invalid request: name is required", http.StatusBadRequest)
		return
	}
	if !strings.HasPrefix(req.Name, "/") {
		http.Error(w, "invalid request: name must be a slash command", http.StatusBadRequest)
		return
	}

	s.seams.mu.RLock()
	cmds := s.seams.commands
	handler := s.seams.commandHandler
	s.seams.mu.RUnlock()

	// Extension command first: the module that registered it executes
	// it (chain-on-miss handler, exactly the TUI's dispatch).
	for _, c := range cmds {
		if c.Name != req.Name {
			continue
		}
		if handler == nil {
			http.Error(w, "no command handler mounted", http.StatusNotImplemented)
			return
		}
		result, dErr := handler(context.Background(), req.Name, req.Args)
		if dErr != nil {
			http.Error(w, "command failed: "+dErr.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, commandDispatchResponse{Kind: "command", Text: result.Text, Actions: result.Actions})
		return
	}

	// Not a registered command: a skill command injects content.
	if strings.TrimPrefix(req.Name, "/") != "" {
		for _, sk := range s.contextSkills() {
			if "/"+sk.Name == req.Name {
				writeJSON(w, http.StatusOK, commandDispatchResponse{Kind: "skill", Name: sk.Name, Content: sk.Content})
				return
			}
		}
	}
	http.Error(w, "unknown command: "+req.Name, http.StatusNotFound)
}