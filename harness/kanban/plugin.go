package kanban

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"sync"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/datadir"
)

// Plugin adapts the kanban Store into omega's plugin system.
type Plugin struct {
	store   *Store
	tools   *ToolProvider
	capsule *capsule
}

// Compile-time assertions: *Plugin is a Plugin, *Store a KanbanProvider.
var _ agent.Plugin = (*Plugin)(nil)

// NewPlugin returns a kanban Plugin instance.
func NewPlugin() *Plugin { return &Plugin{} }

// Store returns the mounted board store (the host and the proof
// surfaces read through it; nil before Mount).
func (p *Plugin) Store() *Store { return p.store }

func (p *Plugin) Name() string { return "kanban" }

// Description implements agent.Describer.
func (p *Plugin) Description() string {
	return "kanban: the durable task board agents and workers coordinate through"
}

// ConfigKey names the config section this plugin reads from ctx.Configs.
func (p *Plugin) ConfigKey() string { return "kanban" }

// Provides stays the declared data the census reads.
func (p *Plugin) Provides() []string { return []string{"kanban", "tools"} }

// Requires: none. The finished-things dependency on the notifications
// seam moved to the emitter registration (below) - the hook receives
// the mounted seam at mount end, nil-safe, so the classic Requires
// edge retired with the mount-time capture (v0.12 phase C1).
func (p *Plugin) Requires() []string { return nil }

// Dismount releases the mounted board store: beyond-Context state the journal cannot.
// Nil-safe: never mounted (or already dismounted) closes nothing.
func (p *Plugin) Dismount(ctx *core.Context) error {
	if p.store == nil {
		return nil
	}
	err := p.store.Close()
	p.store = nil
	return err
}

// MountWithAPI implements agent.APIPlugin. A mount failure (bad dsn, unreadable path, a
// migration that failed its count verification) is a startup error - a board that
// silently does not exist is worse than a crash.
func (p *Plugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	cfg := Default()
	if c, ok := ctx.Configs["kanban"].(Config); ok {
		cfg = c
	}
	dsn := cfg.DBPath
	home, aerr := datadir.AgentHome()
	if aerr != nil {
		return fmt.Errorf("kanban board registry: %w", aerr)
	}
	if home != "" {
		if dsn == "" || dsn == "kanban.db" || registryStaleDSN(home, dsn) {
			dsn = datadir.DBPath(home, "kanban.db")
		}
	} else if dsn == "" || dsn == "kanban.db" {
		return fmt.Errorf("kanban board registry: omega home not determinable - set OMEGA_HOME")
	}
	s, err := MountRegistryDSN(home, dsn)
	if err != nil {
		return err
	}
	if p.store != nil {
		_ = p.store.Close()
	}
	p.store = s
	p.tools = &ToolProvider{board: s}
	p.capsule = newCapsule(s, p.tools.sessionBoard, p.setSessionBoard)
	p.tools.capsule = p.capsule
	for _, step := range []func() error{
		func() error { return p.registerRoutes(api) },
		func() error {
			for toolName, tool := range p.tools.Tools() {
				if err := api.RegisterTool(toolName, tool); err != nil {
					return err
				}
			}
			return nil
		},
		func() error { return api.RegisterPanelProvider(p.capsule) },
		func() error { return p.registerCommands(api) },
		// CLI homecoming (v0.12 phase D, Q6).
		func() error { return api.RegisterSubcommand("kanban", kanbanSummary, cmdKanban) },
		// done transitions report to the finished- things store. The hook hands the mounted
		// notifications seam over at mount end, nil when the module is absent (nil is legal -
		// the emitter is nil-safe by the classic law).
		func() error {
			store := s
			return api.RegisterEmitter(func(svc any) {
				n, _ := svc.(finishedThings)
				store.SetNotifier(n)
			})
		},
	} {
		if err := step(); err != nil {
			_ = s.Close()
			return err
		}
	}
	return nil
}

// registerCommands registers the /board and /boards slash commands through the API.
func (p *Plugin) registerCommands(api *agent.ExtensionAPI) error {
	if err := api.RegisterCommand("/board", "show the active board, or switch it (/board [name])", nil,
		func(c context.Context, name, args string) (core.CommandResult, error) {
			if name != "/board" {
				return core.CommandResult{}, core.CommandMiss(name)
			}
			return p.handleCommand(name, args)
		}); err != nil {
		return err
	}
	return api.RegisterCommand("/boards", "list boards and the current pointer", nil,
		func(c context.Context, name, args string) (core.CommandResult, error) {
			if name != "/boards" {
				return core.CommandResult{}, core.CommandMiss(name)
			}
			return p.handleCommand(name, args)
		})
}

