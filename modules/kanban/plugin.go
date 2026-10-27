package kanban

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/modules/datadir"
)

// Plugin adapts the kanban Store into omega's plugin system. It owns
// the exclusive "kanban" seam (the board provider the HTTP channel
// serves /tasks from) and contributes the board tools so the agent
// manages the board natively in chat. The canvas panels (arc 5,
// t_7bd3dc06) ride the same Mount - the registration IS the plugin
// (see capsule, panel.go). It mounts through the ExtensionAPI
// exclusively (v0.12 phase C1; the classic Mount body is retired to
// the fail-loud stub below - docs/migrating-a-module.md is the recipe
// this migration produced).
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

// ConfigKey names the config section this plugin reads from
// ctx.Configs (see agent.Describer). Empty = reads no section.
func (p *Plugin) ConfigKey() string { return "kanban" }

// Provides stays the declared data the census reads: the exclusive
// kanban seam and the tools surface (the registration vocabulary
// resolves the seams; the sweep claims them the same as ever).
func (p *Plugin) Provides() []string { return []string{"kanban", "tools"} }

// Requires: none. The finished-things dependency on the notifications
// seam moved to the emitter registration (below) - the hook receives
// the mounted seam at mount end, nil-safe, so the classic Requires
// edge retired with the mount-time capture (v0.12 phase C1).
func (p *Plugin) Requires() []string { return nil }

// MountWithAPI implements agent.APIPlugin. The store opens here (same
// resolution the classic body ran); every face registers through the
// api: the kanban seam, the folded task tool, the live panel capsule,
// the /board and /boards commands, and the finished-things emitter.
// A mount failure (bad dsn, unreadable path, corrupt pointer) is a
// startup error - a board that silently does not exist is worse than
// a crash.
func (p *Plugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	cfg := Default()
	if c, ok := ctx.Configs["kanban"].(Config); ok {
		cfg = c
	}
	dsn := cfg.DBPath
	name := ""
	var boardsNamed bool
	ahome, aerr := datadir.AgentHome()
	if aerr != nil {
		return fmt.Errorf("kanban board registry: %w", aerr)
	}
	if home := ahome; home != "" {
		pointer, err := ReadPointer(home)
		if err != nil {
			return fmt.Errorf("kanban board registry: %w", err)
		}
		if dsn == "" || dsn == "kanban.db" {
			// unresolved default: the pointer picks the board
			resolved, derr := BoardDSN(home, pointer)
			if derr != nil {
				return fmt.Errorf("kanban board registry: %w", derr)
			}
			dsn = resolved
			name = pointer
		} else if resolved, derr := BoardDSN(home, pointer); derr == nil && resolved == dsn {
			// host pre-resolved through the pointer: keep the name
			name = pointer
		}
		boardsNamed = true
	}
	s, oerr := Open(dsn)
	if oerr != nil {
		return fmt.Errorf("kanban store: %w", oerr)
	}
	p.store = s
	if boardsNamed {
		home := ahome
		s.ConfigureBoards(BoardsDir(home), name, datadir.DBPath(home, "kanban.db"))
	}
	p.tools = &ToolProvider{board: s}
	// Capsule law (arc 5, t_7bd3dc06): the module ships its view-model
	// with its store. The task panel and the board switcher ride the
	// same Mount - an unregistered plugin ships no store, no tools, no
	// commands, no panels. The session board is the tools' active
	// board: the panels mirror the one truth the tools and /board
	// already share, and the swap hands the switcher its /board
	// equivalence.
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
		// CLI homecoming (v0.12 phase D, Q6): the omega kanban argv
		// lives here now - the register step is the thin name+handler
		// prefix, this capsule parses its own argv.
		func() error { return api.RegisterSubcommand("kanban", kanbanSummary, cmdKanban) },
		// card t_5d9032d3: done transitions report to the finished-
		// things store. The hook hands the mounted notifications seam
		// over at mount end, nil when the module is absent (nil is
		// legal - the emitter is nil-safe by the classic law).
		func() error {
			store := s
			return api.RegisterEmitter(func(n core.NotificationsProvider) { store.SetNotifier(n) })
		},
	} {
		if err := step(); err != nil {
			_ = s.Close()
			return err
		}
	}
	return nil
}

