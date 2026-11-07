package kanban

// The board registry CLI: the kanban subcommand's home (v0.12 phase D,
// grill Q6 - module owns its argv). The capsule registers its own name
// + handler at mount (RegisterSubcommand); the binary routes
// `omega kanban ...` here with the untouched argv and parses nothing.
// One board per workspace needs named boards plus a current-board
// flag - one kanban.db owns every board (t_feae19d3): the boards table
// is the registry, is_current is the pointer. Card operations target
// one board per call, the current board selects which board an omitted
// --board means.

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/EndoTheDev/omega/harness/datadir"
)

// kanbanSummary is the one-line entry the global `omega --help` list
// shows for this capsule (the help listing is registry introspection).
const kanbanSummary = "the task board registry (see: omega kanban --help)"

// kanbanHelp documents the kanban subcommand tree (the module's own
// help: `omega kanban --help` prints it).
const kanbanHelp = `omega kanban - the task board registry

Usage:
  omega kanban board list                  all boards, task counts, current marker
  omega kanban board create <name>         create an empty board
  omega kanban board delete <name>         delete a board (refuses non-empty)
  omega kanban board use <name>            point the home at the board
  omega kanban board current               print the current board
  omega kanban list [--board <name>] [--status <status>]
                                           list cards (default: current board)
  omega kanban create [--board <name>] [--status <status>] <title>
                                           create one card
  omega kanban done [--board <name>] <id>  complete one card

Flags:
  --board <name>   operate on a named board (omit: the current board;
                   'main' is the default board)
  --status <s>     card list filter (triage todo scheduled ready running
                   blocked review done archived; default lists active
                   cards only - archived with --status archived,
                   everything with --status all)
`

// cliHome resolves the omega home for the CLI handlers. The binary
// exports OMEGA_HOME before dispatch (cmd/omega run), so this resolves
// the same home the mount did; a bare handle (no env, no user home)
// fails loud instead of guessing.
func cliHome() (string, error) {
	home, err := datadir.AgentHome()
	if err != nil {
		return "", err
	}
	if home == "" {
		return "", fmt.Errorf("kanban: omega home not determinable - set OMEGA_HOME")
	}
	return home, nil
}

// cmdKanban dispatches the kanban subcommand tree - the handler the
// capsule registers for the `kanban` CLI name.
func cmdKanban(args []string) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Print(kanbanHelp)
		return nil
	}
	switch sub, rest := args[0], args[1:]; sub {
	case "board":
		return cmdKanbanBoard(rest)
	case "list", "create", "done":
		return cmdKanbanCards(sub, rest)
	default:
		fmt.Fprintf(os.Stderr, "unknown kanban subcommand: %s\n", sub)
		fmt.Print(kanbanHelp)
		return fmt.Errorf("unknown kanban subcommand %q", sub)
	}
}

// kanbanCardOpts is the parsed per-call target: the explicit board name
// ("" = the current board, resolved live from the boards table) and
// the single-database store every subcommand opens.
type kanbanCardOpts struct {
	board  string
	status string
	store  *Store
}

func (o *kanbanCardOpts) Close() { o.store.Close() }

// parseCardFlags reads --board/--status from the args and returns the
// remaining positional arguments.
func parseCardFlags(args []string) (opts *kanbanCardOpts, rest []string, err error) {
	home, err := cliHome()
	if err != nil {
		return nil, nil, err
	}
	opts = &kanbanCardOpts{}
	fs := flag.NewFlagSet("kanban", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&opts.status, "status", "", "status filter")
	fs.StringVar(&opts.board, "board", "", "board name")
	if err := fs.Parse(args); err != nil {
		return nil, nil, err
	}
	s, oerr := MountRegistry(home)
	if oerr != nil {
		return nil, nil, oerr
	}
	opts.store = s
	return opts, fs.Args(), nil
}

