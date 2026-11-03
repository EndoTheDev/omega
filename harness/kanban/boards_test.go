package kanban

// The board registry + store isolation tests (card t_d7a7beaa, folded
// into one database by t_feae19d3): a card on board A never appears on
// board B (queries by board_id, never files), the current-board flag
// picks what "" means, deletes refuse anything that would dangle, and
// the legacy per-board-directory layout folds in under verified
// counts - once, with the old layout deleted after.

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// tempHome is a fresh omega home per test (no OMEGA_HOME games: the
// registry helpers take home explicitly, and this suite must not leak
// env into sibling packages).
func tempHome(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// openRegistryStore opens the home's single kanban database exactly the
// way the plugin Mount does (schema ensured, legacy fold-back).
func openRegistryStore(t *testing.T, home string) *Store {
	t.Helper()
	s, err := MountRegistry(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestValidBoardName(t *testing.T) {
	legal := []string{"main", "alpha", "a1", "my_board", "x-y", "a", "j78-a"}
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

func TestFreshRegistryHasMain(t *testing.T) {
	home := tempHome(t)
	s := openRegistryStore(t, home)

	// the one database owns every board; no per-board directory exists
	if _, err := os.Stat(filepath.Join(datadirBoards(home))); !os.IsNotExist(err) {
		t.Fatalf("boards dir exists on a fresh home: %v", err)
	}
	boards, err := s.Boards(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(boards) != 1 || boards[0].Name != DefaultBoardName || !boards[0].Current {
		t.Fatalf("boards = %+v, want [main current]", boards)
	}
	if cur, err := s.CurrentBoard(); err != nil || cur != DefaultBoardName {
		t.Fatalf("current = %q err %v", cur, err)
	}
	// cards land on main by default and the row answers with its id
	bid, err := s.boardID("")
	if err != nil || bid == 0 {
		t.Fatalf("boardID(\"\") = %d err %v", bid, err)
	}
	if main, err := s.boardID(DefaultBoardName); err != nil || main != bid {
		t.Fatalf("main id = %d, current id = %d, err %v", main, bid, err)
	}
}

// datadirBoards is the legacy registry directory inside a home.
func datadirBoards(home string) string {
	return filepath.Join(home, "data", "boards")
}

func TestBoardCRUDRows(t *testing.T) {
	home := tempHome(t)
	s := openRegistryStore(t, home)
	ctx := context.Background()

	if err := s.CreateBoardRow("alpha"); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateBoardRow("alpha"); err == nil {
		t.Fatal("duplicate create accepted")
	}
	if err := s.CreateBoardRow(DefaultBoardName); err == nil {
		t.Fatal("create main accepted")
	}
	if err := s.CreateBoardRow("Not A Name"); err == nil {
		t.Fatal("illegal name accepted")
	}

	boards, err := s.Boards(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(boards) != 2 || boards[0].Name != DefaultBoardName || boards[1].Name != "alpha" {
		t.Fatalf("boards = %+v", boards)
	}
	if boards[0].Name == "alpha" && boards[1].Name == DefaultBoardName {
		// ordering is creation order (position), not name sort
		t.Fatalf("boards out of creation order: %+v", boards)
	}
	if boards[0].Name != DefaultBoardName {
		t.Fatalf("first row = %+v, want main first", boards[0])
	}

	// current flag moves and stays exclusive
	if err := s.SetCurrentBoard("alpha"); err != nil {
		t.Fatal(err)
	}
	boards, _ = s.Boards(ctx)
	if boards[0].Current || !boards[1].Current {
		t.Fatalf("flag did not move: %+v", boards)
	}
	if err := s.SetCurrentBoard("ghost"); err == nil {
		t.Fatal("switch to unknown board accepted")
	}
	if cur, err := s.CurrentBoard(); err != nil || cur != "alpha" {
		t.Fatalf("current = %q err %v", cur, err)
	}
}

func TestDeleteGuards(t *testing.T) {
	home := tempHome(t)
	s := openRegistryStore(t, home)
	ctx := context.Background()

	if err := s.DeleteBoardRow(DefaultBoardName); err == nil {
		t.Fatal("delete main accepted")
	}
	if err := s.DeleteBoardRow("beta"); err == nil {
		t.Fatal("delete unknown accepted")
	}
	if err := s.CreateBoardRow("alpha"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask(ctx, "alpha", KanbanTask{Title: "held"}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteBoardRow("alpha"); err == nil || !strings.Contains(err.Error(), "still holds") {
		t.Fatalf("delete of non-empty: %v", err)
	}
	// the current board refuses even when it is empty
	if err := s.CreateBoardRow("gamma"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCurrentBoard("gamma"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteBoardRow("gamma"); err == nil || !strings.Contains(err.Error(), "current board") {
		t.Fatalf("delete of the current board: %v", err)
	}
	// switch away: gamma is empty and deletable; alpha still is not
	if err := s.SetCurrentBoard(DefaultBoardName); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteBoardRow("gamma"); err != nil {
		t.Fatalf("delete of empty board refused: %v", err)
	}
	if err := s.DeleteBoardRow("alpha"); err == nil || !strings.Contains(err.Error(), "still holds") {
		t.Fatalf("delete of non-empty accepted: %v", err)
	}
	boards, _ := s.Boards(ctx)
	if len(boards) != 2 || boards[0].Name != DefaultBoardName || boards[1].Name != "alpha" {
		t.Fatalf("boards after delete = %+v", boards)
	}
	// archived tasks are still rows - there is no card deletion, so an
	// emptiable board is one that never held a task
}

func TestStoreBoardIsolation(t *testing.T) {
	home := tempHome(t)
	s := openRegistryStore(t, home)
	if err := s.CreateBoardRow("alpha"); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateBoardRow("beta"); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	mainCard, err := s.CreateTask(ctx, "", KanbanTask{Title: "on main"})
	if err != nil {
		t.Fatal(err)
	}
	alphaCard, err := s.CreateTask(ctx, "alpha", KanbanTask{Title: "on alpha"})
	if err != nil {
		t.Fatal(err)
	}
	betaCard, err := s.CreateTask(ctx, "beta", KanbanTask{Title: "on beta"})
	if err != nil {
		t.Fatal(err)
	}

	// ids are drawn from one global space (t_<hex>) - isolation means
	// QUERIES, not id collisions
	ids := map[string]string{mainCard.ID: "main", alphaCard.ID: "alpha", betaCard.ID: "beta"}
	if len(ids) != 3 {
		t.Fatal("task ids collided")
	}
	if !strings.HasPrefix(mainCard.ID, "t_") {
		t.Fatalf("task id shape: %q", mainCard.ID)
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
	if _, err := s.UpdateTask(ctx, "beta", mainCard.ID, KanbanPatch{Status: StatusDone}); err != ErrNotFound {
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
	events, err := s.GetTaskEvents(ctx, "beta", alphaCard.ID)
	if err != nil || len(events) != 0 {
		t.Fatalf("beta saw alpha's events: %d %v", len(events), err)
	}
}

func TestCurrentBoardFlagMovesDefault(t *testing.T) {
	home := tempHome(t)
	ctx := context.Background()
	{
		s := openRegistryStore(t, home)
		if err := s.CreateBoardRow("alpha"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.CreateTask(ctx, "alpha", KanbanTask{Title: "alpha-only"}); err != nil {
			t.Fatal(err)
		}
		s.Close()
	}

	// flip the current-board flag, open a fresh handle: "" lands on alpha
	s2, err := MountRegistry(home)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if err := s2.SetCurrentBoard("alpha"); err != nil {
		t.Fatal(err)
	}
	tasks, err := s2.ListTasks(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].Title != "alpha-only" {
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
	// the flag survives a remount
	s2.Close()
	s3, err := MountRegistry(home)
	if err != nil {
		t.Fatal(err)
	}
	defer s3.Close()
	if cur, err := s3.CurrentBoard(); err != nil || cur != "alpha" {
		t.Fatalf("remounted current = %q err %v", cur, err)
	}
}

// --- the legacy layout migration -----------------------------------------

// seedLegacy is a real legacy sqlite board db (old schema, no boards
// table, no board_id) with two tasks (ids t_<prefix>1/2), one comment,
// three events.
func seedLegacy(t *testing.T, dsn string, prefix string) {
	t.Helper()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`
CREATE TABLE IF NOT EXISTS tasks (
	id TEXT PRIMARY KEY, title TEXT NOT NULL, body TEXT NOT NULL DEFAULT '',
	assignee TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'triage',
	session_id TEXT NOT NULL DEFAULT '', source TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL, started_at TEXT NOT NULL DEFAULT '', completed_at TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS task_comments (
	id INTEGER PRIMARY KEY AUTOINCREMENT, task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
	author TEXT NOT NULL, body TEXT NOT NULL, created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS task_events (
	id INTEGER PRIMARY KEY AUTOINCREMENT, task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
	kind TEXT NOT NULL, detail TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL
)`)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO tasks (id, title, body, assignee, status, created_at) VALUES ('` + prefix + `1', 'first', '', '', 'todo', '2026-11-01T00:00:00Z')`,
		`INSERT INTO tasks (id, title, body, assignee, status, created_at) VALUES ('` + prefix + `2', 'second', '', '', 'done', '2026-11-01T00:00:01Z')`,
		`INSERT INTO task_comments (task_id, author, body, created_at) VALUES ('` + prefix + `1', 'web', 'a comment', '2026-11-01T00:00:02Z')`,
		`INSERT INTO task_events (task_id, kind, detail, created_at) VALUES ('` + prefix + `1', 'created', 'status todo', '2026-11-01T00:00:03Z')`,
		`INSERT INTO task_events (task_id, kind, detail, created_at) VALUES ('` + prefix + `2', 'created', 'status done', '2026-11-01T00:00:04Z')`,
		`INSERT INTO task_events (task_id, kind, detail, created_at) VALUES ('` + prefix + `2', 'moved', 'todo -> done', '2026-11-01T00:00:05Z')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMigrateLegacyBoardLayout(t *testing.T) {
	home := tempHome(t)
	boards := datadirBoards(home)

	// legacy main: tasks already live in the top-level kanban.db
	mainDSN := filepath.Join(home, "data", "kanban.db")
	if err := os.MkdirAll(filepath.Dir(mainDSN), 0755); err != nil {
		t.Fatal(err)
	}
	seedLegacy(t, mainDSN, "main")
	// one real legacy board
	if err := os.MkdirAll(filepath.Join(boards, "legacy-crew"), 0755); err != nil {
		t.Fatal(err)
	}
	seedLegacy(t, filepath.Join(boards, "legacy-crew", "kanban.db"), "crew")
	// a recipe-PINNED journey board: never migrates (the recipes
	// re-create it fresh), but its dir is archived - never destroyed
	if err := os.MkdirAll(filepath.Join(boards, "j78-a"), 0755); err != nil {
		t.Fatal(err)
	}
	seedLegacy(t, filepath.Join(boards, "j78-a", "kanban.db"), "junk")
	// the pointer names the real board
	if err := os.WriteFile(filepath.Join(boards, "current"), []byte("legacy-crew\n"), 0644); err != nil {
		t.Fatal(err)
	}

	s, err := MountRegistry(home)
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	defer s.Close()
	ctx := context.Background()

	// the fold-back: main kept its rows, legacy-crew landed, junk died
	boards_, err := s.Boards(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(boards_) != 2 || boards_[0].Name != DefaultBoardName || boards_[1].Name != "legacy-crew" {
		t.Fatalf("boards = %+v", boards_)
	}
	if boards_[0].Current || !boards_[1].Current {
		t.Fatalf("current flag did not follow the pointer: %+v", boards_)
	}
	mainTasks, err := s.ListTasks(ctx, DefaultBoardName, "")
	if err != nil || len(mainTasks) != 2 {
		t.Fatalf("main tasks = %d err %v", len(mainTasks), err)
	}
	crewTasks, err := s.ListTasks(ctx, "legacy-crew", "")
	if err != nil || len(crewTasks) != 2 {
		t.Fatalf("legacy-crew tasks = %d err %v", len(crewTasks), err)
	}
	comments, err := s.GetTaskComments(ctx, "legacy-crew", "crew1")
	if err != nil || len(comments) != 1 || comments[0].Body != "a comment" {
		t.Fatalf("legacy comments = %+v err %v", comments, err)
	}
	events, err := s.GetTaskEvents(ctx, "legacy-crew", "crew2")
	if err != nil || len(events) != 2 {
		t.Fatalf("legacy events = %+v err %v", events, err)
	}
	// old dirs gone - migrated board, pointer file, all of it; the
	// pinned journey board did NOT become a row and did NOT die: it is
	// archived under data/boards-junk with its data intact
	if _, serr := os.Stat(boards); !os.IsNotExist(serr) {
		t.Fatalf("legacy boards dir survives migration: %v", serr)
	}
	if s.BoardExists("j78-a") {
		t.Fatal("pinned journey board became a row")
	}
	if _, serr := os.Stat(filepath.Join(home, "data", "boards-junk", "j78-a", "kanban.db")); serr != nil {
		t.Fatalf("pinned journey board not archived with its data: %v", serr)
	}
	// the old main db file (now the single db) keeps the full shape:
	// unknown ids answer not-found on the migrated board
	if _, err := s.GetTask(ctx, "legacy-crew", "t_nope"); err != ErrNotFound {
		t.Fatalf("bogus id answered: %v", err)
	}

	// second boot: no old layout - a strict no-op, data intact
	s.Close()
	s2, err := MountRegistry(home)
	if err != nil {
		t.Fatalf("remount: %v", err)
	}
	defer s2.Close()
	again, err := s2.Boards(ctx)
	if err != nil || len(again) != 2 {
		t.Fatalf("second boot boards = %+v err %v", again, err)
	}
	if ct, err := s2.ListTasks(ctx, "legacy-crew", ""); err != nil || len(ct) != 2 {
		t.Fatalf("second boot lost rows: %d %v", len(ct), err)
	}
	if cur, err := s2.CurrentBoard(); err != nil || cur != "legacy-crew" {
		t.Fatalf("second boot current = %q err %v", cur, err)
	}
}

func TestMigrateAbortsOnUnreadableSource(t *testing.T) {
	home := tempHome(t)
	boards := datadirBoards(home)
	mainDSN := filepath.Join(home, "data", "kanban.db")
	if err := os.MkdirAll(filepath.Dir(mainDSN), 0755); err != nil {
		t.Fatal(err)
	}
	seedLegacy(t, mainDSN, "main")
	if err := os.MkdirAll(filepath.Join(boards, "corrupt"), 0755); err != nil {
		t.Fatal(err)
	}
	// not a database
	if err := os.WriteFile(filepath.Join(boards, "corrupt", "kanban.db"), []byte("definitely not sqlite"), 0644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(mainDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.migrateLegacyBoards(home); err == nil {
		t.Fatal("corrupt source board migrated without verification")
	}
	// the old layout survives untouched: the file remains, main's rows
	// intact, no board row for the corrupt source
	if _, serr := os.Stat(filepath.Join(boards, "corrupt", "kanban.db")); serr != nil {
		t.Fatalf("corrupt source db deleted despite failed verification: %v", serr)
	}
	if s.BoardExists("corrupt") {
		t.Fatal("corrupt board became a row")
	}
	boardsRows, _ := s.Boards(context.Background())
	if len(boardsRows) != 1 {
		t.Fatalf("boards = %+v, want just main", boardsRows)
	}
}

// reseedLegacyBoardDir replants one legacy board directory after the
// migration already ran: the poisoned state the fold used to mint
// (commit landed, the layout delete never did) re-runs the migration
// against rows that are already in the single database.
func reseedLegacyBoardDir(t *testing.T, boards, name, prefix string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(boards, name), 0755); err != nil {
		t.Fatal(err)
	}
	seedLegacy(t, filepath.Join(boards, name, "kanban.db"), prefix)
}

func TestMigrateReRunAfterCommittedFoldIsNoOp(t *testing.T) {
	home := tempHome(t)
	boards := datadirBoards(home)
	mainDSN := filepath.Join(home, "data", "kanban.db")
	if err := os.MkdirAll(filepath.Dir(mainDSN), 0755); err != nil {
		t.Fatal(err)
	}
	seedLegacy(t, mainDSN, "main")
	reseedLegacyBoardDir(t, boards, "legacy-crew", "crew")
	if err := os.WriteFile(filepath.Join(boards, "current"), []byte("legacy-crew\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// boot 1: migration commits the fold and retires the layout
	s, err := MountRegistry(home)
	if err != nil {
		t.Fatalf("first mount: %v", err)
	}
	s.Close()

	// the poisoned state: the layout delete never happened (crash,
	// detach on the wrong pooled connection, partial RemoveAll). The
	// boards/ dir reappears with the same legacy content. Every boot
	// from here re-enters the migration against committed rows.
	reseedLegacyBoardDir(t, boards, "legacy-crew", "crew")
	if err := os.WriteFile(filepath.Join(boards, "current"), []byte("legacy-crew\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// boot 2 must be a no-op SUCCESS: no UNIQUE refusal, no duplicated
	// rows, mount does not boot-fail
	s2, err := MountRegistry(home)
	if err != nil {
		t.Fatalf("re-run mount failed - migration not re-runnable: %v", err)
	}
	defer s2.Close()
	ctx := context.Background()
	names, err := s2.Boards(ctx)
	if err != nil || len(names) != 2 || names[1].Name != "legacy-crew" || !names[1].Current {
		t.Fatalf("re-run boards = %+v err %v", names, err)
	}
	if ct, err := s2.ListTasks(ctx, "legacy-crew", ""); err != nil || len(ct) != 2 {
		t.Fatalf("re-run crew tasks = %d err %v (duplicated rows?)", len(ct), err)
	}
	comments, err := s2.GetTaskComments(ctx, "legacy-crew", "crew1")
	if err != nil || len(comments) != 1 {
		t.Fatalf("re-run comments = %d err %v (duplicated rows?)", len(comments), err)
	}
	if events, err := s2.GetTaskEvents(ctx, "legacy-crew", "crew2"); err != nil || len(events) != 2 {
		t.Fatalf("re-run events = %d err %v (duplicated rows?)", len(events), err)
	}
	// and the re-run still retires the layout
	if _, serr := os.Stat(boards); !os.IsNotExist(serr) {
		t.Fatalf("re-run left the legacy boards dir: %v", serr)
	}
}

func TestMigrateKeepsJourneyLookalikeBoard(t *testing.T) {
	home := tempHome(t)
	boards := datadirBoards(home)
	mainDSN := filepath.Join(home, "data", "kanban.db")
	if err := os.MkdirAll(filepath.Dir(mainDSN), 0755); err != nil {
		t.Fatal(err)
	}
	seedLegacy(t, mainDSN, "main")
	// a LEGAL user board the old shape-signature (^j\d+) silently
	// destroyed: never backed up, never ledgered, gone with the layout
	if err := os.MkdirAll(filepath.Join(boards, "j2026-planning"), 0755); err != nil {
		t.Fatal(err)
	}
	seedLegacy(t, filepath.Join(boards, "j2026-planning", "kanban.db"), "plan")

	s, err := MountRegistry(home)
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	defer s.Close()
	ctx := context.Background()
	names, err := s.Boards(ctx)
	if err != nil || len(names) != 2 || names[1].Name != "j2026-planning" {
		t.Fatalf("boards = %+v err %v (lookalike board destroyed?)", names, err)
	}
	if planTasks, err := s.ListTasks(ctx, "j2026-planning", ""); err != nil || len(planTasks) != 2 {
		t.Fatalf("j2026-planning tasks = %d err %v", len(planTasks), err)
	}
	// data intact AND in the live registry: the dir folded, nothing
	// was shipped off to the archive
	if _, serr := os.Stat(filepath.Join(home, "data", "boards-junk", "j2026-planning")); !os.IsNotExist(serr) {
		t.Fatalf("lookalike board archived instead of migrated: %v", serr)
	}
	if _, serr := os.Stat(boards); !os.IsNotExist(serr) {
		t.Fatalf("legacy boards dir survives migration: %v", serr)
	}
}
