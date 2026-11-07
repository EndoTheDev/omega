package tools

// Regression pins for card t_b9f74bea: a directory that failed the
// walk used to vanish as "[skipped N walk errors]" - no name, no
// reason. The walk now carries the path + cause into the result, and
// the no-match early-return keeps the detail instead of swallowing
// it. The walker is injectable (walkDirFn): a genuinely ACL-denied
// directory is not reproducible portably inside a temp tree, so the
// test injects the denied-dir error shape the Windows lane produces.
// Tests restore walkDirFn via defer and never leave it swapped (no
// test in the package uses t.Parallel).

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubDirEntry satisfies fs.DirEntry for the injected walker; the
// search callbacks only touch it for the root / non-error paths.
type stubDirEntry struct{}

func (stubDirEntry) Name() string               { return "stub" }
func (stubDirEntry) IsDir() bool                { return true }
func (stubDirEntry) Type() fs.FileMode          { return fs.ModeDir }
func (stubDirEntry) Info() (fs.FileInfo, error) { return nil, fs.ErrNotExist }

// withInjectedDeniedDir swaps walkDirFn for a walker that first feeds
// fn one denied-directory error per name, then runs the real walk.
// Returns a restore func.
func withInjectedDeniedDir(names ...string) func() {
	orig := walkDirFn
	walkDirFn = func(root string, fn fs.WalkDirFunc) error {
		for _, name := range names {
			path := filepath.Join(root, name)
			denied := &fs.PathError{Op: "ReadDir", Path: path, Err: fs.ErrPermission}
			if err := fn(path, stubDirEntry{}, denied); err != nil {
				return err
			}
		}
		return orig(root, fn)
	}
	return func() { walkDirFn = orig }
}

// TestGlobWalkErrorsListPathAndCause: a denied directory appears in
// the result with its path and its cause, next to the matches the
// walk could still read.
func TestGlobWalkErrorsListPathAndCause(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hit.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	restore := withInjectedDeniedDir("locked")
	defer restore()

	p := NewSearchProvider(nil)
	out, err := p.RunGlob(context.Background(), map[string]any{"pattern": "**/*.txt", "path": dir})
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if !strings.Contains(out, "hit.txt") {
		t.Fatalf("glob lost the matchable file: %q", out)
	}
	if !strings.Contains(out, "[walk error: ") {
		t.Fatalf("glob has no walk-error detail: %q", out)
	}
	if !strings.Contains(out, "locked") || !strings.Contains(out, "permission denied") {
		t.Fatalf("walk detail missing path/cause: %q", out)
	}
	if strings.Contains(out, "skipped") {
		t.Fatalf("bare walk-error count survived: %q", out)
	}
}

// TestGlobWalkErrorSurvivesNoMatches: a denied directory is not
// swallowed by the "no matches" early return - the vanished tree is
// exactly where the detail matters.
func TestGlobWalkErrorSurvivesNoMatches(t *testing.T) {
	dir := t.TempDir()
	restore := withInjectedDeniedDir("locked")
	defer restore()

	p := NewSearchProvider(nil)
	out, err := p.RunGlob(context.Background(), map[string]any{"pattern": "*.zzz", "path": dir})
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if !strings.Contains(out, "[walk error: ") || !strings.Contains(out, "locked") {
		t.Fatalf("no-match glob dropped the walk error: %q", out)
	}
}

// TestGrepWalkErrorsListPathAndCause: the grep walk carries the same
// detail (one fixture, two tools - the class, not just one tool).
func TestGrepWalkErrorsListPathAndCause(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "quiet.txt"), []byte("nothing here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	restore := withInjectedDeniedDir("locked")
	defer restore()

	p := NewSearchProvider(nil)
	out, err := p.RunGrep(context.Background(), map[string]any{"pattern": "nothing", "path": dir, "output_mode": "content"})
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	if !strings.Contains(out, "quiet.txt:1:") || !strings.Contains(out, "[walk error: ") {
		t.Fatalf("grep walk detail wrong: %q", out)
	}
}

// TestGlobWalkErrorDetailCap: the detail lines are capped with an
// honest "...and N more" tail, so a pathological tree cannot flood
// the result.
func TestGlobWalkErrorDetailCap(t *testing.T) {
	dir := t.TempDir()
	names := make([]string, 0, 13)
	for i := range 13 {
		names = append(names, fmt.Sprintf("locked%d", i))
	}
	restore := withInjectedDeniedDir(names...)
	defer restore()

	p := NewSearchProvider(nil)
	out, err := p.RunGlob(context.Background(), map[string]any{"pattern": "*.go", "path": dir})
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	details := strings.Count(out, "[walk error: ")
	if details != walkErrorDetailCap {
		t.Fatalf("walk-error detail lines = %d, want %d: %q", details, walkErrorDetailCap, out)
	}
	if !strings.Contains(out, "[...and 3 more walk errors]") {
		t.Fatalf("missing the honest tail: %q", out)
	}
}
