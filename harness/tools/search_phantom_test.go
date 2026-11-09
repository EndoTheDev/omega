package tools

// Lane A of the phantom-results probe (card t_a54ef2ce): the prod
// agent's skill accused files.grep of serving STALE INDEX rows
// ("line numbers 0 or column -1", paths that files.read then reports
// as nonexistent). The probe disproved the index - grep walks the live
// tree on every call - so these tests pin the property that was
// accused, in the shape a future regression would break: rows served
// by grep must track the filesystem between calls, and every content
// row must carry a real 1-based line number.

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestGrepReadsLiveFilesystemNoIndex: nothing about a grep result may
// outlive the walk. Rewrite the file and the next call sees the new
// content at the new line; delete it and the path stops appearing.
// A cache or index layer introduced here would serve the old rows.
func TestGrepReadsLiveFilesystemNoIndex(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.md")
	grep := func() string {
		t.Helper()
		got, err := provider().RunGrep(context.Background(), map[string]any{
			"pattern":     "needle",
			"path":        root,
			"output_mode": "content",
		})
		if err != nil {
			t.Fatalf("grep: %v", err)
		}
		return got
	}

	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	write("pad\npad\nneedle here\n")
	if got := grep(); !strings.Contains(got, "note.md:3: needle here") {
		t.Fatalf("first grep = %q, want the line-3 row", got)
	}

	// Same text, moved: the served line number must follow the file.
	write("needle here\npad\npad\npad\n")
	if got := grep(); !strings.Contains(got, "note.md:1: needle here") {
		t.Fatalf("after rewrite = %q, want the line-1 row (a stale index would still say 3)", got)
	}

	// Removed: the path must leave the result entirely.
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if got := grep(); strings.Contains(got, "note.md") {
		t.Fatalf("after delete = %q, want no row for the deleted file", got)
	}
}

// TestGrepContentRowsAreOneBased: the row format is path:line: text and
// the line is a real 1-based file line - no zero rows, no phantom
// trailing row past the last newline. The probe's red flag was a
// fabricated "line 0 / column -1" table; this pins the real shape so a
// malformed row can never be served silently.
func TestGrepContentRowsAreOneBased(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantRow string
	}{
		{"no trailing newline", "needle", "a.txt:1: needle"},
		{"leading blank line", "\nneedle\n", "a.txt:2: needle"},
		{"crlf", "x\r\nneedle\r\n", "a.txt:2: needle"},
		{"trailing blank lines", "needle\n\n\n", "a.txt:1: needle"},
	}
	rowRe := regexp.MustCompile(`^([^:]+):(\d+):`)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte(tc.body), 0o644); err != nil {
				t.Fatalf("seed: %v", err)
			}
			got, err := provider().RunGrep(context.Background(), map[string]any{
				"pattern":     "needle",
				"path":        root,
				"output_mode": "content",
			})
			if err != nil {
				t.Fatalf("grep: %v", err)
			}
			if !strings.Contains(got, tc.wantRow) {
				t.Fatalf("grep = %q, want row %q", got, tc.wantRow)
			}
			for _, line := range strings.Split(strings.TrimSuffix(got, "\n"), "\n") {
				m := rowRe.FindStringSubmatch(line)
				if m == nil {
					t.Fatalf("row %q does not match path:line: text", line)
				}
				n, convErr := strconv.Atoi(m[2])
				if convErr != nil || n < 1 {
					t.Fatalf("row %q carries line %q - content rows are 1-based", line, m[2])
				}
			}
		})
	}
}