// registerCommands registers the /board and /boards slash commands
// through the API chain: one registration per command, each handler
// answering only its own name - the wrapper's chain-on-miss cascade
// produces the same fall-through the classic single wrapper had.
func (p *Plugin) registerCommands(api *agent.ExtensionAPI) error {
	if err := api.RegisterCommand("/board", "show the active board, or switch it (/board [name])", nil,
		func(c context.Context, name, args string) (core.CommandResult, error) {
			if name != "/board" {
				return core.CommandResult{}, fmt.Errorf("no handler for %s", name)
			}
			return p.handleCommand(name, args)
		}); err != nil {
		return err
	}
	return api.RegisterCommand("/boards", "list boards and the current pointer", nil,
		func(c context.Context, name, args string) (core.CommandResult, error) {
			if name != "/boards" {
				return core.CommandResult{}, fmt.Errorf("no handler for %s", name)
			}
			return p.handleCommand(name, args)
		})
}

// handleCommand answers /boards (list + pointer) and /board (show or
// switch the session's active board). The switch is session-local:
// the task tools target the set board until /board <name> or
// /board main resets it. It deliberately does NOT move the global
// pointer - that is the registry op (omega kanban board use),
// explicit by design.
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
		if _, err := p.store.handleFor(rest); err != nil {
			return core.CommandResult{}, err
		}
		p.setSessionBoard(rest)
		// the switch moved the active marker AND the covered board:
		// both canvas panels follow (arc 5, the switcher's wiring)
		if p.capsule != nil {
			p.capsule.refreshAll()
		}
		return core.CommandResult{Text: "board: " + rest}, nil
	}
	return core.CommandResult{}, fmt.Errorf("no handler for %s", name)
}

// --- session-scoped active board (small, mutexed; the TUI command
// handler and agent tool calls run on different goroutines)

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

// ToolProvider exposes the board as agent tools. The tools take the
// provider seam instead of *Store so embedders can swap implementations.
type ToolProvider struct {
	board KanbanProvider

	// capsule is the module's canvas surface (panel.go, arc 5). Set at
	// Mount; every tool mutation refreshes the task panel through it.
	// Nil-safe: a nil capsule silently ships no push.
	capsule *capsule

	// active names the board the tools hit when a call omits board.
	// "" = the default board (pointer target at mount). Mutated by the
	// /board command; read by tool calls - mutexed.
	mu     sync.Mutex
	active string
}

// sessionBoard is the read side for the runTask board resolution.
func (p *ToolProvider) sessionBoard() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.active
}

// taskActions is the structured sub-action metadata for the
// consolidated task tool: one source of truth for the schema enum, the
// prompt, and the /tools inventory.
var taskActions = []core.ToolAction{
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

// Tools returns the board tools. The six dotted verbs were
// consolidated into one "kanban.task" domain tool with an action enum
// (derived from taskActions) plus a batch operations array; the
// registry carries the one namespaced name (no backwards
// compatibility - the alias shim era closed with the mount migration,
// whose machine-facing identity is the module.verb namespace pair).
func (p *ToolProvider) Tools() map[string]core.Tool {
	statusDesc := "Board status: one of triage todo scheduled ready running blocked review done archived (create: initial status; update: new status; list: filter)."
	tools := map[string]core.Tool{
		"kanban.task": {
			Description: "One tool for the kanban board: create, list, update, complete, comment on, and link tasks. Board: pass board to operate on a named board; omit to use the active board (/board). Batch: pass operations to run several in one call (create several tasks, or update + comment together).",
			Actions:     taskActions,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action": map[string]any{
						"type":        "string",
						"description": "Which board operation to run.",
					},
					"board":      map[string]any{"type": "string", "description": "Board name to operate on (omit = the active board; 'main' is the default board)."},
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
				// earlier ops still applied: the panel refresh below
				// is exactly the catch-up the partial batch needs
				p.refreshCapsule()
				return sb.String() + fmt.Sprintf("failed at operations[%d]: %v", i, err), nil
			}
			fmt.Fprintf(&sb, "%d: %s\n", i+1, res)
		}
		p.refreshCapsule()
		fmt.Fprintf(&sb, "%d operations applied", len(ops))
		return sb.String(), nil
	}
	res, err := p.runTaskOp(ctx, args)
	if err == nil {
		p.refreshCapsule()
	}
	return res, err
}

