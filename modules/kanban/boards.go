package kanban

// The board registry (t_feae19d3, Endo ruling 2026-11-01): ALL boards
// live inside ONE sqlite database - <home>/data/kanban.db. The old
// layout, a directory of per-board sqlite files (<home>/data/boards/)
// plus a one-line `current` pointer, accumulated dead directories at
// journey scale and bought nothing: a board is a row in the `boards`
// table now, the current board is an is_current flag on that row, and
// every card, comment and event carries a board_id FK. Isolation stays
// QUERIES, not files: task ids are globally unique (t_<hex>), a task
// list is always filtered by board_id, and nothing renders cross-board
// unless asked (the cross-board-move ban from the reference law holds -
// switching board is a pointer USE (the is_current flag), never a card
// mutation).
//
// The legacy layout migrates once at boot (migrateLegacyBoards): the
// top-level kanban.db becomes board "main", every real board directory
// folds its rows into the one database under a verified count check,
// the pointer content becomes the is_current flag, and the whole
// boards/ directory is then deleted (deletion-over-archive; the count
// check is the safety net). Journey-minted boards (names matching
// junkBoardRE) do NOT migrate - they die with the old layout by
// design.

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/EndoTheDev/omega/modules/datadir"
)

// DefaultBoardName is the default board: on a fresh install it exists
// as the first row of the boards table, its data in the single
// <home>/data/kanban.db. The name is reserved.
const DefaultBoardName = "main"

// boardNameRE governs registry names: lowercase, starts alphanumeric,
// 1-64 chars, filesystem-safe (names were directory components in the
// legacy registry; the grammar keeps a stable, injection-free wire).
var boardNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// junkBoardRE is the journey-board signature: the recipes mint boards
// named j<recipe><timestamp>* (j77..., j78..., j86...). Legacy boards
// matching it are journey ephemerals - they never migrate (their cards
// die with the old layout by design) because the pinned journeys
// re-create the fixed-name journey boards as fresh rows.
var junkBoardRE = regexp.MustCompile(`^j\d+`)

// ValidBoardName reports whether name may name a board. "main" is
// legal (the default board); "" is not a board.
func ValidBoardName(name string) bool {
	return boardNameRE.MatchString(name)
}

// registryStaleDSN reports whether dsn carries the legacy pointer-era
// resolution: a path inside the boards directory (dirs/<name>/kanban.db
// was BoardDSN's answer for every non-main board).
func registryStaleDSN(home, dsn string) bool {
	rel, err := filepath.Rel(filepath.Join(datadir.Dir(home), "boards"), dsn)
	return err == nil && !strings.HasPrefix(rel, "..")
}