// handleCommand answers /boards (list + pointer) and /board.
func (p *Plugin) handleCommand(name, args string) (core.CommandResult, error) {
	switch name {
	case "/boards":
		boards, err := p.store.Boards(context.Background())
		if err != nil {
			return core.CommandResult{}, err
		}
		current := p.sessionBoard()
		var sb strings.Builder
		for _, b := range boards {
			marker := " "
			switch {
			case b.Name == current:
				marker = "*"
			case b.Current:
				marker = "="
			}
			fmt.Fprintf(&sb, "%s %s\n", marker, b.Name)
		}
		sb.WriteString("\n* active in this session   = pointer target\n")
		return core.CommandResult{Text: sb.String()}, nil
	case "/board":
		rest := strings.TrimSpace(args)
		if rest == "" {
			return core.CommandResult{Text: "board: " + p.sessionBoard() + " (/board <name> to switch, /board main for the default)"}, nil
		}
		if rest != DefaultBoardName && !ValidBoardName(rest) {
			return core.CommandResult{}, fmt.Errorf("illegal board name %q", rest)
		}
		if !p.store.BoardExists(rest) {
			return core.CommandResult{}, fmt.Errorf("no such board %q", rest)
		}
		p.setSessionBoard(rest)
		// the switch moved the active marker AND the covered board.
		if p.capsule != nil {
			p.capsule.refreshAll()
		}
		return core.CommandResult{Text: "board: " + rest}, nil
	}
	return core.CommandResult{}, core.CommandMiss(name)
}

// session-scoped active board.

func (p *Plugin) sessionBoard() string {
	if p.tools == nil {
		return ""
	}
	p.tools.mu.Lock()
	defer p.tools.mu.Unlock()
	return p.tools.active
}

func (p *Plugin) setSessionBoard(name string) {
	p.tools.mu.Lock()
	p.tools.active = name
	p.tools.mu.Unlock()
}

// ToolProvider exposes the board as agent tools.
type ToolProvider struct {
	board KanbanProvider

	// capsule is the module's canvas surface (panel.go, arc 5). Nil-safe: a nil capsule
	// silently ships no push.
	capsule *capsule

	mu     sync.Mutex
	active string
}

// sessionBoard is the read side for the runTask board resolution.
func (p *ToolProvider) sessionBoard() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.active
}

// taskVerbs is the kanban verb set: the batch contract's per-op action enum derives.
// The registry unification B rule split the flat action-protocol tool into
// kanban.<verb> wire tools - dotted names are the norm on this registry (process.*,
// delegate.*, web.*); kanban.batch keeps the per-op action contract for the operations
// array.
var taskVerbs = []string{"create", "list", "update", "complete", "comment", "delete"}

