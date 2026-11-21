package kanban

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/EndoTheDev/omega/agent"
)

// The board REST surface: the wire shapes the webui consumes. Switching boards is the
// registry is_current flag's job (omega kanban board use), never a card op.

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func boardParam(r *http.Request) string {
	return r.URL.Query().Get("board")
}

const scopeAll = "all"

// allBoardLister is the optional capability behind ?scope=all - the same
// shape as TaskDeleter: a store that cannot answer it serves a 400
// instead of a silently empty list.
type allBoardLister interface {
	ListAllTasks(ctx context.Context, status string) ([]KanbanTask, error)
}

// deleteFromStore deletes through the optional TaskDeleter capability
// (BoardLister/Reorderer's shape): a store that cannot delete answers 400 with the
// reason - the board UI's delete control refuses first, this is the wire's backstop.
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

type taskDetail struct {
	Task     KanbanTask      `json:"task"`
	Comments []KanbanComment `json:"comments"`
	Events   []KanbanEvent   `json:"events"`
}

func taskByIDError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrTaskNotFound) {
		http.Error(w, "task not found: "+err.Error(), http.StatusNotFound)
		return
	}
	http.Error(w, "update task: "+err.Error(), http.StatusBadRequest)
}

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

func tasksHandler(store restProvider) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if r.URL.Query().Get("scope") == scopeAll {
				all, ok := any(store).(allBoardLister)
				if !ok {
					http.Error(w, "this kanban store cannot list every board", http.StatusBadRequest)
					return
				}
				tasks, err := all.ListAllTasks(r.Context(), r.URL.Query().Get("status"))
				if err != nil {
					http.Error(w, "list tasks: "+err.Error(), http.StatusBadRequest)
					return
				}
				writeJSON(w, http.StatusOK, map[string]any{"tasks": tasks})
				return
			}
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

// taskByIDHandler serves GET /tasks/{id} (task + comments + events), PATCH /tasks/{id}
// (any legal status transition by any caller - one agent, no dispatch races to armor)
// and DELETE /tasks/{id} (through the optional TaskDeleter capability).
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
			// task.update keeps a simple non-pointer Status field ("" = no change). An after_id
			// body is the drag-order slot instead: the reorder capability is optional on the
			// seam (single-order-agnostic stores stay legal), so an after_id against one that
			// cannot reorder answers 400 with the reason - the board refetches and the card
			// snaps back.
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

// taskCommentsHandler serves POST /tasks/{id}/comments. An absent author is "web" - the
// board must be able to tell a human UI comment from an agent tool comment.
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

type boardRegistry interface {
	CreateBoardRow(name string) error
	RenameBoardRow(oldName, newName string) error
	DeleteBoardRow(name string) error
}

// boardRenameHandler serves PATCH /boards/{name}. The row keeps its id, so
// the cards do not move; the refusal text (illegal name, duplicate name, unknown board)
// answers 400 and the webui shows it verbatim.
func boardRenameHandler(store boardRegistry) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := r.PathValue("name")
		if name == "" {
			http.Error(w, "missing board name", http.StatusBadRequest)
			return
		}
		var body struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := store.RenameBoardRow(name, body.Name); err != nil {
			http.Error(w, "rename board: "+err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"renamed": body.Name, "from": name})
	}
}

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

type restProvider interface {
	CreateTask(ctx context.Context, board string, in KanbanTask) (KanbanTask, error)
	ListTasks(ctx context.Context, board, status string) ([]KanbanTask, error)
	GetTask(ctx context.Context, board, id string) (KanbanTask, error)
	GetTaskComments(ctx context.Context, board, taskID string) ([]KanbanComment, error)
	GetTaskEvents(ctx context.Context, board, taskID string) ([]KanbanEvent, error)
	UpdateTask(ctx context.Context, board, taskID string, patch KanbanPatch) (KanbanTask, error)
	AddComment(ctx context.Context, board, taskID, author, body string) (KanbanComment, error)
}

// pushTasks and pushAll are the refresh hooks the REST wrapper rides.
// Nil-safe: a plugin mounted without a canvas registration pushes
// nothing, and the frontend's snapshot read is the catch-up.
func (p *Plugin) pushTasks() {
	if p.capsule != nil {
		p.capsule.refreshTasks()
	}
}

func (p *Plugin) pushAll() {
	if p.capsule != nil {
		p.capsule.refreshAll()
	}
}

// statusRecorder remembers the status a handler answered, so the push wrapper can
// tell a mutation from a refusal.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}

// pushAfterMutation wraps one mutating REST handler. A failed mutation pushes nothing
// (the error text is the answer); GET routes are never wrapped.
func (p *Plugin) pushAfterMutation(refresh func(), h func(http.ResponseWriter, *http.Request)) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w}
		h(rec, r)
		if rec.status < 200 || rec.status >= 300 {
			return
		}
		refresh()
	}
}

func (p *Plugin) registerRoutes(api *agent.ExtensionAPI) error {
	rows := []struct {
		pattern string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{"GET /tasks", tasksHandler(p.store)},
		{"POST /tasks", p.pushAfterMutation(p.pushTasks, tasksHandler(p.store))},
		{"GET /tasks/{id}", taskByIDHandler(p.store)},
		{"PATCH /tasks/{id}", p.pushAfterMutation(p.pushTasks, taskByIDHandler(p.store))},
		{"DELETE /tasks/{id}", p.pushAfterMutation(p.pushTasks, taskByIDHandler(p.store))},
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
			}{"POST /boards", p.pushAfterMutation(p.pushAll, boardCreateHandler(reg))})
			rows = append(rows, struct {
				pattern string
				handler func(http.ResponseWriter, *http.Request)
			}{"PATCH /boards/{name}", p.pushAfterMutation(p.pushAll, boardRenameHandler(reg))})
			rows = append(rows, struct {
				pattern string
				handler func(http.ResponseWriter, *http.Request)
			}{"DELETE /boards/{name}", p.pushAfterMutation(p.pushAll, boardDeleteHandler(reg))})
		}
	}
	for _, row := range rows {
		if err := api.RegisterRoute(row.pattern, row.handler); err != nil {
			return err
		}
	}
	return nil
}