// ensureBoardsTable creates the boards table + the one-current partial
// index, and seeds the "main" row (current) when the table is fresh.
// The tasks rebuild in migrate() backfills board_id against main's
// id, so main must exist before any card row.
func ensureBoardsTable(db *sql.DB) error {
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS boards (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	name       TEXT NOT NULL UNIQUE,
	created_at TEXT NOT NULL,
	position   INTEGER NOT NULL DEFAULT 0,
	is_current INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_boards_one_current ON boards(is_current) WHERE is_current = 1;
`); err != nil {
		return err
	}
	_, err := db.Exec(`INSERT OR IGNORE INTO boards (name, created_at, position, is_current) VALUES (?, ?, 0, 1)`,
		DefaultBoardName, now())
	return err
}

// boardID resolves a board name to its row id: "" means the current
// board (the is_current row), a name means that row - unknown or
// illegal names error at the door, the registry never auto-creates.
func (s *Store) boardID(board string) (int64, error) {
	if board == "" {
		var id int64
		row := s.db.QueryRow(`SELECT id FROM boards WHERE is_current = 1 ORDER BY id LIMIT 1`)
		if err := row.Scan(&id); err != nil {
			return 0, fmt.Errorf("no current board row: %w", err)
		}
		return id, nil
	}
	if !ValidBoardName(board) {
		return 0, fmt.Errorf("illegal board name %q", board)
	}
	var id int64
	row := s.db.QueryRow(`SELECT id FROM boards WHERE name = ?`, board)
	if err := row.Scan(&id); err != nil {
		if err == sql.ErrNoRows {
			return 0, fmt.Errorf("no such board %q", board)
		}
		return 0, err
	}
	return id, nil
}

// BoardExists reports whether name is a row in the boards table.
// "/board <name>" and the capsule switch validate through it.
func (s *Store) BoardExists(name string) bool {
	if !ValidBoardName(name) {
		return false
	}
	var one int
	return s.db.QueryRow(`SELECT 1 FROM boards WHERE name = ?`, name).Scan(&one) == nil
}

// Boards implements BoardLister: the boards table, earliest created
// first (main was seeded with position 0), the current board flagged.
func (s *Store) Boards(ctx context.Context) ([]BoardInfo, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name, is_current FROM boards ORDER BY position, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BoardInfo{}
	for rows.Next() {
		var b BoardInfo
		var cur int
		if err := rows.Scan(&b.Name, &cur); err != nil {
			return nil, err
		}
		b.Current = cur == 1
		out = append(out, b)
	}
	return out, rows.Err()
}

// CurrentBoard names the current board (the is_current row). Falls
// back to "main" when no row claims the flag - main always exists and
// the board must answer, a missing flag is not a boot failure.
func (s *Store) CurrentBoard() (string, error) {
	var name string
	err := s.db.QueryRow(`SELECT name FROM boards WHERE is_current = 1 ORDER BY id LIMIT 1`).Scan(&name)
	if err == sql.ErrNoRows {
		return DefaultBoardName, nil
	}
	if err != nil {
		return "", err
	}
	return name, nil
}

// SetCurrentBoard points the home at the named board: the flag moves
// off the old row and onto this one inside one transaction (the
// partial unique index holds the invariant). Unknown boards refuse.
func (s *Store) SetCurrentBoard(name string) error {
	if name == "" || !ValidBoardName(name) {
		return fmt.Errorf("illegal board name %q", name)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE boards SET is_current = 0 WHERE is_current = 1`); err != nil {
		return err
	}
	res, err := tx.Exec(`UPDATE boards SET is_current = 1 WHERE name = ?`, name)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		return fmt.Errorf("no such board %q", name)
	}
	return tx.Commit()
}

// CreateBoardRow inserts the named board. Creating "main" or an
// existing board is an error.
func (s *Store) CreateBoardRow(name string) error {
	if name == DefaultBoardName {
		return fmt.Errorf("main is the default board and already exists")
	}
	if !ValidBoardName(name) {
		return fmt.Errorf("illegal board name %q", name)
	}
	_, err := s.db.Exec(`INSERT INTO boards (name, created_at, position, is_current)
		VALUES (?, ?, (SELECT COALESCE(MAX(position), 0) + 1 FROM boards), 0)`, name, now())
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return fmt.Errorf("board %q already exists", name)
	}
	return err
}

// DeleteBoardRow removes the named board's row, refusing: the default
// board, unknown boards, the current board (the flag would dangle),
// and any board that still holds tasks (archived tasks are still rows
// - there is no card deletion, so an emptiable board is one that never
// held a task). Comments and events ride the tasks, so the task count
// is the emptiness test. There is deliberately no force flag: the
// guards are the safety net the webui and the journeys rest on.
func (s *Store) DeleteBoardRow(name string) error {
	if name == DefaultBoardName {
		return fmt.Errorf("refusing to delete the default board")
	}
	if !ValidBoardName(name) {
		return fmt.Errorf("illegal board name %q", name)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id int64
	var cur int
	row := tx.QueryRow(`SELECT id, is_current FROM boards WHERE name = ?`, name)
	if err := row.Scan(&id, &cur); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("no such board %q", name)
		}
		return err
	}
	if cur == 1 {
		return fmt.Errorf("board %q is the current board - switch first (kanban board use)", name)
	}
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM tasks WHERE board_id = ?`, id).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("board %q still holds %d task(s) - move or delete them first", name, n)
	}
	if _, err := tx.Exec(`DELETE FROM boards WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// migrateLegacyBoards folds the legacy per-board-directory layout into
