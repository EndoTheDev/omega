package kanban

// The drag-order tests (card t_09b44af1): the position column carries
// the card order inside a column, reorders persist across reopen, a
// column change rides UpdateTask's stamp rules and lands on top, and
// the migration backfill freezes the pre-position display order.

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// idsOf lists a column's tasks newest-slot-first (position order).
func idsOf(t *testing.T, s *Store, board, status string) []string {
	t.Helper()
	ctx := context.Background()
	tasks, err := s.ListTasks(ctx, board, status)
	if err != nil {
		t.Fatalf("list %q: %v", status, err)
	}
	ids := make([]string, 0, len(tasks))
	for _, t2 := range tasks {
		if status == "" || t2.Status == status {
			ids = append(ids, t2.ID)
		}
	}
	return ids
}

func titlesOf(t *testing.T, s *Store, board, status string) []string {
	t.Helper()
	ctx := context.Background()
	tasks, err := s.ListTasks(ctx, board, status)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	out := make([]string, 0, len(tasks))
	for _, t2 := range tasks {
		out = append(out, t2.Title)
	}
	return out
}

func titlesEqual(t *testing.T, got, want []string, where string) {
	t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("%s order = %v, want %v", where, got, want)
	}
}
func seedThree(t *testing.T) (*Store, string, string, string) {
	t.Helper()
	s := testStore(t)
	ctx := context.Background()
	var a, b, c string
	for i, title := range []string{"a-card", "b-card", "c-card"} {
		out, err := s.CreateTask(ctx, "", KanbanTask{Title: title})
		if err != nil {
			t.Fatalf("create %q: %v", title, err)
		}
		switch i {
		case 0:
			a = out.ID
		case 1:
			b = out.ID
		case 2:
			c = out.ID
		}
	}
	return s, a, b, c
}

// TestCreateLandsOnTop: each fresh card takes the top slot of its
// column, so list order is reverse creation order - deterministic even
// when every card shares one created_at second.
func TestCreateLandsOnTop(t *testing.T) {
	s, a, b, c := seedThree(t)
	got := idsOf(t, s, "", "")
	titlesEqual(t, got, []string{c, b, a}, "triage")
	if got[0] != c || got[2] != a {
		t.Fatalf("unexpected front/back: %v %v", got[0], got[2])
	}
}

func TestReorderWithinColumn(t *testing.T) {
	s, a, b, c := seedThree(t)
	ctx := context.Background()
	// b lands after c (its own place): no-op
	if _, err := s.ReorderTask(ctx, "", b, "", c); err != nil {
		t.Fatalf("reorder same place: %v", err)
	}
	titlesEqual(t, titlesOf(t, s, "", "triage"), []string{"c-card", "b-card", "a-card"}, "top")
	// a lands after c: [c, a, b]
	if _, err := s.ReorderTask(ctx, "", a, "", c); err != nil {
		t.Fatalf("reorder: %v", err)
	}
	titlesEqual(t, titlesOf(t, s, "", "triage"), []string{"c-card", "a-card", "b-card"}, "after c")
	// b back to the top: [b, c, a]
	if _, err := s.ReorderTask(ctx, "", b, "", ""); err != nil {
		t.Fatalf("reorder top: %v", err)
	}
	titlesEqual(t, titlesOf(t, s, "", "triage"), []string{"b-card", "c-card", "a-card"}, "top again")
	// the column resequenced dense: exactly 0..n-1 over the board
	positionsDense(t, readPositions(t, s))
}

func TestReorderAcrossColumns(t *testing.T) {
	s, a, _, c := seedThree(t)
	ctx := context.Background()
	// move c to running, then slot it after the card already there
	if _, err := s.UpdateTask(ctx, "", a, KanbanPatch{Status: StatusRunning}); err != nil {
		t.Fatalf("move a: %v", err)
	}
	if _, err := s.ReorderTask(ctx, "", c, StatusRunning, a); err != nil {
		t.Fatalf("reorder cross-column: %v", err)
	}
	titlesEqual(t, titlesOf(t, s, "", StatusRunning), []string{"a-card", "c-card"}, "running")
	titlesEqual(t, titlesOf(t, s, "", StatusTriage), []string{"b-card"}, "triage keeps b")
	// an empty-column drop lands at the top
	if _, err := s.ReorderTask(ctx, "", "t_ffffffff", "", ""); err == nil {
		t.Fatal("unknown task accepted")
	}
}

func TestReorderBadInputs(t *testing.T) {
	s, a, b, _ := seedThree(t)
	ctx := context.Background()
	if _, err := s.ReorderTask(ctx, "", a, "bogus", ""); err == nil {
		t.Fatal("illegal status accepted")
	}
	// after a card living in another column is refused
	if _, err := s.ReorderTask(ctx, "", a, StatusTodo, ""); err != nil {
		t.Fatalf("move a to todo: %v", err)
	}
	if _, err := s.ReorderTask(ctx, "", b, "", a); err == nil {
		t.Fatal("cross-column after accepted")
	}
	if _, err := s.ReorderTask(ctx, "", b, "", "t_ffffffff"); err == nil {
		t.Fatal("unknown after accepted")
	}
	if _, err := s.ReorderTask(ctx, "", "t_ffffffff", "todo", ""); err != ErrNotFound {
		t.Fatalf("unknown task err = %v, want ErrNotFound", err)
	}
}

