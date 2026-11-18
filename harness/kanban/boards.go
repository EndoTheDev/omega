package kanban

// The board registry: ALL boards live inside ONE sqlite database. Recipe-pinned journey
// boards (j78-a, j78-b, j86-b - pinnedJourneyBoards) never migrate: the recipes
// re-create them as fresh rows.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/EndoTheDev/omega/harness/datadir"
)

// DefaultBoardName is the name the seeded first board carries on a fresh install.
const DefaultBoardName = "main"

// errNoBoards is the legal EMPTY registry (0.17.3 make-over.
var errNoBoards = errors.New("no boards - create one first")

// boardNameRE governs registry names.
var boardNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// pinnedJourneyBoards is the EXACT set of board names the journey recipes pin
// (78-board-switch.sh: j78-a, j78-b; 86-dock-round5: j86-b) - fixed names the recipes
// re-create as fresh rows on every run, so a legacy row of the same name is never
// migrated. The skip was once a signature regex (^j\d+), which also ate legal user
// boards like "j2026-planning": never again match by shape, match by pinned name only.
var pinnedJourneyBoards = map[string]bool{
	"j78-a": true,
	"j78-b": true,
	"j86-b": true,
}

// isPinnedJourneyBoard reports whether name is a recipe-pinned journey
// board that must never migrate (the recipes re-create it fresh).
func isPinnedJourneyBoard(name string) bool {
	return pinnedJourneyBoards[name]
}

// ValidBoardName reports whether name may name a board.
func ValidBoardName(name string) bool {
	return boardNameRE.MatchString(name)
}

// registryStaleDSN reports whether dsn carries the legacy pointer-era resolution.
func registryStaleDSN(home, dsn string) bool {
	rel, err := filepath.Rel(filepath.Join(datadir.Dir(home), "boards"), dsn)
	return err == nil && !strings.HasPrefix(rel, "..")
}

// ensureBoardsTable creates the boards table + the one-current partial index, and seeds
// the "main" row once. Zero boards is a legal state since 0.17.3: a home whose user
// deleted every board must not have "main" resurrected under it at the next start, and
// the seed is the only insert the registry performs by itself. The tasks rebuild in
// migrate backfills board_id against main's id, so the fresh-database seed must still
// land before any card row.
func ensureBoardsTable(db *sql.DB) error {
	var born int // 0 = the boards table did not exist before this call
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'boards'`).Scan(&born); err != nil {
		return err
	}
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
	if born > 0 {
		return nil // an existing registry owns its rows - the seed already ran (or was deleted on purpose)
	}
	_, err := db.Exec(`INSERT OR IGNORE INTO boards (name, created_at, position, is_current) VALUES (?, ?, 0, 1)`,
		DefaultBoardName, now())
	return err
}

// boardID resolves a board name to its row id: "" means the current board (the
// is_current row), a name means that row - unknown or illegal names error at the door,
// the registry never auto-creates. Reads ride the statement surface (q), so a batch
// clone's board resolution executes inside the batch transaction; the WRITE ops below
// keep db directly (Begin/ATTACH cannot nest).
func (s *Store) boardID(board string) (int64, error) {
	bg := context.Background()
	if board == "" {
		var id int64
		row := s.q.QueryRowContext(bg, `SELECT id FROM boards WHERE is_current = 1 ORDER BY id LIMIT 1`)
		if err := row.Scan(&id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return 0, errNoBoards
			}
			return 0, fmt.Errorf("no current board row: %w", err)
		}
		return id, nil
	}
	if !ValidBoardName(board) {
		return 0, fmt.Errorf("illegal board name %q", board)
	}
	var id int64
	row := s.q.QueryRowContext(bg, `SELECT id FROM boards WHERE name = ?`, board)
	if err := row.Scan(&id); err != nil {
		if err == sql.ErrNoRows {
			return 0, fmt.Errorf("no such board %q", board)
		}
		return 0, err
	}
	return id, nil
}

// BoardExists reports whether name is a row in the boards table.
func (s *Store) BoardExists(name string) bool {
	if !ValidBoardName(name) {
		return false
	}
	var one int
	return s.q.QueryRowContext(context.Background(), `SELECT 1 FROM boards WHERE name = ?`, name).Scan(&one) == nil
}

// Boards implements BoardLister.
func (s *Store) Boards(ctx context.Context) ([]BoardInfo, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT name, is_current FROM boards ORDER BY position, id`)
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

