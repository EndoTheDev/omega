package kanban

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/EndoTheDev/omega/agent"
)

// The board REST surface (v0.12 phase F): the routes http_channel
// served over the kanban core seam serve from the capsule itself now -
// the module noun left core with the seam types (kanbantypes.go), and
// the routes register through RegisterRoute. The wire shapes are the
// the ones the webui consumes, byte-stable from the source move:
// GET /tasks list/filter, POST /tasks create, GET /tasks/{id} detail
// (task + comments + events), PATCH /tasks/{id}, DELETE /tasks/{id}
// (the TaskDeleter capability), POST /tasks/{id}/comments, GET /boards,
// and - since the boards folded into the one database (t_feae19d3) -
// POST /boards to create and DELETE /boards/{name} to remove an empty
// non-default board. A disabled kanban answers 404 - no route
// registered, nothing to degrade to (Q7: one mount path).
//
// There is deliberately no cross-board mutation endpoint: a PATCH
// moves a card inside its board only. Switching boards is the registry
// is_current flag's job (omega kanban board use), never a card op.
// Deleting is a registry op like switching, but destructive - the
// handler keeps DeleteBoardRow's guards verbatim (default board,
// unknown board, current flag holder, non-empty boards all refused)
// and answers 400 with the refusal text.

// writeJSON encodes v and answers status, the envelope the
// handlers before the move wrote.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// boardParam reads the ?board= picker from the request.
func boardParam(r *http.Request) string {
	return r.URL.Query().Get("board")
}

// deleteFromStore deletes through the optional TaskDeleter capability
// (BoardLister/Reorderer's shape): a store that cannot delete answers
// 400 with the reason - the board UI's delete control refuses first,
// this is the wire's backstop.
func deleteFromStore(store restProvider, w http.ResponseWriter, r *http.Request, id string) {
	td, ok := any(store).(TaskDeleter)
	if !ok {
		http.Error(w, "this kanban store cannot delete tasks", http.StatusBadRequest)
		return
	}
	if err := td.DeleteTask(r.Context(), boardParam(r), id); err != nil {
		taskByIDError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
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
				Priority int    `json:"priority"`
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
				Priority: body.Priority,
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

// taskByIDHandler serves GET /tasks/{id} (task + comments + events),
// PATCH /tasks/{id} (any legal status transition by any caller - one
// agent, no dispatch races to armor; PATCH pointer fields distinguish
// "set to this value (possibly empty)" from "leave alone") and DELETE
// /tasks/{id} (through the optional TaskDeleter capability - a store
// without it answers 400 with the reason).
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
				Priority  *int    `json:"priority"`
				Status    string  `json:"status"`
				SessionID *string `json:"session_id"`
				AfterID   *string `json:"after_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
				return
			}
			// task.update keeps a simple non-pointer Status field
			// ("" = no change); wire it through directly. An after_id body
			// is the drag-order slot instead: the reorder capability is
			// optional on the seam (single-order-agnostic stores stay
			// legal), so an after_id against one that cannot reorder
			// answers 400 with the reason - the board refetches and the
			// card snaps back.
			var (
				out KanbanTask
				err error
			)
			if body.AfterID != nil {
				ro, ok := any(store).(Reorderer)
				if !ok {
					http.Error(w, "this kanban store cannot reorder tasks", http.StatusBadRequest)
					return
				}
				out, err = ro.ReorderTask(r.Context(), boardParam(r), id, body.Status, *body.AfterID)
			} else {
				out, err = store.UpdateTask(r.Context(), boardParam(r), id, KanbanPatch{
					Title:     body.Title,
					Body:      body.Body,
					Assignee:  body.Assignee,
					Priority:  body.Priority,
					Status:    body.Status,
					SessionID: body.SessionID,
				})
			}
			if err != nil {
				taskByIDError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, out)
		default:
			deleteFromStore(store, w, r, id)
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

// boardCreateHandler serves POST /boards: create a board (name in the
// JSON body, board-name grammar enforced). Duplicates and reserved
// names answer 400 with the refusal text.
func boardCreateHandler(store boardRegistry) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := store.CreateBoardRow(body.Name); err != nil {
			http.Error(w, "create board: "+err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"created": body.Name})
	}
}

// boardRegistry is the slice of *Store the board routes need (the DB
// registry is a store capability now, not a home-path op).
type boardRegistry interface {
	CreateBoardRow(name string) error
	DeleteBoardRow(name string) error
}

// boardDeleteHandler serves DELETE /boards/{name}: remove a board
// through the registry's own DeleteBoardRow, keeping every guard (the
// default board, the current board, unknown boards, and any board that
// still holds tasks are refused with a 400 carrying the refusal text -
// the webui surfaces it verbatim). There is no force: the guards are
// the law. DeleteBoardRow is the irreversible step (the board's rows
// are already gone; an empty board loses only its row); the webui's
// confirm click is only a courtesy double-take, this handler is the law.
func boardDeleteHandler(store boardRegistry) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if name == "" {
			http.Error(w, "missing board name", http.StatusBadRequest)
			return
		}
		if err := store.DeleteBoardRow(name); err != nil {
			http.Error(w, "delete board: "+err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"deleted": name})
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
		{"DELETE /tasks/{id}", taskByIDHandler(p.store)},
		{"POST /tasks/{id}/comments", taskCommentsHandler(p.store)},
	}
	if b, ok := any(p.store).(BoardLister); ok {
		rows = append(rows, struct {
			pattern string
			handler func(http.ResponseWriter, *http.Request)
		}{"GET /boards", boardsHandler(b)})
		if reg, ok := any(p.store).(boardRegistry); ok {
			rows = append(rows, struct {
				pattern string
				handler func(http.ResponseWriter, *http.Request)
			}{"POST /boards", boardCreateHandler(reg)})
			rows = append(rows, struct {
				pattern string
				handler func(http.ResponseWriter, *http.Request)
			}{"DELETE /boards/{name}", boardDeleteHandler(reg)})
		}
	}
	for _, row := range rows {
		if err := api.RegisterRoute(row.pattern, row.handler); err != nil {
			return err
		}
	}
	return nil
}