// cmdKanbanCards runs the card subcommands (list/create/done), each
// scoped to one board per call.
func cmdKanbanCards(sub string, args []string) error {
	opts, rest, err := parseCardFlags(args)
	if err != nil {
		return err
	}
	defer opts.Close()
	ctx := context.Background()
	board := opts.board

	switch sub {
	case "list":
		if opts.status != "" && !IsListStatus(opts.status) {
			return fmt.Errorf("illegal status %q", opts.status)
		}
		tasks, err := opts.store.ListTasks(ctx, board, opts.status)
		if err != nil {
			return err
		}
		if len(tasks) == 0 {
			name := board
			if name == "" {
				name, err = opts.store.CurrentBoard()
				if err != nil {
					return err
				}
			}
			fmt.Printf("no cards on %s%s\n", name, statusSuffix(opts.status))
			return nil
		}
		for _, t := range tasks {
			line := fmt.Sprintf("%s [%s] %s", t.ID, t.Status, t.Title)
			if t.Assignee != "" {
				line += " @" + t.Assignee
			}
			fmt.Println(line)
		}
		return nil
	case "create":
		title := strings.TrimSpace(strings.Join(rest, " "))
		if title == "" {
			return fmt.Errorf("create needs a title")
		}
		created, err := opts.store.CreateTask(ctx, board, KanbanTask{Title: title, Status: opts.status})
		if err != nil {
			return err
		}
		fmt.Printf("created %s [%s] %s\n", created.ID, created.Status, created.Title)
		return nil
	case "done":
		if len(rest) != 1 {
			return fmt.Errorf("done needs exactly one task id")
		}
		done, err := opts.store.UpdateTask(ctx, board, rest[0], KanbanPatch{Status: StatusDone})
		if err != nil {
			return err
		}
		fmt.Printf("done %s: %s\n", done.ID, done.Title)
		return nil
	}
	return fmt.Errorf("unknown kanban card subcommand %q", sub)
}

// statusSuffix renders the list summary's status part.
func statusSuffix(status string) string {
	if status == "" {
		return ""
	}
	return " [" + status + "]"
}

// cmdKanbanBoard runs the board registry subcommands. These operate on
// the boards table (the DB registry), through the one database's store.
func cmdKanbanBoard(args []string) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(os.Stderr, "usage: omega kanban board {list|create <name>|delete <name>|use <name>|current}\n")
		return fmt.Errorf("missing board subcommand")
	}
	home, err := cliHome()
	if err != nil {
		return err
	}
	ctx := context.Background()
	s, err := MountRegistry(home)
	if err != nil {
		return err
	}
	defer s.Close()

	switch sub, rest := args[0], args[1:]; sub {
	case "list":
		boards, err := s.Boards(ctx)
		if err != nil {
			return err
		}
		for _, b := range boards {
			count := 0
			if tasks, terr := s.ListTasks(ctx, b.Name, ""); terr == nil {
				count = len(tasks)
			}
			marker := " "
			if b.Current {
				marker = "*"
			}
			fmt.Printf("%s %-20s %d card(s)\n", marker, b.Name, count)
		}
		fmt.Println("\n* current board")
		return nil
	case "current":
		pointer, err := s.CurrentBoard()
		if err != nil {
			return err
		}
		fmt.Println(pointer)
		return nil
	case "use":
		if len(rest) != 1 {
			return fmt.Errorf("usage: omega kanban board use <name>")
		}
		if !s.BoardExists(rest[0]) {
			return fmt.Errorf("no such board %q - create it first", rest[0])
		}
		if err := s.SetCurrentBoard(rest[0]); err != nil {
			return err
		}
		fmt.Printf("board: %s\n", rest[0])
		return nil
	case "create":
		if len(rest) != 1 {
			return fmt.Errorf("usage: omega kanban board create <name>")
		}
		if err := s.CreateBoardRow(rest[0]); err != nil {
			return err
		}
		fmt.Printf("created board %s\n", rest[0])
		return nil
	case "delete":
		if len(rest) != 1 {
			return fmt.Errorf("usage: omega kanban board delete <name>")
		}
		if err := s.DeleteBoardRow(rest[0]); err != nil {
			return err
		}
		fmt.Printf("deleted board %s\n", rest[0])
		return nil
	default:
		fmt.Fprintf(os.Stderr, "unknown board subcommand: %s\n", sub)
		return fmt.Errorf("unknown board subcommand %q", sub)
	}
}