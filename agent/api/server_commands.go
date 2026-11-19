package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/EndoTheDev/omega/core"
)

// commandDispatchTimeout caps one extension-command dispatch (v0.14.1 audit NOTE-tier).
const commandDispatchTimeout = time.Minute

// The command palette's server surface. The old shell ran slash commands locally -
// "slash commands run locally and never hit the agent"; the webui palette dispatches
// them through the SAME registration seams: ctx.Commands + the chain-on-miss
// ctx.CommandHandler (rewired at boot and on every live toggle), and the skills list
// for /<skill-name>.

// commandListing is one GET /commands row.
type commandListing struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Args        []string `json:"args,omitempty"`
}

// commandDispatchRequest is one POST /commands/dispatch body. It is optional: a caller
// that binds no session (the terminal loop) keeps resolving from args.
type commandDispatchRequest struct {
	Name      string `json:"name"`
	Args      string `json:"args,omitempty"`
	SessionID string `json:"session_id,omitempty"`
}

// commandDispatchResponse announces WHAT ran.
type commandDispatchResponse struct {
	Kind    string           `json:"kind"` // "command" | "skill"
	Text    string           `json:"text,omitempty"`
	Name    string           `json:"name,omitempty"`
	Content string           `json:"content,omitempty"`
	Actions []core.CmdAction `json:"actions,omitempty"`
}

// compactAction is the /compact intent. The capsule declares it (it owns the
// summarizer, no store); the dispatch below runs it (it owns session shape).
const compactAction = "run_compact"

// runCompact folds the session a dispatch names and answers with what the fold actually
// did. Every branch names a real outcome or a real reason - a command that did nothing
// says so, in its own words.
func (s *Server) runCompact(ctx context.Context, sessionID string) (string, error) {
	switch {
	case sessionID == "":
		return "nothing folded: no session is bound to this chat yet", nil
	case isEphemeralID(sessionID):
		return "nothing folded: ephemeral sessions keep no history", nil
	}
	out, err := s.compactSession(ctx, sessionID)
	if err != nil {
		if errors.Is(err, errNoFoldFace) {
			return "", errors.New("this store cannot hold a context boundary")
		}
		return "", err
	}
	if !out.Folded {
		return "nothing folded: " + out.Why, nil
	}
	return fmt.Sprintf("folded %d messages into a summary - the context now starts at message %s, and the last %d stay verbatim",
		out.Rows, out.Start, out.Kept), nil
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

	for _, c := range cmds {
		if c.Name != req.Name {
			continue
		}
		if handler == nil {
			http.Error(w, "no command handler mounted", http.StatusNotImplemented)
			return
		}
		// the dispatch runs under a hard timeout (v0.14.1 audit NOTE-tier): a module command
		// handler that never returned previously pinned the HTTP request forever - now the
		// wire answers 500 and the client walks away the invoking surface's session rides the
		// context: a session-scoped command resolves it with core.CommandSession instead of
		// demanding the id in args
		ctx, cancel := context.WithTimeout(core.WithCommandSession(r.Context(), req.SessionID), commandDispatchTimeout)
		defer cancel()
		result, dErr := handler(ctx, req.Name, req.Args)
		if dErr != nil {
			// A miss is "nobody owns this name" - the same answer the not-registered path gives.
			if errors.Is(dErr, core.ErrCommandMiss) {
				http.Error(w, "unknown command: "+req.Name, http.StatusNotFound)
				return
			}
			http.Error(w, "command failed: "+dErr.Error(), http.StatusInternalServerError)
			return
		}
		text := result.Text
		// The host interprets the directive: a capsule owns the summarizer and no store,
		// so /compact declares the fold and this surface - which owns session shape - runs
		// it. The action is consumed here; nothing downstream has an executor for it.
		actions := make([]core.CmdAction, 0, len(result.Actions))
		runFold := false
		for _, a := range result.Actions {
			if a.Type == compactAction {
				runFold = true
				continue
			}
			actions = append(actions, a)
		}
		if runFold {
			line, fErr := s.runCompact(ctx, req.SessionID)
			if fErr != nil {
				http.Error(w, "command failed: "+fErr.Error(), http.StatusInternalServerError)
				return
			}
			text = line
		}
		writeJSON(w, http.StatusOK, commandDispatchResponse{Kind: "command", Text: text, Actions: actions})
		return
	}

	if strings.TrimPrefix(req.Name, "/") != "" {
		name := strings.TrimPrefix(req.Name, "/")
		if sk := s.skills(); sk != nil {
			_, contents, listErr := sk.SkillsEntries()
			if listErr != nil {
				http.Error(w, "list skills: "+listErr.Error(), http.StatusInternalServerError)
				return
			}
			if content, ok := contents[name]; ok {
				writeJSON(w, http.StatusOK, commandDispatchResponse{Kind: "skill", Name: name, Content: content})
				return
			}
		}
	}
	http.Error(w, "unknown command: "+req.Name, http.StatusNotFound)
}
