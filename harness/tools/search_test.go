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

func itoa(n int) string { return fmt.Sprintf("%d", n) }
