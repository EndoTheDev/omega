package main

// The board registry CLI: multi-board management against the home's
// data/boards directory. One board per workspace needs named boards
// plus a current-board pointer - the Hermes shape (named dbs, no
// cross-board moves). Card operations target one board per call, the
// pointer selects which board an omitted --board means.

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/EndoTheDev/omega/agent"
	kanbanModule "github.com/EndoTheDev/omega/modules/kanban"
)

// kanbanHelp documents the kanban subcommand tree.
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
                   'main' is the default board - the legacy kanban.db)
  --status <s>     card list filter (triage todo scheduled ready running
                   blocked review done archived)
`

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

// kanbanCardOpts is the parsed per-call target: the board name and the
// store it opens against ("main" and pointer targets share the legacy
// db path, so the same board reachable by two names opens two handles
// - harmless, sqlite WAL tolerates it, and the CLI opens one at a
// time anyway).
type kanbanCardOpts struct {
	board  string
	status string
	store  *kanbanModule.Store
}

func (o *kanbanCardOpts) Close() { o.store.Close() }

// parseCardFlags reads --board/--status from the args and returns the
// remaining positional arguments.
func parseCardFlags(args []string) (opts *kanbanCardOpts, rest []string, err error) {
	opts = &kanbanCardOpts{}
	fs := flag.NewFlagSet("kanban", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&opts.status, "status", "", "status filter")
	fs.StringVar(&opts.board, "board", "", "board name")
	if err := fs.Parse(args); err != nil {
		return nil, nil, err
	}
	home := omegaHome()
	if opts.board == "" {
		name, perr := kanbanModule.ReadPointer(home)
		if perr != nil {
			return nil, nil, perr
		}
		opts.board = name
	}
	dsn, berr := kanbanModule.BoardDSN(home, opts.board)
	if berr != nil {
		return nil, nil, berr
	}
	s, oerr := kanbanModule.Open(dsn)
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

	switch sub {
	case "list":
		if opts.status != "" && !kanbanModule.IsStatus(opts.status) {
			return fmt.Errorf("illegal status %q", opts.status)
		}
		tasks, err := opts.store.ListTasks(ctx, "", opts.status)
		if err != nil {
			return err
		}
		if len(tasks) == 0 {
			fmt.Printf("no cards on %s%s\n", opts.board, statusSuffix(opts.status))
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
		created, err := opts.store.CreateTask(ctx, "", agent.KanbanTask{Title: title, Status: opts.status})
		if err != nil {
			return err
		}
		fmt.Printf("created %s [%s] %s\n", created.ID, created.Status, created.Title)
		return nil
	case "done":
		if len(rest) != 1 {
			return fmt.Errorf("done needs exactly one task id")
		}
		done, err := opts.store.UpdateTask(ctx, "", rest[0], agent.KanbanPatch{Status: kanbanModule.StatusDone})
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

// cmdKanbanBoard runs the board registry subcommands. These operate
// on the home's registry (directories + pointer file), not one store
// handle.
func cmdKanbanBoard(args []string) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(os.Stderr, "usage: omega kanban board {list|create <name>|delete <name>|use <name>|current}\n")
		return fmt.Errorf("missing board subcommand")
	}
	home := omegaHome()
	ctx := context.Background()
	switch sub, rest := args[0], args[1:]; sub {
	case "list":
		names, err := kanbanModule.ListBoards(home)
		if err != nil {
			return err
		}
		pointer, err := kanbanModule.ReadPointer(home)
		if err != nil {
			return err
		}
		for _, name := range names {
			dsn, derr := kanbanModule.BoardDSN(home, name)
			if derr != nil {
				continue
			}
			count := 0
			if s, oerr := kanbanModule.Open(dsn); oerr == nil {
				if tasks, terr := s.ListTasks(ctx, "", ""); terr == nil {
					count = len(tasks)
				}
				s.Close()
			}
			marker := " "
			if name == pointer {
				marker = "*"
			}
			fmt.Printf("%s %-20s %d card(s)\n", marker, name, count)
		}
		fmt.Println("\n* current board")
		return nil
	case "current":
		pointer, err := kanbanModule.ReadPointer(home)
		if err != nil {
			return err
		}
		fmt.Println(pointer)
		return nil
	case "use":
		if len(rest) != 1 {
			return fmt.Errorf("usage: omega kanban board use <name>")
		}
		dsn, derr := kanbanModule.BoardDSN(home, rest[0])
		if derr != nil {
			return derr
		}
		if _, serr := os.Stat(dsn); serr != nil {
			return fmt.Errorf("no such board %q - create it first", rest[0])
		}
		if err := kanbanModule.WritePointer(home, rest[0]); err != nil {
			return err
		}
		fmt.Printf("board: %s\n", rest[0])
		return nil
	case "create":
		if len(rest) != 1 {
			return fmt.Errorf("usage: omega kanban board create <name>")
		}
		if err := kanbanModule.CreateBoard(home, rest[0]); err != nil {
			return err
		}
		fmt.Printf("created board %s\n", rest[0])
		return nil
	case "delete":
		if len(rest) != 1 {
			return fmt.Errorf("usage: omega kanban board delete <name>")
		}
		if err := kanbanModule.DeleteBoard(home, rest[0]); err != nil {
			return err
		}
		fmt.Printf("deleted board %s\n", rest[0])
		return nil
	default:
		fmt.Fprintf(os.Stderr, "unknown board subcommand: %s\n", sub)
		return fmt.Errorf("unknown board subcommand %q", sub)
	}
}