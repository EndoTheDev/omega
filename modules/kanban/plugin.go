package kanban

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/EndoTheDev/omega/agent"
)

// Plugin adapts the kanban Store into omega's plugin system. It owns
// the exclusive "kanban" seam (the board provider the HTTP channel
// serves /tasks from) and contributes the board tools ("tools" seam,
// additive) so the agent manages the board natively in chat.
type Plugin struct {
	store *Store
}

// Compile-time assertions: *Plugin is a Plugin, *Store a KanbanProvider.
var _ agent.Plugin = (*Plugin)(nil)

// NewPlugin returns a kanban Plugin instance.
func NewPlugin() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string     { return "kanban" }
func (p *Plugin) Provides() []string { return []string{"kanban", "tools"} }

// Requires: the board tools are plain store CRUD - nothing to wait for.
func (p *Plugin) Requires() []string { return nil }

// Mount opens the board db and mounts the provider plus the tools.
// The db path is config db_path; the relative default resolves against
// OMEGA_HOME. A mount failure (bad dsn, unreadable path) is a startup
// error - a board that silently does not exist is worse than a crash.
func (p *Plugin) Mount(ctx *agent.Context) error {
	cfg := Default()
	if c, ok := ctx.Configs["kanban"].(Config); ok {
		cfg = c
	}
	dsn := cfg.DBPath
	if dsn == "kanban.db" {
		dsn = filepath.Join(kanbanHome(), "kanban.db")
	}
	s, err := Open(dsn)
	if err != nil {
		return fmt.Errorf("kanban store: %w", err)
	}
	p.store = s
	ctx.Kanban = s
	ctx.ToolProviders = append(ctx.ToolProviders, &ToolProvider{board: s})
	return nil
}

// kanbanHome resolves OMEGA_HOME (mirrors cron's home logic).
func kanbanHome() string {
	home := os.Getenv("OMEGA_HOME")
	if home == "" {
		home = filepath.Join(os.Getenv("HOME"), ".omega")
	}
	return home
}

// KToolProvider exposes the board as agent tools. The tools take the
// provider seam instead of *Store so embedders can swap implementations.
type ToolProvider struct {
	board agent.KanbanProvider
}

// Tools returns the six board tools.
func (p *ToolProvider) Tools() map[string]agent.Tool {
	return map[string]agent.Tool{
		"task_create": {
			Description: "Create a board task (title is the short name, body carries the full spec).",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title":    map[string]any{"type": "string", "description": "Short title (required)."},
					"body":     map[string]any{"type": "string", "description": "Full spec / body (optional)."},
					"assignee": map[string]any{"type": "string", "description": "Who the task is for (optional)."},
					"status":   map[string]any{"type": "string", "description": "Initial status (optional, default triage). One of triage todo scheduled ready running blocked review done archived."},
					"source":   map[string]any{"type": "string", "description": "Provenance tag, e.g. an automator's name (optional)."},
				},
				"required": []string{"title"},
			},
			Run: p.runCreate,
		},
		"task_list": {
			Description: "List board tasks, optionally filtered by status.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"status": map[string]any{"type": "string", "description": "Filter by status (optional). Omit for every task."},
				},
			},
			Run: p.runList,
		},
		"task_update": {
			Description: "Update a task: move status (any legal transition), set assignee, retitle, or rewrite the body.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":       map[string]any{"type": "string", "description": "Task id (required)."},
					"status":   map[string]any{"type": "string", "description": "New status (optional)."},
					"assignee": map[string]any{"type": "string", "description": "New assignee, empty string unassigns (optional)."},
					"title":    map[string]any{"type": "string", "description": "New title (optional)."},
					"body":     map[string]any{"type": "string", "description": "New body (optional)."},
				},
				"required": []string{"id"},
			},
			Run: p.runUpdate,
		},
		"task_comment": {
			Description: "Add a comment to a task's thread.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":    map[string]any{"type": "string", "description": "Task id (required)."},
					"body":  map[string]any{"type": "string", "description": "Comment body (required)."},
					"author": map[string]any{"type": "string", "description": "Comment author (optional, default agent)."},
				},
				"required": []string{"id", "body"},
			},
			Run: p.runComment,
		},
		"task_complete": {
			Description: "Mark a task done (moves it to the done column and stamps completion).",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{"type": "string", "description": "Task id (required)."},
				},
				"required": []string{"id"},
			},
			Run: p.runComplete,
		},
		"task_link_session": {
			Description: "Link a task to the conversation it belongs to (defaults to the current session).",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":         map[string]any{"type": "string", "description": "Task id (required)."},
					"session_id": map[string]any{"type": "string", "description": "Session id (optional, default current session)."},
				},
				"required": []string{"id"},
			},
			Run: p.runLink,
		},
	}
}

