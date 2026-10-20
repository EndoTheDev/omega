package kanban

import (
	"context"
	"encoding/json"
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

// ToolProvider exposes the board as agent tools. The tools take the
// provider seam instead of *Store so embedders can swap implementations.
type ToolProvider struct {
	board agent.KanbanProvider
}

// taskActions is the structured sub-action metadata for the
// consolidated task tool: one source of truth for the schema enum, the
// prompt, and the /tools inventory.
var taskActions = []agent.ToolAction{
	{Name: "create", Description: "Create a board task (title is the short name, body carries the full spec)."},
	{Name: "list", Description: "List board tasks, optionally filtered by status."},
	{Name: "update", Description: "Update a task: move status (any legal transition), set assignee, retitle, or rewrite the body."},
	{Name: "complete", Description: "Mark a task done (moves it to the done column and stamps completion)."},
	{Name: "comment", Description: "Add a comment to a task's thread."},
	{Name: "link_session", Description: "Link a task to the conversation it belongs to (defaults to the current session)."},
}

// taskActionNames returns the action names from the metadata.
func taskActionNames() []string {
	names := make([]string, len(taskActions))
	for i, a := range taskActions {
		names[i] = a.Name
	}
	return names
}

// legacyTaskNames maps folded actions back to the pre-fold dotted tool
// names (the alias shim set).
var legacyTaskNames = map[string]string{
	"create":       "task.create",
	"list":         "task.list",
	"update":       "task.update",
	"complete":     "task.complete",
	"comment":      "task.comment",
	"link_session": "task.link_session",
}

// Tools returns the board tools. The six dotted verbs are consolidated
// into one "task" domain tool with an action enum (derived from
// taskActions) plus a batch operations array; the legacy task.* names
// stay registered as alias shims for one release.
func (p *ToolProvider) Tools() map[string]agent.Tool {
	statusDesc := "Board status: one of triage todo scheduled ready running blocked review done archived (create: initial status; update: new status; list: filter)."
	tools := map[string]agent.Tool{
		"task": {
			Description: "One tool for the kanban board: create, list, update, complete, comment on, and link tasks. Batch: pass operations to run several in one call (create several tasks, or update + comment together).",
			Actions:     taskActions,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action": map[string]any{
						"type":        "string",
						"description": "Which board operation to run.",
					},
					"id":         map[string]any{"type": "string", "description": "Task id (update, complete, comment, link_session; batch ops carry their own)."},
					"title":      map[string]any{"type": "string", "description": "Short title (create); new title (update)."},
					"body":       map[string]any{"type": "string", "description": "Full spec / body (create); new body (update); comment body (comment, required)."},
					"assignee":   map[string]any{"type": "string", "description": "Who the task is for (create); new assignee, empty string unassigns (update)."},
					"status":     map[string]any{"type": "string", "description": statusDesc},
					"source":     map[string]any{"type": "string", "description": "Provenance tag, e.g. an automator's name (create)."},
					"author":     map[string]any{"type": "string", "description": "Comment author (comment, default agent)."},
					"session_id": map[string]any{"type": "string", "description": "Session id (link_session, default current session)."},
					"operations": map[string]any{
						"type":        "array",
						"description": "Batch: array of {action, ...args} operations, applied in order. If one fails, later operations are not attempted.",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"action": map[string]any{"type": "string", "enum": taskActionNames()},
							},
							"required": []string{"action"},
						},
					},
				},
				"required": []string{"action"},
			},
			Run: p.runTask,
		},
	}
	for _, a := range taskActions {
		legacy := legacyTaskNames[a.Name]
		if legacy == "" {
			continue
		}
		action := a.Name
		tools[legacy] = agent.Tool{
			Description: fmt.Sprintf("%s (alias for task with action %q)", a.Description, action),
			AliasOf:     "task",
			Run: func(ctx context.Context, args map[string]any) (string, error) {
				args["action"] = action
				return p.runTask(ctx, args)
			},
		}
	}
	return tools
}

// parseTaskOperations converts the raw interface{} from tool args into
// ordered single-op arg maps, validating the action enum.
func parseTaskOperations(raw any) ([]map[string]any, error) {
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid operations: %v", err)
	}
	var ops []map[string]any
	if err := json.Unmarshal(data, &ops); err != nil {
		return nil, fmt.Errorf("invalid operations: %v", err)
	}
	if len(ops) == 0 {
		return nil, fmt.Errorf("operations array is empty")
	}
	legal := make(map[string]bool, len(taskActions))
	for _, a := range taskActions {
		legal[a.Name] = true
	}
	for i, op := range ops {
		act, _ := op["action"].(string)
		if !legal[act] {
			return nil, fmt.Errorf("operations[%d]: action must be one of %v, got %q", i, taskActionNames(), act)
		}
	}
	return ops, nil
}

// runTask dispatches the consolidated task tool: a single operation or
// a batch. The batch is fail-forward (the board is a DB, not a file
// store - there is no snapshot to restore; ops stop at the first
// failure and the result reports which ops already applied).
func (p *ToolProvider) runTask(ctx context.Context, args map[string]any) (string, error) {
	if opsRaw, ok := args["operations"]; ok && opsRaw != nil {
		ops, err := parseTaskOperations(opsRaw)
		if err != nil {
			return "", err
		}
		var sb strings.Builder
		for i, op := range ops {
			res, err := p.runTaskOp(ctx, op)
			if err != nil {
				return sb.String() + fmt.Sprintf("failed at operations[%d]: %v", i, err), nil
			}
			fmt.Fprintf(&sb, "%d: %s\n", i+1, res)
		}
		fmt.Fprintf(&sb, "%d operations applied", len(ops))
		return sb.String(), nil
	}
	return p.runTaskOp(ctx, args)
}

// runTaskOp executes one task operation by its action.
func (p *ToolProvider) runTaskOp(ctx context.Context, args map[string]any) (string, error) {
	action, _ := args["action"].(string)
	switch action {
	case "create":
		return p.runCreate(ctx, args)
	case "list":
		return p.runList(ctx, args)
	case "update":
		return p.runUpdate(ctx, args)
	case "complete":
		return p.runComplete(ctx, args)
	case "comment":
		return p.runComment(ctx, args)
	case "link_session":
		return p.runLink(ctx, args)
	default:
		return "", fmt.Errorf("action is required: one of %v, or pass operations for a batch", taskActionNames())
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