// refreshCapsule pushes the task panel through the module's canvas
// surface after a tool mutation (arc 5). Nil-safe: the capsule is
// nil only when Mount was never reached - and then nothing pushed.
func (p *ToolProvider) refreshCapsule() {
	if p.capsule == nil {
		return
	}
	p.capsule.refreshTasks()
}

// runTaskOp executes one task operation by its action. Every op
// resolves its board the same way: explicit board arg, else the
// session's active board, else "" (the default board). There is no
// cross-board op by design - one op touches one board.
func (p *ToolProvider) runTaskOp(ctx context.Context, args map[string]any) (string, error) {
	action, _ := args["action"].(string)
	board := str(args["board"])
	if board == "" {
		board = p.sessionBoard()
	}
	switch action {
	case "create":
		return p.runCreate(ctx, board, args)
	case "list":
		return p.runList(ctx, board, args)
	case "update":
		return p.runUpdate(ctx, board, args)
	case "complete":
		return p.runComplete(ctx, board, args)
	case "comment":
		return p.runComment(ctx, board, args)
	case "link_session":
		return p.runLink(ctx, board, args)
	default:
		return "", fmt.Errorf("action is required: one of %v, or pass operations for a batch", taskActionNames())
	}
}

func (p *ToolProvider) runCreate(ctx context.Context, board string, args map[string]any) (string, error) {
	in := KanbanTask{
		Title:     str(args["title"]),
		Body:      str(args["body"]),
		Assignee:  str(args["assignee"]),
		Status:    str(args["status"]),
		Source:    str(args["source"]),
		SessionID: agent.SessionIDFromContext(ctx),
	}
	out, err := p.board.CreateTask(ctx, board, in)
	if err != nil {
		return "", err
	}
	return "created " + out.ID + " [" + out.Status + "] " + out.Title, nil
}

func (p *ToolProvider) runList(ctx context.Context, board string, args map[string]any) (string, error) {
	tasks, err := p.board.ListTasks(ctx, board, str(args["status"]))
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

func (p *ToolProvider) runUpdate(ctx context.Context, board string, args map[string]any) (string, error) {
	id := str(args["id"])
	patch := KanbanPatch{
		Status: str(args["status"]),
		Title:  optStr(args["title"]),
		Body:   optStr(args["body"]),
	}
	if assignee, ok := args["assignee"]; ok {
		patch.Assignee = optStr(assignee)
	}
	out, err := p.board.UpdateTask(ctx, board, id, patch)
	if err != nil {
		return "", err
	}
	return "updated " + out.ID + " [" + out.Status + "] " + out.Title, nil
}

func (p *ToolProvider) runComment(ctx context.Context, board string, args map[string]any) (string, error) {
	author := str(args["author"])
	if author == "" {
		author = "agent"
	}
	c, err := p.board.AddComment(ctx, board, str(args["id"]), author, str(args["body"]))
	if err != nil {
		return "", err
	}
	return "commented on " + c.TaskID, nil
}

func (p *ToolProvider) runComplete(ctx context.Context, board string, args map[string]any) (string, error) {
	id := str(args["id"])
	out, err := p.board.UpdateTask(ctx, board, id, KanbanPatch{Status: StatusDone})
	if err != nil {
		return "", err
	}
	return "done " + out.ID + ": " + out.Title, nil
}

func (p *ToolProvider) runLink(ctx context.Context, board string, args map[string]any) (string, error) {
	id := str(args["id"])
	sess := str(args["session_id"])
	if sess == "" {
		sess = agent.SessionIDFromContext(ctx)
	}
	if sess == "" {
		return "", fmt.Errorf("no session id: pass session_id")
	}
	sptr := sess
	out, err := p.board.UpdateTask(ctx, board, id, KanbanPatch{SessionID: &sptr})
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

// Toggleable marks this capsule as a module capsule (v0.12 E1, the
// tier law): the omega.json disable list may name it.
func (p *Plugin) Toggleable() bool { return true }
