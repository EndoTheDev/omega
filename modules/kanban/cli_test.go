package kanban

// Kanban CLI tests (t_d7a7beaa, argv homed to the capsule in v0.12
// phase D t_69322045): board registry ops against a temp home, and the
// isolation probe the card asks for - two boards, a card on each, no
// bleed across list calls.

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// captureStdout reroutes os.Stdout through a pipe for the duration of
// fn (the CLI handlers print straight to stdout).
func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = old }()
	outCh := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		outCh <- string(b)
	}()
	fnErr := fn()
	w.Close()
	return <-outCh, fnErr
}

// cardCount opens the board's db and counts tasks (registry wired,
// the way a mount does it).
func cardCount(t *testing.T, home, board string) int {
	t.Helper()
	s := openBoardStore(t, home)
	tasks, err := s.ListTasks(context.Background(), board, "")
	if err != nil {
		t.Fatal(err)
	}
	return len(tasks)
}

// openBoardStore opens the pointer-default dsn with the registry
// wired, matching a mount.
func openBoardStore(t *testing.T, home string) *Store {
	t.Helper()
	dsn, err := BoardDSN(home, DefaultBoardName)
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	s.ConfigureBoards(BoardsDir(home), DefaultBoardName, dsn)
	t.Cleanup(func() { s.Close() })
	return s
}

func TestKanbanBoardRegistryCLI(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)

	// create two boards
	if err := cmdKanban([]string{"board", "create", "alpha"}); err != nil {
		t.Fatalf("create alpha: %v", err)
	}
	if err := cmdKanban([]string{"board", "create", "alpha"}); err == nil {
		t.Fatal("duplicate create accepted")
	}
	if err := cmdKanban([]string{"board", "create", "Not A Name"}); err == nil {
		t.Fatal("illegal name accepted")
	}

	// current pointer is main; use lands on existing boards only
	if out, err := captureStdout(t, func() error {
		return cmdKanban([]string{"board", "current"})
	}); err != nil || strings.TrimSpace(out) != "main" {
		t.Fatalf("current = %q err %v", out, err)
	}
	if err := cmdKanban([]string{"board", "use", "beta"}); err == nil {
		t.Fatal("use of unknown board accepted")
	}
	if err := cmdKanban([]string{"board", "use", "alpha"}); err != nil {
		t.Fatalf("use alpha: %v", err)
	}
	if out, err := captureStdout(t, func() error {
		return cmdKanban([]string{"board", "current"})
	}); err != nil || strings.TrimSpace(out) != "alpha" {
		t.Fatalf("current after use = %q err %v", out, err)
	}

	// card ops: omitted --board follows the pointer (alpha)
	if err := cmdKanban([]string{"create", "flows", "to", "pointer"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if n := cardCount(t, home, "alpha"); n != 1 {
		t.Fatalf("alpha cards = %d", n)
	}
	if n := cardCount(t, home, "main"); n != 0 {
		t.Fatalf("main bleed = %d", n)
	}
	// explicit --board overrides the pointer
	if err := cmdKanban([]string{"create", "--board", "main", "on main"}); err != nil {
		t.Fatalf("create main: %v", err)
	}
	if n := cardCount(t, home, "main"); n != 1 {
		t.Fatalf("main cards = %d", n)
	}

	// list on both boards: no bleed across list calls
	for _, probe := range []struct {
		args []string
		want string
	}{{[]string{"list"}, "flows to pointer"}, {[]string{"list", "--board", "main"}, "on main"}} {
		other := "on main"
		if probe.want == "on main" {
			other = "flows to pointer"
		}
		out, err := captureStdout(t, func() error { return cmdKanban(probe.args) })
		if err != nil || !strings.Contains(out, probe.want) || !strings.Contains(out, "t_") {
			t.Fatalf("list %v: %q err %v", probe.args, out, err)
		}
		if strings.Contains(out, other) {
			t.Fatalf("list %v bled across boards: %q", probe.args, out)
		}
	}

	// done completes on the right board
	tasks := listBoardTasks(t, home, "alpha")
	if len(tasks) != 1 {
		t.Fatalf("alpha tasks = %d", len(tasks))
	}
	if err := cmdKanban([]string{"done", tasks[0].ID}); err != nil {
		t.Fatalf("done: %v", err)
	}
	if done := listBoardTasks(t, home, "alpha")[0].Status; done != StatusDone {
		t.Fatalf("status = %q, want done", done)
	}

	// delete: refuses the pointer target, then a non-empty board, then
	// an empty board lands
	if err := cmdKanban([]string{"board", "delete", "alpha"}); err == nil {
		t.Fatal("delete of pointer target accepted")
	}
	if err := cmdKanban([]string{"board", "use", "main"}); err != nil {
		t.Fatal(err)
	}
	if err := cmdKanban([]string{"board", "delete", "alpha"}); err == nil {
		t.Fatal("delete of non-empty accepted")
	}
	if err := cmdKanban([]string{"board", "create", "beta"}); err != nil {
		t.Fatal(err)
	}
	if err := cmdKanban([]string{"board", "delete", "beta"}); err != nil {
		t.Fatalf("delete of empty board: %v", err)
	}
}

func listBoardTasks(t *testing.T, home, board string) []core.KanbanTask {
	t.Helper()
	s := openBoardStore(t, home)
	tasks, err := s.ListTasks(context.Background(), board, "")
	if err != nil {
		t.Fatal(err)
	}
	return tasks
}

func TestKanbanCLICardValidation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	if err := cmdKanban([]string{"create"}); err == nil {
		t.Fatal("titleless create accepted")
	}
	if err := cmdKanban([]string{"done"}); err == nil {
		t.Fatal("idless done accepted")
	}
	if err := cmdKanban([]string{"done", "a", "b"}); err == nil {
		t.Fatal("two ids accepted")
	}
	if err := cmdKanban([]string{"list", "--status", "bogus"}); err == nil {
		t.Fatal("illegal status accepted")
	}
	if err := cmdKanban([]string{"bogus", "sub"}); err == nil {
		t.Fatal("unknown subcommand accepted")
	}
}