// CurrentBoard names the current board (the is_current row). When no row claims the
// flag the earliest board answers instead - a registry that lost its pointer still has
// an order, and the view must name something.
func (s *Store) CurrentBoard() (string, error) {
	bg := context.Background()
	var name string
	err := s.q.QueryRowContext(bg, `SELECT name FROM boards WHERE is_current = 1 ORDER BY id LIMIT 1`).Scan(&name)
	if err == nil {
		return name, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if err := s.q.QueryRowContext(bg, `SELECT name FROM boards ORDER BY position, id LIMIT 1`).Scan(&name); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil // no boards at all: the legal empty registry
		}
		return "", err
	}
	return name, nil
}

// SetCurrentBoard points the home at the named board: the flag moves off the old row
// and onto this one inside one transaction (the partial unique index holds the
// invariant).
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

// CreateBoardRow inserts the named board. A duplicate name errors. A board created into
// an EMPTY registry claims the pointer: with zero boards there is no current row, and
// the first board back is the one the views must open on.
func (s *Store) CreateBoardRow(name string) error {
	if !ValidBoardName(name) {
		return fmt.Errorf("illegal board name %q", name)
	}
	_, err := s.db.Exec(`INSERT INTO boards (name, created_at, position, is_current)
		VALUES (?, ?, (SELECT COALESCE(MAX(position), 0) + 1 FROM boards),
		        (SELECT CASE WHEN COUNT(*) = 0 THEN 1 ELSE 0 END FROM boards))`, name, now())
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return fmt.Errorf("board %q already exists", name)
	}
	return err
}

// RenameBoardRow renames a board (0.17.3. The new name obeys the same grammar and must
// be free; the row keeps its id, so every card, comment and event stays where it was
// and the is_current flag follows the row.
func (s *Store) RenameBoardRow(oldName, newName string) error {
	if !ValidBoardName(oldName) {
		return fmt.Errorf("illegal board name %q", oldName)
	}
	if !ValidBoardName(newName) {
		return fmt.Errorf("illegal board name %q", newName)
	}
	if oldName == newName {
		return nil
	}
	res, err := s.db.Exec(`UPDATE boards SET name = ? WHERE name = ?`, newName, oldName)
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return fmt.Errorf("board %q already exists", newName)
	}
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		return fmt.Errorf("no such board %q", oldName)
	}
	return nil
}