// Tools returns the kanban wire: seven dotted tools, one per verb.
func (p *ToolProvider) Tools() map[string]core.Tool {
	board := prop("string", "Board name to operate on (omit = the active board; 'main' is the default board).")
	return map[string]core.Tool{
		"kanban.list": {
			Description: "List board tasks, optionally filtered by status. An empty call lists the active board's tasks.",
			Parameters: object(map[string]any{
				"board":  board,
				"status": prop("string", "Filter by status: omit = active only (archived hidden), 'archived' lists the archive, 'all' includes it."),
			}),
			Run: p.verbRunner("list"),
		},
		"kanban.create": {
			Description: "Create a board task (title is the short name, body carries the full spec; priority higher = more urgent, 0 = normal).",
			Parameters: object(map[string]any{
				"title":    prop("string", "Short title for the task."),
				"body":     prop("string", "Full spec / body."),
				"assignee": prop("string", "Who the task is for."),
				"priority": prop("number", "Urgency tiebreak, 0 = normal (default), higher = more urgent; negative refused."),
				"status":   prop("string", "Initial board status; omit for triage (the store default)."),
				"source":   prop("string", "Provenance tag, e.g. an automator's name."),
				"board":    board,
			}, "title"),
			Run: p.verbRunner("create"),
		},
		"kanban.update": {
			Description: "Update a task: move status (any legal transition), set assignee, retitle, rewrite the body, or set priority.",
			Parameters: object(map[string]any{
				"id":       prop("string", "Task id."),
				"title":    prop("string", "New title."),
				"body":     prop("string", "New body."),
				"assignee": prop("string", "New assignee; empty string unassigns."),
				"priority": prop("number", "New priority (0 = normal; negative refused)."),
				"status":   prop("string", "New board status (any legal transition): triage todo scheduled ready running blocked review done archived."),
				"board":    board,
			}, "id"),
			Run: p.verbRunner("update"),
		},
		"kanban.complete": {
			Description: "Mark a task done (moves it to the done column and stamps completion).",
			Parameters: object(map[string]any{
				"id":    prop("string", "Task id."),
				"board": board,
			}, "id"),
			Run: p.verbRunner("complete"),
		},
		"kanban.comment": {
			Description: "Add a comment to a task's thread.",
			Parameters: object(map[string]any{
				"id":     prop("string", "Task id."),
				"body":   prop("string", "Comment body."),
				"author": prop("string", "Comment author (default agent)."),
				"board":  board,
			}, "id", "body"),
			Run: p.verbRunner("comment"),
		},
		"kanban.delete": {
			Description: "Permanently remove a task from the board (its comments and events go with it). Archive is the reversible path - prefer it unless the card must vanish.",
			Parameters: object(map[string]any{
				"id":    prop("string", "Task id."),
				"board": board,
			}, "id"),
			Run: p.verbRunner("delete"),
		},
		"kanban.batch": {
			Description: "Run several board operations in one call (create several tasks, or update + comment together). On the mounted store the whole array is atomic: everything applies or everything reverts.",
			Parameters: object(map[string]any{
				"operations": map[string]any{
					"type":        "array",
					"description": "Array of {action, ...args} operations, applied in order inside one transaction - everything applies or everything reverts (a failed batch rolls back the operations it already applied; no orphaned creates). In-batch referencing: id \"LAST\" names the task the most recent create made, \"#N\" (1-based) names the task created at operations[N-1], matching the numbered result lines. Each operation resolves its own board; omit = the active board.",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"action": map[string]any{"type": "string", "enum": taskVerbs},
						},
						"required": []string{"action"},
					},
				},
			}, "operations"),
			Run: p.runBatchTool,
		},
	}
}

// prop assembles one typed schema property.
func prop(typ, desc string) map[string]any {
	return map[string]any{"type": typ, "description": desc}
}

// object assembles the tool's JSON object schema. required names the args THIS verb
// cannot run without - its own list, never another verb's (comment requires body,
// create requires title); a verb with no required args (list - the empty call by
// construction) omits the key entirely.
func object(props map[string]any, required ...string) map[string]any {
	schema := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

// parseTaskOperations converts the raw interface{} from tool args into ordered.
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
	legal := make(map[string]bool, len(taskVerbs))
	for _, v := range taskVerbs {
		legal[v] = true
	}
	for i, op := range ops {
		act, _ := op["action"].(string)
		if !legal[act] {
			return nil, fmt.Errorf("operations[%d]: action must be one of %v, got %q", i, taskVerbs, act)
		}
	}
	return ops, nil
}

// runBatchTool is kanban.batch's Run: parse the operations array (its own per-op action
// contract), then hand it to the batch engine. The batch is atomic on a Batcher board
// (one transaction, create-then-act referenced in-batch; batch.go) and fail-forward,
// honestly reported, where the board cannot provide one.
func (p *ToolProvider) runBatchTool(ctx context.Context, args map[string]any) (string, error) {
	ops, err := parseTaskOperations(args["operations"])
	if err != nil {
		return "", err
	}
	return p.runTaskBatch(ctx, ops)
}

// verbRunner binds one verb of the dotted surface.
func (p *ToolProvider) verbRunner(action string) func(context.Context, map[string]any) (string, error) {
	return func(ctx context.Context, args map[string]any) (string, error) {
		a := maps.Clone(args)
		if a == nil {
			a = map[string]any{}
		}
		a["action"] = action
		res, _, err := p.runTaskOp(ctx, p.board, a)
		if err == nil {
			p.refreshCapsule()
		}
		return res, err
	}
}

// refreshCapsule pushes the task panel through the module's canvas surface.
// Nil-safe: the capsule is nil only when Mount was never reached - and then nothing
// pushed.
func (p *ToolProvider) refreshCapsule() {
	if p.capsule == nil {
		return
	}
	p.capsule.refreshTasks()
}