func (p *ToolProvider) runCreate(ctx context.Context, args map[string]any) (string, error) {
	in := agent.KanbanTask{
		Title:     str(args["title"]),
		Body:      str(args["body"]),
		Assignee:  str(args["assignee"]),
		Status:    str(args["status"]),
		Source:    str(args["source"]),
		SessionID: agent.SessionIDFromContext(ctx),
	}
	out, err := p.board.CreateTask(ctx, in)
	if err != nil {
		return "", err
	}
	return "created " + out.ID + " [" + out.Status + "] " + out.Title, nil
}

func (p *ToolProvider) runList(ctx context.Context, args map[string]any) (string, error) {
	tasks, err := p.board.ListTasks(ctx, str(args["status"]))
	if err != nil {
		return "", err
	}
	if len(tasks) == 0 {
		return "no tasks", nil
	}
	var sb strings.Builder
	for _, t := range tasks {
		sb.WriteString(t.ID + " [" + t.Status + "] " + t.Title)
		if t.Assignee != "" {
			sb.WriteString(" (@" + t.Assignee + ")")
		}
		sb.WriteString("\n")
	}
	return sb.String(), nil
}

func (p *ToolProvider) runUpdate(ctx context.Context, args map[string]any) (string, error) {
	id := str(args["id"])
	patch := agent.KanbanPatch{
		Status: str(args["status"]),
		Title:  optStr(args["title"]),
		Body:   optStr(args["body"]),
	}
	if assignee, ok := args["assignee"]; ok {
		patch.Assignee = optStr(assignee)
	}
	out, err := p.board.UpdateTask(ctx, id, patch)
	if err != nil {
		return "", err
	}
	return "updated " + out.ID + " [" + out.Status + "] " + out.Title, nil
}

func (p *ToolProvider) runComment(ctx context.Context, args map[string]any) (string, error) {
	author := str(args["author"])
	if author == "" {
		author = "agent"
	}
	c, err := p.board.AddComment(ctx, str(args["id"]), author, str(args["body"]))
	if err != nil {
		return "", err
	}
	return "commented on " + c.TaskID, nil
}

func (p *ToolProvider) runComplete(ctx context.Context, args map[string]any) (string, error) {
	id := str(args["id"])
	out, err := p.board.UpdateTask(ctx, id, agent.KanbanPatch{Status: StatusDone})
	if err != nil {
		return "", err
	}
	return "done " + out.ID + ": " + out.Title, nil
}

func (p *ToolProvider) runLink(ctx context.Context, args map[string]any) (string, error) {
	id := str(args["id"])
	sess := str(args["session_id"])
	if sess == "" {
		sess = agent.SessionIDFromContext(ctx)
	}
	if sess == "" {
		return "", fmt.Errorf("no session id: pass session_id")
	}
	sptr := sess
	out, err := p.board.UpdateTask(ctx, id, agent.KanbanPatch{SessionID: &sptr})
	if err != nil {
		return "", err
	}
	return "linked " + out.ID + " to session " + sess, nil
}

// str reads a tool arg that is always present.
func str(v any) string {
	s, _ := v.(string)
	return s
}

// optStr is str, but distinguishes "arg absent" (nil) from "arg given".
func optStr(v any) *string {
	s, ok := v.(string)
	if !ok {
		return nil
	}
	return &s
}