// the single database. Runs once when <home>/data/boards still exists;
// a home without it (or a second boot) is a strict no-op.
//
// Per board directory <name>: junk names (junkBoardRE) skip - their
// cards die with the layout by design; real names insert a board row
// and copy tasks/comments/events in with board_id set, then the
// destination counts are verified against the source counts before the
// board's rows are allowed to stand. The `current` pointer file content
// migrates to the is_current flag (junk targets fall back to main).
// After the fold-back verifies, the whole boards/ directory is deleted
// - the verified counts are the safety net, nothing is archived.
//
// Everything runs in ONE transaction: any failure rolls the entire
// fold-back and leaves the old layout untouched, so the next boot
// re-runs the migration cleanly (idempotent by construction).
func (s *Store) migrateLegacyBoards(home string) error {
	dir := filepath.Join(datadir.Dir(home), "boards")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // already migrated or a fresh home - no-op
		}
		return err
	}

	// the current pointer: the legacy file's content moves to the
	// is_current flag (missing/empty/garbage = main stays current)
	pointer := readPointerContent(filepath.Join(dir, "current"))

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// the legacy sources ride ATTACHed aliases for the whole migration
	// (one tx): sqlite refuses ATTACH/DETACH inside a transaction that
	// has already written, so every migrating board attaches UP FRONT
	// - before any write - and detaches after the commit. A failed
	// migration leaves the sources attached; the handle closes on the
	// mount failure path, and an attached read-only source is harmless
	// to the same handle otherwise (detached on Close).
	// alias is index-derived, never name-derived: board names may
	// carry characters sqlite parses as operators inside an
	// unquoted alias (legacy-crew -> legacy_legacy-crew reads as
	// arithmetic and reds "no such column").
	attach := map[string]string{} // alias -> dsn
	var aliases []string
	boards := []struct{ name, alias string }{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dsn := filepath.Join(dir, e.Name(), "kanban.db")
		if _, statErr := os.Stat(dsn); statErr != nil {
			continue // a directory without kanban.db is not a board
		}
		if e.Name() == DefaultBoardName || !ValidBoardName(e.Name()) || junkBoardRE.MatchString(e.Name()) {
			// journey junk: never migrates, dies with the layout
			continue
		}
		alias := fmt.Sprintf("legacy%d", len(aliases))
		if _, ok := attach[alias]; !ok { // distinct dir names, belt for dupes
			attach[alias] = dsn
			aliases = append(aliases, alias)
			boards = append(boards, struct{ name, alias string }{e.Name(), alias})
			if _, err := tx.Exec(`ATTACH DATABASE ? AS ` + alias, dsn); err != nil {
				return fmt.Errorf("migrate board %q: attach: %w", e.Name(), err)
			}
		}
	}

	position := 1
	for _, b := range boards {
		res, err := tx.Exec(`INSERT INTO boards (name, created_at, position, is_current) VALUES (?, ?, ?, 0)`,
			b.name, now(), position)
		if err != nil {
			return fmt.Errorf("migrate board %q: %w", b.name, err)
		}
		position++
		id, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("migrate board %q: %w", b.name, err)
		}
		if err := copyLegacyBoard(b.alias, b.name, tx, id); err != nil {
			return err
		}
		if err := verifyBoardCopy(b.alias, b.name, tx, id); err != nil {
			return err
		}
	}

	// pointer -> is_current (only a real, migrated board may claim it)
	if pointer != "" && s.boardRowIDTx(tx, pointer) != 0 {
		if _, err := tx.Exec(`UPDATE boards SET is_current = 0 WHERE is_current = 1`); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE boards SET is_current = 1 WHERE name = ?`, pointer); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	for _, alias := range aliases {
		if _, err := s.db.Exec(`DETACH DATABASE ` + alias); err != nil {
			return fmt.Errorf("migrate: detach %s: %w", alias, err)
		}
	}
	// the old layout is dead: delete the directory AND every legacy
	// sqlite file inside (migrated boards + the junk that never moves)
	return os.RemoveAll(dir)
}

// boardRowIDTx resolves a board name inside the open migration tx, 0
// when the name does not name a row (junk names refuse; main is a
// seeded row like any other).
func (s *Store) boardRowIDTx(tx *sql.Tx, pointer string) int64 {
	if !ValidBoardName(pointer) || junkBoardRE.MatchString(pointer) {
		return 0
	}
	var id int64
	if err := tx.QueryRow(`SELECT id FROM boards WHERE name = ?`, pointer).Scan(&id); err != nil {
		return 0
	}
	return id
}

// readPointerContent reads the legacy pointer file, "" when unreadable,
// empty, or not a board name (a main is a valid board name).
func readPointerContent(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	name := strings.TrimSpace(string(data))
	if !ValidBoardName(name) {
		return ""
	}
	return name
}

// copyLegacyBoard copies one legacy board db's rows into the single
// database (board_id stamped on every row) inside the migration tx.
// The source rides an already-ATTACHed alias - INSERT INTO ... SELECT
// is the file-to-file table copy the sqlite driver ships.
func copyLegacyBoard(alias, name string, tx *sql.Tx, boardID int64) error {
	copies := []struct{ sql, what string }{
		{`INSERT INTO tasks (id, title, body, assignee, status, session_id, source, created_at, started_at, completed_at, board_id)
			SELECT id, title, body, assignee, status, session_id, source, created_at, started_at, completed_at, ? FROM ` + alias + `.tasks`, "tasks"},
		{`INSERT INTO task_comments (task_id, author, body, created_at, board_id)
			SELECT task_id, author, body, created_at, ? FROM ` + alias + `.task_comments`, "comments"},
		{`INSERT INTO task_events (task_id, kind, detail, created_at, board_id)
			SELECT task_id, kind, detail, created_at, ? FROM ` + alias + `.task_events`, "events"},
	}
	for _, c := range copies {
		if _, err := tx.Exec(c.sql, boardID); err != nil {
			return fmt.Errorf("migrate board %q: copy %s: %w", name, c.what, err)
		}
	}
	return nil
}

// verifyBoardCopy demands the migrated row counts match the source
// exactly: tasks, comments AND events. The source is already ATTACHed;
// a mismatch fails the whole migration (one tx - everything rolls
// back, the old layout survives untouched).
func verifyBoardCopy(alias, name string, tx *sql.Tx, boardID int64) error {
	type pair struct{ sql, what string }
	pairs := []pair{
		{"SELECT (SELECT COUNT(*) FROM " + alias + ".tasks), (SELECT COUNT(*) FROM tasks WHERE board_id = ?)", "task"},
		{"SELECT (SELECT COUNT(*) FROM " + alias + ".task_comments), (SELECT COUNT(*) FROM task_comments WHERE board_id = ?)", "comment"},
		{"SELECT (SELECT COUNT(*) FROM " + alias + ".task_events), (SELECT COUNT(*) FROM task_events WHERE board_id = ?)", "event"},
	}
	for _, p := range pairs {
		var src, dst int
		if err := tx.QueryRow(p.sql, boardID).Scan(&src, &dst); err != nil {
			return fmt.Errorf("migrate board %q: verify: %w", name, err)
		}
		if src != dst {
			return fmt.Errorf("migrate board %q: %s count %d != source %d - migration aborted, old layout kept", name, p.what, dst, src)
		}
	}
	return nil
}

// MountRegistry opens the home's single kanban database (schema
// ensured, main seeded), folds any legacy per-board directories in,
// and returns the store. The CLI opens the same way. A migration
// failure is a startup error - a board that silently dropped rows is
// worse than a crash.
func MountRegistry(home string) (*Store, error) {
	return MountRegistryDSN(home, datadir.DBPath(home, "kanban.db"))
}

// MountRegistryDSN is MountRegistry against an explicit dsn (custom
// db_path configs ride their own database file; the legacy fold-back
// still merges into the store that the process actually serves).
func MountRegistryDSN(home, dsn string) (*Store, error) {
	s, err := Open(dsn)
	if err != nil {
		return nil, fmt.Errorf("kanban store: %w", err)
	}
	if home != "" {
		if err := s.migrateLegacyBoards(home); err != nil {
			s.Close()
			return nil, fmt.Errorf("kanban board registry: %w", err)
		}
	}
	return s, nil
}
