package kanban

// The board registry CLI: the kanban subcommand's home (v0.12 phase D, grill Q6.

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/EndoTheDev/omega/harness/datadir"
)

// kanbanSummary is the one-line entry the global `omega --help` list shows for this.
const kanbanSummary = "the task board registry (see: omega kanban --help)"

// kanbanHelp documents the kanban subcommand tree (the module's own help.
const kanbanHelp = `omega kanban - the task board registry

Usage:
  omega kanban board list                  all boards, task counts, current marker
  omega kanban board create <name>         create an empty board
  omega kanban board rename <old> <new>    rename a board in place
  omega kanban board delete <name>         delete a board (refuses while a
                                           live card is on it; archived cards
                                           ride out with it - zero boards is legal)
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

// cliHome resolves the omega home for the CLI handlers.
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

// cmdKanban dispatches the kanban subcommand tree.
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

// kanbanCardOpts is the parsed per-call target.
type kanbanCardOpts struct {
	board  string
	status string
	store  *Store
}

func (o *kanbanCardOpts) Close() { o.store.Close() }

// parseCardFlags reads --board/--status from the args and returns the remaining.
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

// cmdKanbanCards runs the card subcommands (list/create/done), each scoped to one.
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

// cmdKanbanBoard runs the board registry subcommands.
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
	case "rename":
		if len(rest) != 2 {
			return fmt.Errorf("usage: omega kanban board rename <old> <new>")
		}
		if err := s.RenameBoardRow(rest[0], rest[1]); err != nil {
			return err
		}
		fmt.Printf("renamed board %s -> %s\n", rest[0], rest[1])
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