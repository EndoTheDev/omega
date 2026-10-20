package http_channel

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/EndoTheDev/omega/agent"
)

// The board HTTP surface: the kanban module's provider over
// GET /tasks (list, filterable), POST /tasks, GET /tasks/{id}
// (task + comments + events), PATCH /tasks/{id} and
// POST /tasks/{id}/comments. Every route answers 501 when the
// kanban seam is not mounted - the board is optional, the API
// degrades to a clear message instead of a panic.

// boardUnavailable is the shared 501 guard for unmounted kanban.
func boardUnavailable(w http.ResponseWriter) bool {
	http.Error(w, "kanban board not configured", http.StatusNotImplemented)
	return true
}

// taskDetail is the GET /tasks/{id} payload: the task plus its thread.
type taskDetail struct {
	Task     agent.KanbanTask      `json:"task"`
	Comments []agent.KanbanComment `json:"comments"`
	Events   []agent.KanbanEvent   `json:"events"`
}

// handleTasks serves GET /tasks (list; ?status= filters, the empty
// value lists every status) and POST /tasks (create; title required,
// unspecified status defaults to triage in the store).
func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	if s.Kanban == nil {
		boardUnavailable(w)
		return
	}
	switch r.Method {
	case http.MethodGet:
		tasks, err := s.Kanban.ListTasks(r.Context(), r.URL.Query().Get("status"))
		if err != nil {
			http.Error(w, "list tasks: "+err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"tasks": tasks})
	case http.MethodPost:
		var body struct {
			Title    string `json:"title"`
			Body     string `json:"body"`
			Assignee string `json:"assignee"`
			Status   string `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
			return
		}
		out, err := s.Kanban.CreateTask(r.Context(), agent.KanbanTask{
			Title:    body.Title,
			Body:     body.Body,
			Assignee: body.Assignee,
			Status:   body.Status,
		})
		if err != nil {
			http.Error(w, "create task: "+err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusCreated, out)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleTaskByID serves GET /tasks/{id} (task + comments + events) and
// PATCH /tasks/{id} (any legal status transition by any caller - one
// agent, no dispatch races to armor). PATCH pointer fields distinguish
// "set to this value (possibly empty)" from "leave alone".
func (s *Server) handleTaskByID(w http.ResponseWriter, r *http.Request) {
	if s.Kanban == nil {
		boardUnavailable(w)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing task id", http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodGet:
		t, err := s.Kanban.GetTask(r.Context(), id)
		if err != nil {
			taskByIDError(w, err)
			return
		}
		comments, err := s.Kanban.GetTaskComments(r.Context(), id)
		if err != nil {
			http.Error(w, "get comments: "+err.Error(), http.StatusInternalServerError)
			return
		}
		events, err := s.Kanban.GetTaskEvents(r.Context(), id)
		if err != nil {
			http.Error(w, "get events: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, taskDetail{Task: t, Comments: comments, Events: events})
	case http.MethodPatch:
		var body struct {
			Title     *string `json:"title"`
			Body      *string `json:"body"`
			Assignee  *string `json:"assignee"`
			Status    string  `json:"status"`
			SessionID *string `json:"session_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
			return
		}
		// task.update keeps a simple non-pointer Status field ("" = no
		// change); wire it through directly
		out, err := s.Kanban.UpdateTask(r.Context(), id, agent.KanbanPatch{
			Title:     body.Title,
			Body:      body.Body,
			Assignee:  body.Assignee,
			Status:    body.Status,
			SessionID: body.SessionID,
		})
		if err != nil {
			taskByIDError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleTaskComments serves POST /tasks/{id}/comments. An absent author
// is "web" - the board must be able to tell a human UI comment from an
// agent tool comment.
func (s *Server) handleTaskComments(w http.ResponseWriter, r *http.Request) {
	if s.Kanban == nil {
		boardUnavailable(w)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing task id", http.StatusBadRequest)
		return
	}
	var body struct {
		Author string `json:"author"`
		Body   string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.Author == "" {
		body.Author = "web"
	}
	c, err := s.Kanban.AddComment(r.Context(), id, body.Author, body.Body)
	if err != nil {
		taskByIDError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

// taskByIDError maps store errors onto status codes: missing task 404,
// bad input 400, everything else 500.
func taskByIDError(w http.ResponseWriter, err error) {
	if errors.Is(err, agent.ErrTaskNotFound) {
		http.Error(w, "task not found: "+err.Error(), http.StatusNotFound)
		return
	}
	http.Error(w, "update task: "+err.Error(), http.StatusBadRequest)
}