// DeleteBoardRow removes a board and everything resting on it. A live card (any other
// status) refuses the delete by name and count - the archive lane is the one place a
// card can wait without blocking its board's removal.
func (s *Store) DeleteBoardRow(name string) error {
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
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("no such board %q", name)
		}
		return err
	}
	var live int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM tasks WHERE board_id = ? AND status != ?`, id, StatusArchived).Scan(&live); err != nil {
		return err
	}
	if live > 0 {
		return fmt.Errorf("board %q still holds %d live card(s) - archive or delete them first", name, live)
	}
	for _, stmt := range []string{
		`DELETE FROM task_events WHERE board_id = ?`,
		`DELETE FROM task_comments WHERE board_id = ?`,
		`DELETE FROM tasks WHERE board_id = ?`,
		`DELETE FROM boards WHERE id = ?`,
	} {
		if _, err := tx.Exec(stmt, id); err != nil {
			return err
		}
	}
	if cur == 1 {
		var next string
		err := tx.QueryRow(`SELECT name FROM boards ORDER BY position, id LIMIT 1`).Scan(&next)
		switch {
		case err == nil:
			if _, err := tx.Exec(`UPDATE boards SET is_current = 1 WHERE name = ?`, next); err != nil {
				return err
			}
		case errors.Is(err, sql.ErrNoRows):
		default:
			return err
		}
	}
	return tx.Commit()
}

// migrateLegacyBoards folds the legacy per-board-directory layout into the single. Per
// board directory <name>: pinned journey boards, illegal names and a stray main dir
// skip - they are ARCHIVED into <home>/data/boards-junk (recoverable), never deleted
// silently. A committed-before-failure state is equally recoverable: every next boot
// re-verifies against the source and deletes the layout (idempotent).
func (s *Store) migrateLegacyBoards(home string) error {
	dir := filepath.Join(datadir.Dir(home), "boards")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // already migrated or a fresh home - no-op
		}
		return err
	}

	pointer := readPointerContent(filepath.Join(dir, "current"))

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// the legacy sources ride ATTACHed aliases for the whole migration (one tx). alias is
	// index-derived, never name-derived: board names may carry characters sqlite parses as
	// operators inside an unquoted alias (legacy-crew -> legacy_legacy-crew reads as
	// arithmetic and reds "no such column").
	attach := map[string]string{} // alias -> dsn
	var aliases []string
	skipped := []string{} // legacy dirs the fold leaves behind: archived, never deleted
	boards := []struct{ name, alias string }{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dsn := filepath.Join(dir, e.Name(), "kanban.db")
		if _, statErr := os.Stat(dsn); statErr != nil {
			continue // a directory without kanban.db is not a board
		}
		if e.Name() == DefaultBoardName || !ValidBoardName(e.Name()) || isPinnedJourneyBoard(e.Name()) {
			// pinned journey boards re-create fresh; illegal names and a stray main dir are not
			// foldable.
			skipped = append(skipped, e.Name())
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
		// re-run aware (commit before the layout delete is the one way the migration can
		// half-land: a detach that runs on the wrong pooled connection, a crash, a partial
		// RemoveAll): when the board row already exists, the fold landed wholesale inside the
		// earlier tx - copy NOTHING (plain inserts would die on the unique ids and duplicate
		// the events), just verify that what stands matches the source and carry on.
		id := s.boardRowIDTx(tx, b.name)
		if id == 0 {
			res, err := tx.Exec(`INSERT INTO boards (name, created_at, position, is_current) VALUES (?, ?, ?, 0)`,
				b.name, now(), position)
			if err != nil {
				return fmt.Errorf("migrate board %q: %w", b.name, err)
			}
			id, err = res.LastInsertId()
			if err != nil {
				return fmt.Errorf("migrate board %q: %w", b.name, err)
			}
			if err := copyLegacyBoard(b.alias, b.name, tx, id); err != nil {
				return err
			}
		}
		position++
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
		// ATTACH is connection-scoped. The error is cosmetic anyway - Close drops every
		// attached alias - and a detach failure must never poison an already-committed fold:
		// the migration below is re-runnable, this one refusal is not.
		_, _ = s.db.Exec(`DETACH DATABASE ` + alias)
	}
	archive := filepath.Join(datadir.Dir(home), "boards-junk")
	if err := archiveSkippedBoardDirs(dir, archive, skipped); err != nil {
		return err
	}
	// the old layout is dead: delete the directory AND every migrated board's sqlite.
	return os.RemoveAll(dir)
}

// archiveSkippedBoardDirs moves every legacy directory the fold deliberately did not
// migrate from the boards dir into the archive dir <home>/data/boards-junk, so the
// RemoveAll that retires the old layout can never take a skipped dir's data with it. A
// collision lands under a distinct name instead of overwriting.
func archiveSkippedBoardDirs(dir, archive string, names []string) error {
	if len(names) == 0 {
		return nil
	}
	if err := os.MkdirAll(archive, 0755); err != nil {
		return err
	}
	for _, name := range names {
		dst := filepath.Join(archive, name)
		if _, err := os.Stat(dst); err == nil {
			dst = filepath.Join(archive, name+"."+now())
		}
		if err := os.Rename(filepath.Join(dir, name), dst); err != nil {
			return fmt.Errorf("migrate: archive %q: %w", name, err)
		}
	}
	return nil
}

// boardRowIDTx resolves a board name inside the open migration tx, 0 when the name.
func (s *Store) boardRowIDTx(tx *sql.Tx, pointer string) int64 {
	if !ValidBoardName(pointer) || isPinnedJourneyBoard(pointer) {
		return 0
	}
	var id int64
	if err := tx.QueryRow(`SELECT id FROM boards WHERE name = ?`, pointer).Scan(&id); err != nil {
		return 0
	}
	return id
}

// readPointerContent reads the legacy pointer file, "" when unreadable, empty, or not.
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

// copyLegacyBoard copies one legacy board db's rows into the single database.
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

// verifyBoardCopy demands the migrated row counts match the source exactly.
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

// MountRegistry opens the home's single kanban database (schema ensured, main. A
// migration failure is a startup error - a board that silently dropped rows is worse
// than a crash.
func MountRegistry(home string) (*Store, error) {
	return MountRegistryDSN(home, datadir.DBPath(home, "kanban.db"))
}

// MountRegistryDSN is MountRegistry against an explicit dsn (custom db_path configs
// ride their own database file.
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
