package tools

// The search tools' self-check (card t_db98ebfe): a seeded tree,
// both tools run for real against it. Every test builds its own
// scratch tree and never touches anything outside its t.TempDir.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/datadir"
)

// seedTree writes a small source tree into a fresh temp dir:
//
//	main.go   : "alpha beta\nsearch me please\n"
//	notes.md  : "search me please\nsecond note\n"
//	sub/keep.log : "search me please\n"
//	bin.dat   : binary bytes (NUL-filled head, "search" also present)
func seedTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("seed dirs: %v", err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("seed %s: %v", rel, err)
		}
	}
	write("main.go", "alpha beta\nsearch me please\n")
	write("notes.md", "search me please\nsecond note\n")
	write("sub/keep.log", "search me please\n")
	write("bin.dat", "search\x00 binary head\nsearch again")
	return root
}

func provider() *SearchProvider { return NewSearchProvider(nil) }

func TestGlobRegexp(t *testing.T) {
	cases := []struct {
		pattern string
		input   string
		match   bool
	}{
		{"*.go", "main.go", true},
		{"*.go", "sub/main.go", false},
		{"**/*.md", "sub/deep/n.md", true},
		{"sub/*.md", "sub/n.md", true},
		{"sub/*.md", "other/n.md", false},
		{"?.log", "a.log", true},
		{"?.log", "ab.log", false},
		{"**/*.log", "sub/keep.log", true},
	}
	for _, tc := range cases {
		re, err := globToRegexp(tc.pattern)
		if err != nil {
			t.Fatalf("globToRegexp(%q): %v", tc.pattern, err)
		}
		if got := re.MatchString(tc.input); got != tc.match {
			t.Errorf("pattern %q on %q = %v, want %v", tc.pattern, tc.input, got, tc.match)
		}
	}
}

