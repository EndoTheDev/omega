package kanban

// Multi-board registry + store isolation tests (card t_d7a7beaa): a
// card on board A never appears on board B, the pointer picks the
// default board, and deletes refuse anything that would dangle.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
)

// testHome is a fresh omega home per test (no OMEGA_HOME games: the
// registry helpers take home explicitly, and this suite must not leak
// env into sibling packages).
func tempHome(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// wireStore opens a store on the pointer's board and wires the
// registry exactly as the plugin Mount does.
func wireStore(t *testing.T, home string) *Store {
	t.Helper()
	dsn, err := PointerDSN(home)
	if err != nil {
		t.Fatalf("pointer dsn: %v", err)
	}
	s, err := Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	s.ConfigureBoards(BoardsDir(home), mustPointer(t, home), filepath.Join(home, "data", "kanban.db"))
	t.Cleanup(func() { s.Close() })
	return s
}

func mustPointer(t *testing.T, home string) string {
	t.Helper()
	name, err := ReadPointer(home)
	if err != nil {
		t.Fatalf("read pointer: %v", err)
	}
	return name
}

func TestValidBoardName(t *testing.T) {
	legal := []string{"main", "alpha", "a1", "my_board", "x-y", "a"}
	for _, n := range legal {
		if !ValidBoardName(n) {
			t.Errorf("%q should be legal", n)
		}
	}
	illegal := []string{"", "A", "a b", "-x", "a/b", "a.b", "with$dot", longName()}
	for _, n := range illegal {
		if ValidBoardName(n) {
			t.Errorf("%q should be illegal", n)
		}
	}
}

func longName() string { return strings.Repeat("a", 65) }

func TestPointerDefaultsToMain(t *testing.T) {
	home := tempHome(t)
	if got := mustPointer(t, home); got != DefaultBoardName {
		t.Fatalf("pointer = %q, want main", got)
	}
	// main's dsn is the legacy kanban.db - the upgrade-compat anchor
	dsn, err := BoardDSN(home, DefaultBoardName)
	if err != nil {
		t.Fatal(err)
	}
	if dsn != filepath.Join(home, "data", "kanban.db") {
		t.Fatalf("main dsn = %q", dsn)
	}
}

func TestPointerRoundTripAndReject(t *testing.T) {
	home := tempHome(t)
	if err := WritePointer(home, "alpha"); err != nil {
		t.Fatal(err)
	}
	if got := mustPointer(t, home); got != "alpha" {
		t.Fatalf("pointer = %q, want alpha", got)
	}
	if err := WritePointer(home, "Not A Board"); err == nil {
		t.Fatal("illegal pointer name accepted")
	}
	// a corrupt pointer file fails loudly (mount surfaces it)
	if err := WritePointer(home, "alpha"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(BoardsDir(home), "current"), []byte("garbage name!\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPointer(home); err == nil {
		t.Fatal("corrupt pointer accepted")
	}
}

func TestCreateListDelete(t *testing.T) {
	home := tempHome(t)
	if err := CreateBoard(home, "alpha"); err != nil {
		t.Fatal(err)
	}
	names, err := ListBoards(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || names[0] != DefaultBoardName || names[1] != "alpha" {
		t.Fatalf("boards = %v", names)
	}
	if err := CreateBoard(home, "alpha"); err == nil {
		t.Fatal("duplicate create accepted")
	}
	if err := CreateBoard(home, DefaultBoardName); err == nil {
		t.Fatal("create main accepted")
	}

	// delete: refuses main, refuses unknown, refuses non-empty, refuses
	// the pointer target; a moved pointer + empty board deletes fine
	if err := DeleteBoard(home, DefaultBoardName); err == nil {
		t.Fatal("delete main accepted")
	}
	if err := DeleteBoard(home, "beta"); err == nil {
		t.Fatal("delete unknown accepted")
	}
	if err := WritePointer(home, "alpha"); err != nil {
		t.Fatal(err)
	}
	s := wireStore(t, home)
	if _, err := s.CreateTask(context.Background(), "alpha", agent.KanbanTask{Title: "held"}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if err := DeleteBoard(home, "alpha"); err == nil {
		t.Fatal("delete of pointer-target accepted")
	}
	// non-empty refusal (pointer moved elsewhere first)
	if err := WritePointer(home, DefaultBoardName); err != nil {
		t.Fatal(err)
	}
	if err := DeleteBoard(home, "alpha"); err == nil {
		t.Fatal("delete of non-empty accepted")
	}
	// archived tasks are still rows - there is no card deletion, so an
	// emptiable board is one that never held a task
	if err := CreateBoard(home, "gamma"); err != nil {
		t.Fatal(err)
	}
	if err := DeleteBoard(home, "gamma"); err != nil {
		t.Fatalf("delete of empty board refused: %v", err)
	}
	if names, err := ListBoards(home); err != nil || len(names) != 2 {
		t.Fatalf("boards after delete = %v err %v", names, err)
	}
}

func TestStoreBoardIsolation(t *testing.T) {
	home := tempHome(t)
	if err := CreateBoard(home, "alpha"); err != nil {
		t.Fatal(err)
	}
	if err := CreateBoard(home, "beta"); err != nil {
		t.Fatal(err)
	}
	s := wireStore(t, home) // pointer on main

	ctx := context.Background()
	mainCard, err := s.CreateTask(ctx, "", agent.KanbanTask{Title: "on main"})
	if err != nil {
		t.Fatal(err)
	}
	alphaCard, err := s.CreateTask(ctx, "alpha", agent.KanbanTask{Title: "on alpha"})
	if err != nil {
		t.Fatal(err)
	}
	betaCard, err := s.CreateTask(ctx, "beta", agent.KanbanTask{Title: "on beta"})
	if err != nil {
		t.Fatal(err)
	}

	// each board lists only its own card
	for board, want := range map[string]string{"": mainCard.ID, "alpha": alphaCard.ID, "beta": betaCard.ID} {
		tasks, err := s.ListTasks(ctx, board, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) != 1 || tasks[0].ID != want {
			t.Fatalf("board %q: %d cards, want exactly %q", board, len(tasks), want)
		}
	}

	// cross-board reads return not-found - a card on A never appears on B
	if _, err := s.GetTask(ctx, "alpha", mainCard.ID); err != ErrNotFound {
		t.Fatalf("main card visible on alpha: err = %v", err)
	}
	if _, err := s.GetTask(ctx, "", alphaCard.ID); err != ErrNotFound {
		t.Fatalf("alpha card visible on default: err = %v", err)
	}
	// cross-board mutation is refused the same way
	if _, err := s.UpdateTask(ctx, "beta", mainCard.ID, agent.KanbanPatch{Status: StatusDone}); err != ErrNotFound {
		t.Fatalf("cross-board move: err = %v", err)
	}
	if _, err := s.AddComment(ctx, "", alphaCard.ID, "t", "hi"); err != ErrNotFound {
		t.Fatalf("cross-board comment: err = %v", err)
	}

	// unknown / illegal boards error at the door; the store never
	// auto-creates a named board
	if _, err := s.ListTasks(ctx, "gamma", ""); err == nil {
		t.Fatal("unknown board accepted")
	}
	if _, err := s.ListTasks(ctx, "Bad Name", ""); err == nil {
		t.Fatal("illegal board name accepted")
	}

	// comments and events stay on their board
	if _, err := s.AddComment(ctx, "alpha", alphaCard.ID, "a", "note"); err != nil {
		t.Fatal(err)
	}
	comments, err := s.GetTaskComments(ctx, "alpha", alphaCard.ID)
	if err != nil || len(comments) != 1 {
		t.Fatalf("alpha comments = %d err %v", len(comments), err)
	}
	// beta cannot see the comment: the id simply has no thread there
	betaComments, err := s.GetTaskComments(ctx, "beta", alphaCard.ID)
	if err != nil || len(betaComments) != 0 {
		t.Fatalf("beta saw alpha's comment: %d %v", len(betaComments), err)
	}
}

func TestPointerSwitchMovesDefault(t *testing.T) {
	home := tempHome(t)
	if err := CreateBoard(home, "alpha"); err != nil {
		t.Fatal(err)
	}
	s := wireStore(t, home)
	ctx := context.Background()
	alphaCard, err := s.CreateTask(ctx, "alpha", agent.KanbanTask{Title: "alpha-only"})
	if err != nil {
		t.Fatal(err)
	}
	s.Close()

	// switch the pointer, remount: "" now lands on alpha
	if err := WritePointer(home, "alpha"); err != nil {
		t.Fatal(err)
	}
	s2 := wireStore(t, home)
	tasks, err := s2.ListTasks(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].ID != alphaCard.ID {
		t.Fatalf("default after switch = %d cards, want alpha's", len(tasks))
	}
	// main is still reachable by name and still holds nothing
	mainTasks, err := s2.ListTasks(ctx, DefaultBoardName, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(mainTasks) != 0 {
		t.Fatalf("main after switch = %d cards, want 0", len(mainTasks))
	}
}

func TestBoardsListing(t *testing.T) {
	home := tempHome(t)
	if err := CreateBoard(home, "alpha"); err != nil {
		t.Fatal(err)
	}
	s := wireStore(t, home)
	boards, err := s.Boards(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(boards) != 2 {
		t.Fatalf("boards = %+v", boards)
	}
	if boards[0].Name != DefaultBoardName || !boards[0].Current {
		t.Fatalf("first = %+v, want main current", boards[0])
	}
	if boards[1].Name != "alpha" || boards[1].Current {
		t.Fatalf("second = %+v", boards[1])
	}
	// a directory without kanban.db is not a board
	if mkdirErr := os.MkdirAll(filepath.Join(BoardsDir(home), "hollow"), 0755); mkdirErr != nil {
		t.Fatal(mkdirErr)
	}
	boards, err = s.Boards(context.Background())
	if err != nil || len(boards) != 2 {
		t.Fatalf("boards = %+v err %v", boards, err)
	}
}