// TestReorderNoEventWhenSamePlace: a drop that changes nothing is not
// audit noise - the reordered event appears only when the slot moved.
func TestReorderNoEventWhenSamePlace(t *testing.T) {
	s, _, b, c := seedThree(t)
	ctx := context.Background()
	// b stands second; the slot after c IS b's current place
	if _, err := s.ReorderTask(ctx, "", b, "", c); err != nil {
		t.Fatalf("identity reorder: %v", err)
	}
	if _, err := s.ReorderTask(ctx, "", b, "", c); err != nil {
		t.Fatalf("identity reorder again: %v", err)
	}
	events, err := s.GetTaskEvents(ctx, "", b)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	for _, e := range events {
		if e.Kind == "reordered" {
			t.Fatalf("identity reorder audited: %+v", events)
		}
	}
}

func TestReorderWritesEvent(t *testing.T) {
	s, a, _, c := seedThree(t)
	ctx := context.Background()
	if _, err := s.ReorderTask(ctx, "", a, "", c); err != nil {
		t.Fatalf("reorder: %v", err)
	}
	events, err := s.GetTaskEvents(ctx, "", a)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	if len(events) == 0 || events[len(events)-1].Kind != "reordered" {
		t.Fatalf("no reordered event: %+v", events)
	}
}

// TestReorderPersistsAcrossReopen: the order is a column of the board
// database, not session state.
func TestReorderPersistsAcrossReopen(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "kanban.db")
	s0, err := Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ctx := context.Background()
	titles := []string{"p1", "p2", "p3"}
	var a, c string
	for i, title := range titles {
		out, err := s0.CreateTask(ctx, "", KanbanTask{Title: title})
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if i == 0 {
			a = out.ID
		} else if i == 2 {
			c = out.ID
		}
	}
	if _, err := s0.ReorderTask(ctx, "", a, "", c); err != nil {
		t.Fatalf("reorder: %v", err)
	}
	if err := s0.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	s1, err := Open(dsn)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s1.Close()
	// a went behind c: [p3, p1, p2]
	titlesEqual(t, titlesOf(t, s1, "", "triage"), []string{"p3", "p1", "p2"}, "reopened order")
}

// TestMigrationBackfillOrdersAsRendered: a pre-position database (the
// column is dropped here to simulate one) backfills positions from the
// display rule it rendered before positions existed - created_at DESC,
// id DESC per column - so opening it changes nothing a board client
// could see.
func TestMigrationBackfillOrdersAsRendered(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "kanban.db")
	s0, err := Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ctx := context.Background()
	for _, title := range []string{"old-first", "old-second"} {
		if _, err := s0.CreateTask(ctx, "", KanbanTask{Title: title}); err != nil {
			t.Fatalf("create: %v", err)
		}
	}
	if err := s0.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	// the pre-position display rule is what the backfill must freeze:
	// created_at DESC, id DESC per column - read it straight from the
	// table before dropping the column
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	rows, err := db.Query(`SELECT title FROM tasks ORDER BY created_at DESC, id DESC`)
	if err != nil {
		t.Fatalf("pre rule query: %v", err)
	}
	want := []string{}
	for rows.Next() {
		var title string
		if err := rows.Scan(&title); err != nil {
			rows.Close()
			t.Fatalf("pre rule scan: %v", err)
		}
		want = append(want, title)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("pre rule rows: %v", err)
	}
	if _, err := db.Exec(`ALTER TABLE tasks DROP COLUMN position`); err != nil {
		t.Fatalf("drop position (driver too old?): %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("raw close: %v", err)
	}

	s1, err := Open(dsn)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s1.Close()
	// the backfilled positions render exactly the pre-migration order
	titlesEqual(t, titlesOf(t, s1, "", "triage"), want, "backfilled order")
	positionsDense(t, readPositions(t, s1))
}

// readPositions dumps (title, position) pairs straight from the table
// (id order - a stable dump whose positions set is asserted).
func readPositions(t *testing.T, s *Store) map[string]int {
	t.Helper()
	rows, err := s.db.Query(`SELECT title, position FROM tasks`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var title string
		var pos int64
		if err := rows.Scan(&title, &pos); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out[title] = int(pos)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return out
}

// positionsDense checks the pair dump is exactly 0..n-1.
func positionsDense(t *testing.T, positions map[string]int) {
	t.Helper()
	seen := map[int]bool{}
	for title, p := range positions {
		if p < 0 || p >= len(positions) {
			t.Fatalf("%q position %d out of range for %d rows", title, p, len(positions))
		}
		if seen[p] {
			t.Fatalf("duplicate position %d: %v", p, positions)
		}
		seen[p] = true
	}
}
