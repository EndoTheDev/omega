package kanban

// The board registry (multi-board, Hermes-shaped): every board is its
// own sqlite file under <home>/data/boards/<name>/kanban.db, and a
// one-line pointer file <home>/data/boards/current names the board
// that "" (unspecified) resolves to. Storage decision, with its
// ceiling: per-board dbs mean one store handle per board, so
// cross-board queries never exist. That is fine on purpose - the
// Hermes law from the reference implementation holds (cross-board
// card moves corrupt done-counts; switching board is a pointer use,
// two operations, never a card mutation).

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/EndoTheDev/omega/modules/datadir"
)

// DefaultBoardName is the legacy default board: it has no directory of
// its own - its data stays in the historical <home>/data/kanban.db, so
// existing installs keep their cards on upgrade. The name is reserved.
const DefaultBoardName = "main"

// boardNameRE governs registry names: lowercase, starts alphanumeric,
// 1-64 chars, filesystem-safe. It doubles as a safety filter because
// names become directory components.
var boardNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// ValidBoardName reports whether name may name a board. "main" is
// legal (the default board); "" is not a board.
func ValidBoardName(name string) bool {
	return boardNameRE.MatchString(name)
}

// BoardsDir returns the board registry directory for a home:
// <home>/data/boards.
func BoardsDir(home string) string {
	return filepath.Join(datadir.Dir(home), "boards")
}

// PointerPath returns the current-board pointer file for a home.
func PointerPath(home string) string {
	return filepath.Join(BoardsDir(home), "current")
}

// BoardDSN returns the sqlite path for the named board: "main" stays
// at the legacy <home>/data/kanban.db; every other (valid) board gets
// its own directory.
func BoardDSN(home, name string) (string, error) {
	if name == DefaultBoardName {
		return datadir.DBPath(home, "kanban.db"), nil
	}
	if !ValidBoardName(name) {
		return "", fmt.Errorf("illegal board name %q", name)
	}
	return filepath.Join(BoardsDir(home), name, "kanban.db"), nil
}

// ReadPointer returns the board the pointer file names, defaulting to
// "main" when the file does not exist (fresh install, no boards made
// yet). A present-but-empty pointer falls back to "main" too - an
// empty file is not a board name.
func ReadPointer(home string) (string, error) {
	data, err := os.ReadFile(PointerPath(home))
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultBoardName, nil
		}
		return "", err
	}
	name := strings.TrimSpace(string(data))
	if name == "" {
		return DefaultBoardName, nil
	}
	if !ValidBoardName(name) {
		return "", fmt.Errorf("current-board pointer names %q: not a board name", name)
	}
	return name, nil
}

// PointerDSN resolves the current pointer to its board's sqlite path
// (mount hosts use this to pre-resolve the default board's dsn).
func PointerDSN(home string) (string, error) {
	name, err := ReadPointer(home)
	if err != nil {
		return "", err
	}
	return BoardDSN(home, name)
}

// WritePointer points the home at the named board.
func WritePointer(home, name string) error {
	if !ValidBoardName(name) {
		return fmt.Errorf("illegal board name %q", name)
	}
	if err := os.MkdirAll(BoardsDir(home), 0755); err != nil {
		return err
	}
	return os.WriteFile(PointerPath(home), []byte(name+"\n"), 0644)
}

// ListBoards returns the home's board names: "main" first, then the
// named boards (sorted). A board directory without a kanban.db inside
// is not a board.
func ListBoards(home string) ([]string, error) {
	names := []string{DefaultBoardName}
	entries, err := os.ReadDir(BoardsDir(home))
	if err != nil {
		if os.IsNotExist(err) {
			return names, nil
		}
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() || e.Name() == DefaultBoardName {
			continue
		}
		if _, err := os.Stat(filepath.Join(BoardsDir(home), e.Name(), "kanban.db")); err == nil {
			names = append(names, e.Name())
		}
	}
	slices.Sort(names[1:])
	return names, nil
}

// CreateBoard creates the named board: its directory + an empty
// schema. Creating "main" or an existing board is an error. A
// non-home dir is not required.
func CreateBoard(home, name string) error {
	if name == DefaultBoardName {
		return fmt.Errorf("main is the default board and already exists")
	}
	if !ValidBoardName(name) {
		return fmt.Errorf("illegal board name %q", name)
	}
	dsn, err := BoardDSN(home, name)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dsn); err == nil {
		return fmt.Errorf("board %q already exists", name)
	}
	// the board's directory: Migrate only guarantees data/, not the
	// per-board subdirectory
	if err := os.MkdirAll(filepath.Dir(dsn), 0755); err != nil {
		return err
	}
	s, err := Open(dsn)
	if err != nil {
		return err
	}
	return s.Close()
}

// DeleteBoard removes the named board, refusing: the default board,
// unknown boards, the current pointer target (the pointer would
// dangle), and any board that still holds tasks. Comments and events
// ride the tasks via cascade, so the task count is the emptiness test.
func DeleteBoard(home, name string) error {
	if name == DefaultBoardName {
		return fmt.Errorf("refusing to delete the default board")
	}
	if !ValidBoardName(name) {
		return fmt.Errorf("illegal board name %q", name)
	}
	dsn, err := BoardDSN(home, name)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dsn); err != nil {
		return fmt.Errorf("no such board %q", name)
	}
	pointer, err := ReadPointer(home)
	if err != nil {
		return err
	}
	if pointer == name {
		return fmt.Errorf("board %q is the current board - switch first (kanban board use)", name)
	}
	s, err := Open(dsn)
	if err != nil {
		return err
	}
	defer s.Close()
	// the emptiness check needs the named-board registry wired
	s.ConfigureBoards(BoardsDir(home), DefaultBoardName, datadir.DBPath(home, "kanban.db"))
	tasks, err := s.ListTasks(context.Background(), name, "")
	if err != nil {
		return err
	}
	if len(tasks) > 0 {
		return fmt.Errorf("board %q still holds %d task(s) - move or delete them first", name, len(tasks))
	}
	boardDir := filepath.Dir(dsn)
	if err := os.RemoveAll(boardDir); err != nil {
		return err
	}
	return nil
}