func TestGrepFilesWithMatches(t *testing.T) {
	root := seedTree(t)
	got, err := provider().RunGrep(context.Background(), map[string]any{
		"pattern": "search me",
		"path":    root,
	})
	if err != nil {
		t.Fatalf("RunGrep: %v", err)
	}
	for _, want := range []string{"main.go", "notes.md", "keep.log"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
	if !strings.Contains(got, "skipped 1 binary") {
		t.Errorf("the binary skip must be counted:\n%s", got)
	}
}

func TestGrepContentModeWithContext(t *testing.T) {
	root := seedTree(t)
	got, err := provider().RunGrep(context.Background(), map[string]any{
		"pattern":     "alpha",
		"path":        root,
		"output_mode": "content",
		"context":     float64(1),
	})
	if err != nil {
		t.Fatalf("RunGrep: %v", err)
	}
	if !strings.Contains(got, "main.go:1: alpha beta") {
		t.Errorf("missing match row:\n%s", got)
	}
	if !strings.Contains(got, "main.go:2: search me please") {
		t.Errorf("missing context row:\n%s", got)
	}
}

func TestGrepCountMode(t *testing.T) {
	root := seedTree(t)
	got, err := provider().RunGrep(context.Background(), map[string]any{
		"pattern":     "search",
		"path":        root,
		"output_mode": "count",
		"glob":        "**/*.md",
	})
	if err != nil {
		t.Fatalf("RunGrep: %v", err)
	}
	if !strings.Contains(got, "notes.md: 1") {
		t.Errorf("count row missing:\n%s", got)
	}
	if strings.Contains(got, "main.go") {
		t.Errorf("glob filter leaked:\n%s", got)
	}
}

func TestGrepErrors(t *testing.T) {
	p := provider()
	root := seedTree(t)
	if _, err := p.RunGrep(context.Background(), map[string]any{"pattern": ""}); err == nil {
		t.Error("empty pattern must error")
	}
	if _, err := p.RunGrep(context.Background(), map[string]any{"pattern": "a(b"}); err == nil {
		t.Error("bad regexp must error")
	}
	if _, err := p.RunGrep(context.Background(), map[string]any{"pattern": "x", "path": filepath.Join(root, "missing")}); err == nil {
		t.Error("missing root must error")
	}
	if _, err := p.RunGrep(context.Background(), map[string]any{"pattern": "x", "output_mode": "bogus"}); err == nil {
		t.Error("bad output_mode must error")
	}
	if _, err := p.RunGrep(context.Background(), map[string]any{"pattern": "x", "context": "one"}); err == nil {
		t.Error("non-numeric context must error")
	}
}

func TestGrepCapsAreHonest(t *testing.T) {
	// A tree with more matching files than the cap: the first
	// grepMaxFiles rows print and the cap note appears.
	root := t.TempDir()
	for i := 0; i < grepMaxFiles+10; i++ {
		p := filepath.Join(root, "f"+itoa(i)+".txt")
		if err := os.WriteFile(p, []byte("needle\n"), 0o644); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	got, err := provider().RunGrep(context.Background(), map[string]any{"pattern": "needle", "path": root})
	if err != nil {
		t.Fatalf("RunGrep: %v", err)
	}
	if !strings.Contains(got, "f0.txt") || !strings.Contains(got, "f"+itoa(grepMaxFiles-1)+".txt") {
		t.Errorf("expected the first grepMaxFiles rows, got:\n%s", got)
	}
	rows := 0
	for _, line := range strings.Split(got, "\n") {
		if strings.HasSuffix(line, ".txt") {
			rows++
		}
	}
	if rows != grepMaxFiles {
		t.Errorf("want exactly %d rows, got %d", grepMaxFiles, rows)
	}
	if !strings.Contains(got, "result cap") {
		t.Errorf("truncation note missing:\n%s", got)
	}
}

func TestGrepSingleFileRoot(t *testing.T) {
	root := seedTree(t)
	got, err := provider().RunGrep(context.Background(), map[string]any{
		"pattern":     "beta",
		"path":        filepath.Join(root, "main.go"),
		"output_mode": "content",
	})
	if err != nil {
		t.Fatalf("RunGrep: %v", err)
	}
	if !strings.Contains(got, "beta") {
		t.Errorf("single-file search failed:\n%s", got)
	}
}

func TestGrepRunWorkspaceRooting(t *testing.T) {
	root := seedTree(t)
	ctx := core.WithRunWorkspace(context.Background(), filepath.Join(root, "sub"), "")
	got, err := provider().RunGrep(ctx, map[string]any{
		"pattern":     "search me",
		"output_mode": "count",
	})
	if err != nil {
		t.Fatalf("RunGrep: %v", err)
	}
	if !strings.Contains(got, "keep.log: 1") {
		t.Errorf("relative root must ride the run workspace:\n%s", got)
	}
	if strings.Contains(got, "main.go") {
		t.Errorf("workspace rooting leaked outside the sub dir:\n%s", got)
	}
}

func TestGrepGuardDeniesDBPaths(t *testing.T) {
	// A real guard over a seeded fake instance db: the db file itself
	// (and its WAL/SHM siblings) must never be searched even when the
	// pattern matches content inside them.
	root := t.TempDir()
	dbPath := filepath.Join(root, "home", "data", "omega.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatalf("seed dir: %v", err)
	}
	for _, f := range []string{dbPath, dbPath + "-wal"} {
		if err := os.WriteFile(f, []byte("needle inside the db\n"), 0o644); err != nil {
			t.Fatalf("seed db: %v", err)
		}
	}
	okay := filepath.Join(root, "home", "plain.txt")
	if err := os.WriteFile(okay, []byte("needle\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	p := NewSearchProvider(datadir.NewGuard(dbPath))
	got, err := p.RunGrep(context.Background(), map[string]any{"pattern": "needle", "path": filepath.Join(root, "home")})
	if err != nil {
		t.Fatalf("RunGrep: %v", err)
	}
	if !strings.Contains(got, "plain.txt") {
		t.Errorf("plain file must still be found:\n%s", got)
	}
	if strings.Contains(got, "omega.db") {
		t.Errorf("the db family must be skipped:\n%s", got)
	}
	if !strings.Contains(got, "skipped 2 the instance database's door") {
		t.Errorf("the skip must be counted honestly:\n%s", got)
	}

	// The db itself as root is a hard refusal.
	if _, err := p.RunGrep(context.Background(), map[string]any{"pattern": "needle", "path": dbPath}); err == nil {
		t.Error("searching the instance db root must be refused")
	}
}

func TestGlobBasics(t *testing.T) {
	root := seedTree(t)
	got, err := provider().RunGlob(context.Background(), map[string]any{"pattern": "**/*.log", "path": root})
	if err != nil {
		t.Fatalf("RunGlob: %v", err)
	}
	if !strings.Contains(got, "sub/keep.log") {
		t.Errorf("** missing:\n%s", got)
	}

	got, err = provider().RunGlob(context.Background(), map[string]any{"pattern": "*.go", "path": root})
	if err != nil {
		t.Fatalf("RunGlob: %v", err)
	}
	if !strings.Contains(got, "main.go") || strings.Contains(got, "notes.md") {
		t.Errorf("top-level filter wrong:\n%s", got)
	}

	got, err = provider().RunGlob(context.Background(), map[string]any{"pattern": "*", "path": root, "type": "dirs"})
	if err != nil {
		t.Fatalf("RunGlob: %v", err)
	}
	if got != "sub" {
		t.Errorf("dirs filter: got %q, want \"sub\"", got)
	}
}

func TestGlobErrorsAndCaps(t *testing.T) {
	p := provider()
	root := seedTree(t)
	if _, err := p.RunGlob(context.Background(), map[string]any{"pattern": "", "path": root}); err == nil {
		t.Error("empty pattern must error")
	}
	if _, err := p.RunGlob(context.Background(), map[string]any{"pattern": "*", "path": root, "type": "bogus"}); err == nil {
		t.Error("bad type must error")
	}
	if _, err := p.RunGlob(context.Background(), map[string]any{"pattern": "*", "path": filepath.Join(root, "nope")}); err == nil {
		t.Error("missing root must error")
	}

	// Cap: seed more files than globMaxPaths under one dir.
	big := t.TempDir()
	for i := 0; i < globMaxPaths+5; i++ {
		if err := os.WriteFile(filepath.Join(big, itoa(i)+".txt"), []byte("x"), 0o644); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	got, err := p.RunGlob(context.Background(), map[string]any{"pattern": "*.txt", "path": big})
	if err != nil {
		t.Fatalf("RunGlob: %v", err)
	}
	rows := 0
	for _, line := range strings.Split(got, "\n") {
		if strings.HasSuffix(line, ".txt") {
			rows++
		}
	}
	if rows != globMaxPaths {
		t.Errorf("want exactly %d rows, got %d", globMaxPaths, rows)
	}
	if !strings.Contains(got, "result cap") {
		t.Errorf("cap note missing:\n%s", got)
	}
}

func TestGlobDefaultsToWorkspace(t *testing.T) {
	root := seedTree(t)
	ctx := core.WithRunWorkspace(context.Background(), root, "")
	got, err := provider().RunGlob(ctx, map[string]any{"pattern": "**/*.md"})
	if err != nil {
		t.Fatalf("RunGlob: %v", err)
	}
	if got != "notes.md" {
		t.Errorf("default root must be the run workspace: got %q", got)
	}
}

func TestNoMatchesSentinel(t *testing.T) {
	root := seedTree(t)
	if got, err := provider().RunGrep(context.Background(), map[string]any{"pattern": "zzzz", "path": root}); err != nil || got != "no matches" {
		t.Errorf("grep no-match: got %q, err %v", got, err)
	}
	if got, err := provider().RunGlob(context.Background(), map[string]any{"pattern": "*.zzz", "path": root}); err != nil || got != "no matches" {
		t.Errorf("glob no-match: got %q, err %v", got, err)
	}
}

// Audit round (card t_db98ebfe): a single-file root with a glob
// filter must match the file by its base name, not silently miss it
// (rel is "." for the root file itself).
func TestGrepSingleFileRootWithGlobFilter(t *testing.T) {
	root := seedTree(t)
	p := filepath.Join(root, "notes.md")
	got, err := provider().RunGrep(context.Background(), map[string]any{
		"pattern": "search me",
		"path":    p,
		"glob":    "*.md",
	})
	if err != nil {
		t.Fatalf("RunGrep: %v", err)
	}
	if !strings.Contains(got, "notes.md") {
		t.Errorf("single-file root must match its own glob filter:\n%s", got)
	}
	// and a non-matching filter still excludes it
	got, err = provider().RunGrep(context.Background(), map[string]any{
		"pattern": "search me",
		"path":    p,
		"glob":    "*.go",
	})
	if err != nil {
		t.Fatalf("RunGrep: %v", err)
	}
	if got != "no matches" {
		t.Errorf("non-matching filter must exclude the root file: got %q", got)
	}
}

// Audit round (card t_db98ebfe): a scan error mid-file must not print
// partial matches as if the file was fully read - an overlong line
// (bufio.ErrTooLong) ends the scan and the output carries the read
// error note.
func TestGrepScanErrorNotesTheFile(t *testing.T) {
	root := t.TempDir()
	long := strings.Repeat("x", 2*1024*1024) // 2 MB line: over the 1 MB scanner cap
	err := os.WriteFile(filepath.Join(root, "long.txt"),
		[]byte("search me before\n"+long+"\n"), 0o644)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	got, err := provider().RunGrep(context.Background(), map[string]any{
		"pattern":     "search me",
		"path":        root,
		"output_mode": "content",
	})
	if err != nil {
		t.Fatalf("RunGrep: %v", err)
	}
	if !strings.Contains(got, "long.txt:1: search me before") {
		t.Errorf("the match row must still print:\n%s", got)
	}
	if !strings.Contains(got, "read error") {
		t.Errorf("the partial read must carry an honest note:\n%s", got)
	}
}

func itoa(n int) string { return fmt.Sprintf("%d", n) }

// resultRows strips the note lines ("[...]") off a search result, so a
// test can assert on the served rows without the honest notes matching
// the same words.
func resultRows(out string) []string {
	var rows []string
	for _, line := range strings.Split(out, "\n") {
		if line == "" || strings.HasPrefix(line, "[") {
			continue
		}
		rows = append(rows, line)
	}
	return rows
}

// seedNoisyTree writes the shape the prod defect came from (card
// t_d5d092be): a walk-lexically-early pile of ignored trees that all
// contain the needle, plus the file the caller actually wanted.
// gitObjects fills .git/objects with matching entries (more than the
// grep file cap for the cap-spending proof; 0 for the plain fixtures).
func seedNoisyTree(t *testing.T, gitObjects int) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("seed dirs: %v", err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("seed %s: %v", rel, err)
		}
	}
	write(".cache/typescript/accelerometer.js", "needle\n")
	write(".git/config", "needle\n")
	write(".git/objects/pack/pack-abc.pack", "needle\x00pack bytes")
	write(".nuxt/build.js", "needle\n")
	write("dist/bundle.js", "needle\n")
	write("node_modules/x/y.js", "needle\n")
	write("node_modules/x/blob.node", "needle\x00native bytes")
	write("notes/real.md", "needle\n")
	write("app/main.js", "needle\n")
	for i := 0; i < gitObjects; i++ {
		write(fmt.Sprintf(".git/objects/pack/obj%d", i), "needle\n")
	}
	return root
}

// TestGrepPrunesIgnoredDirectories: the default skip set keeps the
// walk out of the noise trees entirely - the real hit arrives well
// inside the file cap, and the note names what was not searched.
func TestGrepPrunesIgnoredDirectories(t *testing.T) {
	root := seedNoisyTree(t, grepMaxFiles+5)
	got, err := provider().RunGrep(context.Background(), map[string]any{
		"pattern": "needle",
		"path":    root,
	})
	if err != nil {
		t.Fatalf("RunGrep: %v", err)
	}
	rows := resultRows(got)
	if len(rows) != 2 {
		t.Fatalf("rows = %v, want the two real files only:\n%s", rows, got)
	}
	for _, want := range []string{"notes/real.md", "app/main.js"} {
		if !strings.Contains(got, want) {
			t.Errorf("the real hit %q was lost:\n%s", want, got)
		}
	}
	for _, noise := range []string{".git/", "node_modules/", "dist/", ".cache/", ".nuxt/"} {
		for _, row := range rows {
			if strings.Contains(row, noise) {
				t.Errorf("ignored tree leaked into the rows (%s):\n%s", noise, got)
			}
		}
	}
	if !strings.Contains(got, "[skipped 5 ignored directories") {
		t.Errorf("the prune note must count the ignored trees:\n%s", got)
	}
	for _, name := range []string{".git", ".cache", "node_modules", "dist", ".nuxt"} {
		if !strings.Contains(got, name) {
			t.Errorf("the prune note must name %s:\n%s", name, got)
		}
	}
	// Pruned at the directory entry: nothing under the noise trees was
	// opened, so no per-file skip was counted (the 276398-binary-skips
	// cost in the prod incident).
	if strings.Contains(got, "binary") || strings.Contains(got, "size cap") {
		t.Errorf("ignored trees were walked file-by-file, not pruned:\n%s", got)
	}
	if strings.Contains(got, "result cap") {
		t.Errorf("the file cap was spent before the real hits:\n%s", got)
	}
}

// TestGrepIncludeIgnoredSearchesTheTrees: the skip set is a default,
// and include_ignored is the promised way back in.
func TestGrepIncludeIgnoredSearchesTheTrees(t *testing.T) {
	root := seedNoisyTree(t, 0)
	got, err := provider().RunGrep(context.Background(), map[string]any{
		"pattern":         "needle",
		"path":            root,
		"include_ignored": true,
	})
	if err != nil {
		t.Fatalf("RunGrep: %v", err)
	}
	for _, want := range []string{"node_modules/x/y.js", "dist/bundle.js", ".cache/typescript/accelerometer.js", ".nuxt/build.js", ".git/config", "notes/real.md"} {
		if !strings.Contains(got, want) {
			t.Errorf("include_ignored lost %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "ignored directories") {
		t.Errorf("nothing was pruned, so no prune note belongs here:\n%s", got)
	}
	// The other half of the prune test's claim: unpruned, the binary
	// noise trees ARE read and counted. This is the 276398-skip cost.
	if !strings.Contains(got, "[skipped 2 binary]") {
		t.Errorf("the unpruned walk must read the ignored binaries:\n%s", got)
	}
}

// TestGlobPrunesIgnoredDirectories: the same skip set on the name
// search, same note.
func TestGlobPrunesIgnoredDirectories(t *testing.T) {
	root := seedNoisyTree(t, 0)
	got, err := provider().RunGlob(context.Background(), map[string]any{
		"pattern": "**/*.js",
		"path":    root,
	})
	if err != nil {
		t.Fatalf("RunGlob: %v", err)
	}
	rows := resultRows(got)
	if len(rows) != 1 || rows[0] != "app/main.js" {
		t.Fatalf("rows = %v, want [app/main.js]:\n%s", rows, got)
	}
	if !strings.Contains(got, "[skipped 5 ignored directories") || !strings.Contains(got, "node_modules") {
		t.Errorf("glob prune note wrong:\n%s", got)
	}

	got, err = provider().RunGlob(context.Background(), map[string]any{
		"pattern":         "**/*.js",
		"path":            root,
		"include_ignored": true,
	})
	if err != nil {
		t.Fatalf("RunGlob: %v", err)
	}
	for _, want := range []string{"node_modules/x/y.js", "dist/bundle.js", ".cache/typescript/accelerometer.js", ".nuxt/build.js"} {
		if !strings.Contains(got, want) {
			t.Errorf("include_ignored glob lost %q:\n%s", want, got)
		}
	}
}

// TestIgnoreNeverPrunesAnExplicitRoot: naming an ignored tree as the
// root is a request to search it - the skip set applies below the
// root only.
func TestIgnoreNeverPrunesAnExplicitRoot(t *testing.T) {
	root := seedNoisyTree(t, 0)
	got, err := provider().RunGrep(context.Background(), map[string]any{
		"pattern":     "needle",
		"path":        filepath.Join(root, ".git"),
		"output_mode": "content",
	})
	if err != nil {
		t.Fatalf("RunGrep: %v", err)
	}
	if !strings.Contains(got, "config:1: needle") {
		t.Errorf("an explicit .git root must be searched:\n%s", got)
	}
	if got, err = provider().RunGlob(context.Background(), map[string]any{
		"pattern": "**/*.js",
		"path":    filepath.Join(root, "node_modules"),
	}); err != nil {
		t.Fatalf("RunGlob: %v", err)
	} else if !strings.Contains(got, "x/y.js") {
		t.Errorf("an explicit node_modules root must be listed:\n%s", got)
	}
}

// TestPrunedDirNoteCap: the note names at most ignoredDirNoteCap
// directories, with an honest tail - a home root has many
// node_modules trees and the note must not become the result.
func TestPrunedDirNoteCap(t *testing.T) {
	root := t.TempDir()
	want := ignoredDirNoteCap + 3
	for i := 0; i < want; i++ {
		dir := filepath.Join(root, "p"+itoa(i), "node_modules")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	got, err := provider().RunGlob(context.Background(), map[string]any{"pattern": "*.zzz", "path": root})
	if err != nil {
		t.Fatalf("RunGlob: %v", err)
	}
	if !strings.Contains(got, fmt.Sprintf("[skipped %d ignored directories", want)) {
		t.Errorf("the note must count every pruned dir:\n%s", got)
	}
	if !strings.Contains(got, "...and 3 more") {
		t.Errorf("the note must cap the names with an honest tail:\n%s", got)
	}
}

// TestGrepBadIncludeIgnored: a non-boolean include_ignored is a loud
// argument error, never a silent default.
func TestGrepBadIncludeIgnored(t *testing.T) {
	root := seedTree(t)
	if _, err := provider().RunGrep(context.Background(), map[string]any{
		"pattern": "x", "path": root, "include_ignored": "yes",
	}); err == nil {
		t.Error("grep: non-boolean include_ignored must error")
	}
	if _, err := provider().RunGlob(context.Background(), map[string]any{
		"pattern": "*", "path": root, "include_ignored": "yes",
	}); err == nil {
		t.Error("glob: non-boolean include_ignored must error")
	}
}
