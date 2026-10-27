package kanban

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/EndoTheDev/omega/engine/agent"
)

// The board REST surface (v0.12 phase F): the routes http_channel
// served over the kanban core seam serve from the capsule itself now -
// the module noun left core with the seam types (kanbantypes.go), and
// the routes register through RegisterRoute. The wire shapes are the
// ones the webui consumes, byte-stable from the engine-side move:
// GET /tasks list/filter, POST /tasks create, GET /tasks/{id} detail
// (task + comments + events), PATCH /tasks/{id}, POST
// /tasks/{id}/comments, GET /boards. A disabled kanban answers 404 -
// no route registered, nothing to degrade to (Q7: one mount path).
//
// There is deliberately no cross-board mutation endpoint: a PATCH
// moves a card inside its board only. Switching boards is the
// registry pointer's job (omega kanban board use), never a card op.

// writeJSON encodes v and answers status, the envelope the
// engine-side handlers wrote.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// boardParam reads the ?board= picker from the request.
func boardParam(r *http.Request) string {
	return r.URL.Query().Get("board")
}

// taskDetail is the GET /tasks/{id} payload: the task plus its thread.
type taskDetail struct {
	Task     KanbanTask      `json:"task"`
	Comments []KanbanComment `json:"comments"`
	Events   []KanbanEvent   `json:"events"`
}

// taskByIDError maps store errors onto status codes: missing task 404,
// bad input 400, everything else 500.
func taskByIDError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrTaskNotFound) {
		http.Error(w, "task not found: "+err.Error(), http.StatusNotFound)
		return
	}
	http.Error(w, "update task: "+err.Error(), http.StatusBadRequest)
}

// boardsHandler serves GET /boards: the board registry with the
// current board flagged. Registered only when the store carries the
// multi-board capability (a single-board implementation stays legal).
func boardsHandler(store BoardLister) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		boards, err := store.Boards(r.Context())
		if err != nil {
			http.Error(w, "list boards: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"boards": boards})
	}
}

// tasksHandler serves GET /tasks (list; ?status= filters, the empty
// value lists every status; ?board= picks the board) and POST /tasks
// (create; title required, unspecified status defaults to triage in
// the store).
func tasksHandler(store restProvider) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			tasks, err := store.ListTasks(r.Context(), boardParam(r), r.URL.Query().Get("status"))
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
			out, err := store.CreateTask(r.Context(), boardParam(r), KanbanTask{
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
}

// taskByIDHandler serves GET /tasks/{id} (task + comments + events) and
// PATCH /tasks/{id} (any legal status transition by any caller - one
// agent, no dispatch races to armor). PATCH pointer fields distinguish
// "set to this value (possibly empty)" from "leave alone".
func taskByIDHandler(store restProvider) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			http.Error(w, "missing task id", http.StatusBadRequest)
			return
		}
		switch r.Method {
		case http.MethodGet:
			t, err := store.GetTask(r.Context(), boardParam(r), id)
			if err != nil {
				taskByIDError(w, err)
				return
			}
			comments, err := store.GetTaskComments(r.Context(), boardParam(r), id)
			if err != nil {
				http.Error(w, "get comments: "+err.Error(), http.StatusInternalServerError)
				return
			}
			events, err := store.GetTaskEvents(r.Context(), boardParam(r), id)
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
			// task.update keeps a simple non-pointer Status field
			// ("" = no change); wire it through directly
			out, err := store.UpdateTask(r.Context(), boardParam(r), id, KanbanPatch{
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
}

// taskCommentsHandler serves POST /tasks/{id}/comments. An absent
// author is "web" - the board must be able to tell a human UI comment
// from an agent tool comment.
func taskCommentsHandler(store restProvider) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
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
		c, err := store.AddComment(r.Context(), boardParam(r), id, body.Author, body.Body)
		if err != nil {
			taskByIDError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, c)
	}
}

// restProvider is the slice of KanbanProvider the handlers need.
type restProvider interface {
	CreateTask(ctx context.Context, board string, in KanbanTask) (KanbanTask, error)
	ListTasks(ctx context.Context, board, status string) ([]KanbanTask, error)
	GetTask(ctx context.Context, board, id string) (KanbanTask, error)
	GetTaskComments(ctx context.Context, board, taskID string) ([]KanbanComment, error)
	GetTaskEvents(ctx context.Context, board, taskID string) ([]KanbanEvent, error)
	UpdateTask(ctx context.Context, board, taskID string, patch KanbanPatch) (KanbanTask, error)
	AddComment(ctx context.Context, board, taskID, author, body string) (KanbanComment, error)
}

// registerRoutes hands the capsule's REST routes to the registry
// (RegisterRoute, the phase-D vocabulary): the served surface rides
// ctx.ModuleRoutes from here on.
func (p *Plugin) registerRoutes(api *agent.ExtensionAPI) error {
	rows := []struct {
		pattern string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{"GET /tasks", tasksHandler(p.store)},
		{"POST /tasks", tasksHandler(p.store)},
		{"GET /tasks/{id}", taskByIDHandler(p.store)},
		{"PATCH /tasks/{id}", taskByIDHandler(p.store)},
		{"POST /tasks/{id}/comments", taskCommentsHandler(p.store)},
	}
	if b, ok := any(p.store).(BoardLister); ok {
		rows = append(rows, struct {
			pattern string
			handler func(http.ResponseWriter, *http.Request)
		}{"GET /boards", boardsHandler(b)})
	}
	for _, row := range rows {
		if err := api.RegisterRoute(row.pattern, row.handler); err != nil {
			return err
		}
	}
	return nil
}