// runTaskOp executes one task operation by its action on the provider b.
func (p *ToolProvider) runTaskOp(ctx context.Context, b KanbanProvider, args map[string]any) (string, string, error) {
	action, _ := args["action"].(string)
	board := str(args["board"])
	if board == "" {
		board = p.sessionBoard()
	}
	switch action {
	case "create":
		return p.runCreate(ctx, b, board, args)
	case "list":
		res, err := p.runList(ctx, b, board, args)
		return res, "", err
	case "update":
		res, err := p.runUpdate(ctx, b, board, args)
		return res, "", err
	case "complete":
		res, err := p.runComplete(ctx, b, board, args)
		return res, "", err
	case "comment":
		res, err := p.runComment(ctx, b, board, args)
		return res, "", err
	case "delete":
		res, err := p.runDelete(ctx, b, board, args)
		return res, "", err
	default:
		return "", "", fmt.Errorf("unknown task action %q (one of %v)", action, taskVerbs)
	}
}

func (p *ToolProvider) runCreate(ctx context.Context, b KanbanProvider, board string, args map[string]any) (string, string, error) {
	in := KanbanTask{
		Title:     str(args["title"]),
		Body:      str(args["body"]),
		Assignee:  str(args["assignee"]),
		Priority:  intArg(args["priority"]),
		Status:    str(args["status"]),
		Source:    str(args["source"]),
		SessionID: agent.SessionIDFromContext(ctx),
	}
	out, err := b.CreateTask(ctx, board, in)
	if err != nil {
		return "", "", err
	}
	return "created " + out.ID + " [" + out.Status + "] " + out.Title, out.ID, nil
}

func (p *ToolProvider) runList(ctx context.Context, b KanbanProvider, board string, args map[string]any) (string, error) {
	tasks, err := b.ListTasks(ctx, board, str(args["status"]))
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

func (p *ToolProvider) runUpdate(ctx context.Context, b KanbanProvider, board string, args map[string]any) (string, error) {
	id := str(args["id"])
	patch := KanbanPatch{
		Status:   str(args["status"]),
		Title:    optStr(args["title"]),
		Body:     optStr(args["body"]),
		Priority: optInt(args["priority"]),
	}
	if assignee, ok := args["assignee"]; ok {
		patch.Assignee = optStr(assignee)
	}
	out, err := b.UpdateTask(ctx, board, id, patch)
	if err != nil {
		return "", err
	}
	return "updated " + out.ID + " [" + out.Status + "] " + out.Title, nil
}

func (p *ToolProvider) runComment(ctx context.Context, b KanbanProvider, board string, args map[string]any) (string, error) {
	author := str(args["author"])
	if author == "" {
		author = "agent"
	}
	c, err := b.AddComment(ctx, board, str(args["id"]), author, str(args["body"]))
	if err != nil {
		return "", err
	}
	return "commented on " + c.TaskID, nil
}

func (p *ToolProvider) runComplete(ctx context.Context, b KanbanProvider, board string, args map[string]any) (string, error) {
	id := str(args["id"])
	out, err := b.UpdateTask(ctx, board, id, KanbanPatch{Status: StatusDone})
	if err != nil {
		return "", err
	}
	return "done " + out.ID + ": " + out.Title, nil
}

func (p *ToolProvider) runDelete(ctx context.Context, b KanbanProvider, board string, args map[string]any) (string, error) {
	bd, ok := b.(TaskDeleter)
	if !ok {
		return "", fmt.Errorf("this board cannot delete tasks - archive instead (update status archived)")
	}
	id := str(args["id"])
	if err := bd.DeleteTask(ctx, board, id); err != nil {
		return "", err
	}
	return "deleted " + id, nil
}

// str reads a tool arg that is always present.
func str(v any) string {
	s, _ := v.(string)
	return s
}

// intArg is str for numbers.
func intArg(v any) int {
	f, ok := v.(float64)
	if !ok {
		return 0
	}
	return int(f)
}

// optInt is intArg, but distinguishes "arg absent" (nil) from "arg given".
func optInt(v any) *int {
	f, ok := v.(float64)
	if !ok {
		return nil
	}
	n := int(f)
	return &n
}

// optStr is str, but distinguishes "arg absent" (nil) from "arg given".
func optStr(v any) *string {
	s, ok := v.(string)
	if !ok {
		return nil
	}
	return &s
}

// Toggleable marks this capsule as a module capsule (v0.12 E1, the tier law).
func (p *Plugin) Toggleable() bool { return true }
