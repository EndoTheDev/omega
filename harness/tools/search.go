package tools

// The search domain: content search (grep) and name match (glob) as
// one provider on the tools-seam rail. The registry capsule ships
// them (card t_db98ebfe, the roadmap's search stubs): grep and glob
// are two single-purpose tools (the one-domain law, t_73994e55 - two
// domains, one verb each, no sub-action split needed).
//
// Everything is stdlib: regexp + filepath.WalkDir. Output is bounded
// (file/line caps with an honest truncation note), binary files are
// refused, and every path passes the instance-database deny guard the
// file tools use (ENDO'S LAW 2026-10-18, harness/datadir).

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/datadir"
)

// Bounded-output caps. The note lines carry the honest totals so the
// model knows what it did not see, exactly like shellOutput's
// truncation note.
const (
	grepMaxFiles  = 200 // files_with_matches rows
	grepMaxLines  = 400 // content-mode output lines
	globMaxPaths  = 1000
	maxSearchFile = 4 << 20 // 4 MB: larger files are skipped
	headProbeSize = 8192    // NUL detector window
)

// errSearchCapReached aborts a walk when a cap hits; the callers turn
// it into a truncation note, never a tool error.
var errSearchCapReached = errors.New("search result cap reached")

// SearchProvider serves the grep and glob tools. The guard refuses
// paths that resolve into the instance database; nil is pass-through.
type SearchProvider struct {
	guard *datadir.Guard
}

// Tools returns the two search tools keyed by name.
func (p *SearchProvider) Tools() map[string]core.Tool {
	return map[string]core.Tool{
		"grep": {
			Description: "Search file contents with a regular expression across a directory tree. Returns matching files, lines, or counts. Binary files are skipped and output is capped.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"pattern":     map[string]any{"type": "string", "description": "Regular expression to search for (Go regexp syntax)."},
					"path":        map[string]any{"type": "string", "description": "Root directory (or single file) to search. Relative paths root at the session workspace; default is the workspace."},
					"glob":        map[string]any{"type": "string", "description": "Optional file-name filter, e.g. \"*.go\" or \"**/*.md\" (a * never crosses a directory boundary; ** does)."},
					"output_mode": map[string]any{"type": "string", "enum": []string{"files_with_matches", "content", "count"}, "description": "files_with_matches (default), content (lines with path:line: text), or count (per-file match counts)."},
					"context":     map[string]any{"type": "integer", "description": "Lines of context around each match (content mode only)."},
				},
				"required": []string{"pattern"},
			},
			Run: p.RunGrep,
		},
		"glob": {
			Description: "List paths matching a name pattern under a directory. Supports * (never crosses a directory boundary), ** (crosses), and ? . Returns matching paths, newest-tree order.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"pattern": map[string]any{"type": "string", "description": "Name pattern relative to the root, e.g. \"*.go\", \"sub/*.md\", \"**/*.yaml\"."},
					"path":    map[string]any{"type": "string", "description": "Root directory. Relative paths root at the session workspace; default is the workspace."},
					"type":    map[string]any{"type": "string", "enum": []string{"files", "dirs"}, "description": "Restrict to files or directories; empty means both."},
				},
				"required": []string{"pattern"},
			},
			Run: p.RunGlob,
		},
	}
}

// argString extracts a required string argument.
func argString(args map[string]any, key string) (string, error) {
	v, ok := args[key]
	if !ok {
		return "", fmt.Errorf("missing required argument %q", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("argument %q must be a string, got %T", key, v)
	}
	return s, nil
}

// argInt extracts an optional integer argument (JSON numbers are
// float64 on the tool-args wire).
func argInt(args map[string]any, key string) (int, bool, error) {
	v, ok := args[key]
	if !ok || v == nil {
		return 0, false, nil
	}
	f, ok := v.(float64)
	if !ok {
		return 0, false, fmt.Errorf("argument %q must be a number", key)
	}
	if f < 0 || f != float64(int(f)) {
		return 0, false, fmt.Errorf("argument %q must be a non-negative integer", key)
	}
	return int(f), true, nil
}

// searchRoot resolves the root-path argument: empty means the run's
// session workspace (the process working directory when the run is
// not rooted); relative paths root there exactly like the file tools'
// resolveToolPath; absolute paths pass through.
func searchRoot(ctx context.Context, raw string) string {
	if strings.TrimSpace(raw) == "" {
		if dir := core.RunWorkspace(ctx); dir != "" {
			return dir
		}
		return "."
	}
	if filepath.IsAbs(raw) {
		return filepath.Clean(raw)
	}
	if dir := core.RunWorkspace(ctx); dir != "" {
		return filepath.Join(dir, raw)
	}
	return raw
}

// globToRegexp translates a glob pattern into a regexp over
// slash-separated relative paths: * stays within one directory
// level, ** crosses them (with the bash-globstar spelling "**/"
// allowing zero directories), all else is literal. This is the one
// matcher grep's glob filter and the glob tool share.
func globToRegexp(pattern string) (*regexp.Regexp, error) {
	var sb strings.Builder
	sb.WriteString("(?s)^")
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		if c == '*' && i+1 < len(pattern) && pattern[i+1] == '*' {
			// "**/" may expand to zero directories; a bare "**"
			// crosses levels inside a segment.
			if i+2 < len(pattern) && pattern[i+2] == '/' {
				sb.WriteString("(?:.*/)?")
				i += 2
			} else {
				sb.WriteString(".*")
				i++
			}
			continue
		}
		switch c {
		case '*':
			sb.WriteString("[^/]*")
		case '?':
			sb.WriteString("[^/]")
		case '\\':
			if i+1 < len(pattern) {
				i++
				sb.WriteString(regexp.QuoteMeta(string(pattern[i])))
			}
		default:
			sb.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	sb.WriteString("$")
	re, err := regexp.Compile(sb.String())
	if err != nil {
		return nil, fmt.Errorf("bad glob pattern %q: %v", pattern, err)
	}
	return re, nil
}

// relMatch matches an entry's path relative to the search root
// (slash-separated) against the filter pattern.
func relMatch(re *regexp.Regexp, root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return re.MatchString(filepath.ToSlash(rel))
}

// skipReason reports why a file is out of the search's reach: it
// resolves into the instance database, looks binary, or exceeds the
// size cap. "" means searchable.
func (p *SearchProvider) skipReason(path string) string {
	if p.guard.DenyFile(path) {
		return "the instance database's door"
	}
	fi, err := os.Stat(path)
	if err != nil {
		return "unreadable"
	}
	if !fi.Mode().IsRegular() {
		return "not a regular file"
	}
	if fi.Size() > maxSearchFile {
		return "size cap"
	}
	return ""
}

// looksBinary probes the file head for a NUL byte.
func looksBinary(f *os.File) (bool, error) {
	head := make([]byte, headProbeSize)
	n, err := f.Read(head)
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	return bytes.IndexByte(head[:n], 0) >= 0, nil
}

// RunGrep searches file contents under a root for a regexp.
func (p *SearchProvider) RunGrep(ctx context.Context, args map[string]any) (string, error) {
	pattern, err := argString(args, "pattern")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(pattern) == "" {
		return "", fmt.Errorf("pattern must not be empty")
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("bad regexp: %v", err)
	}

	root := searchRoot(ctx, stringArg(args, "path"))
	if fi, statErr := os.Stat(root); statErr != nil {
		return "", fmt.Errorf("root %s: %v", root, statErr)
	} else if !fi.IsDir() && !fi.Mode().IsRegular() {
		return "", fmt.Errorf("root %s: neither a directory nor a file", root)
	}
	if p.guard.DenyFile(root) {
		return "", datadir.DBDenied(root)
	}

	var filter *regexp.Regexp
	if v, ok := args["glob"].(string); ok && strings.TrimSpace(v) != "" {
		if filter, err = globToRegexp(v); err != nil {
			return "", err
		}
	}
	mode := "files_with_matches"
	if v, ok := args["output_mode"].(string); ok && v != "" {
		switch v {
		case "files_with_matches", "content", "count":
			mode = v
		default:
			return "", fmt.Errorf("output_mode must be files_with_matches, content, or count, got %q", v)
		}
	}
	context, hasContext, err := argInt(args, "context")
	if err != nil {
		return "", err
	}

	var out strings.Builder
	matchFiles := 0 // files_with_matches rows (and per-file blocks below)
	outLines := 0   // content-mode rows
	truncated := false
	skipped := map[string]int{} // reason -> count

	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if path == root {
				return walkErr
			}
			skipped["walk errors"]++
			return nil
		}
		if d.IsDir() {
			return nil // symlinked dirs are never followed by WalkDir
		}
		if fi, statErr := d.Info(); statErr != nil || !fi.Mode().IsRegular() {
			skipped["not regular files"]++
			return nil
		}
		if filter != nil && !relMatch(filter, root, path) {
			return nil
		}
		return p.grepFile(&out, root, path, re, mode, context, hasContext,
			&matchFiles, &outLines, &truncated, skipped)
	})
	if err != nil && !errors.Is(err, errSearchCapReached) {
		return "", fmt.Errorf("grep walk: %v", err)
	}
	if errors.Is(err, errSearchCapReached) {
		truncated = true
	}

	if matchFiles == 0 && outLines == 0 {
		return "no matches", nil
	}
	if mode == "content" {
		if truncated {
			out.WriteString("[output truncated at the line cap]\n")
		}
	} else if truncated {
		fmt.Fprintf(&out, "[result cap (%d files) reached]\n", grepMaxFiles)
	}
	for reason, n := range skipped {
		fmt.Fprintf(&out, "[skipped %d %s]\n", n, reason)
	}
	return strings.TrimSuffix(out.String(), "\n"), nil
}

// grepFile scans one file and appends its contribution to out. It
// returns errSearchCapReached when the caller's caps are spent; any
// other error comes from the walk (fs.SkipAll semantics).
func (p *SearchProvider) grepFile(out *strings.Builder, root, path string, re *regexp.Regexp,
	mode string, context int, hasContext bool,
	matchFiles, outLines *int, truncated *bool, skipped map[string]int) error {

	if reason := p.skipReason(path); reason != "" {
		skipped[reason]++
		return nil
	}

	f, err := os.Open(path)
	if err != nil {
		skipped["open errors"]++
		return nil
	}
	defer f.Close()
	if binary, err := looksBinary(f); err != nil {
		skipped["read errors"]++
		return nil
	} else if binary {
		skipped["binary"]++
		return nil
	}
	if _, err := f.Seek(0, 0); err != nil {
		skipped["seek errors"]++
		return nil
	}

	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = path
	}
	rel = filepath.ToSlash(rel)

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	if mode != "content" {
		count := 0
		line := 0
		for scanner.Scan() {
			line++
			if re.Match(scanner.Bytes()) {
				count++
				if mode == "files_with_matches" {
					break // one row per file is all it needs
				}
			}
		}
		if count == 0 {
			return nil
		}
		if mode == "count" {
			fmt.Fprintf(out, "%s: %d\n", rel, count)
		} else {
			fmt.Fprintf(out, "%s\n", rel)
		}
		*matchFiles++
		if *matchFiles >= grepMaxFiles {
			return errSearchCapReached
		}
		return nil
	}

	// content mode: collect the matched line numbers, then render the
	// context blocks (the context lines must come from memory of the
	// file's text, which is capped by maxSearchFile).
	lineNo := 0
	var text strings.Builder
	var matches []int
	for scanner.Scan() {
		lineNo++
		line := scanner.Bytes()
		if re.Match(line) {
			matches = append(matches, lineNo)
		}
		text.Write(scopy(line))
		text.WriteByte('\n')
	}
	if len(matches) == 0 {
		return nil
	}
	*matchFiles++
	if !emitContextBlocks(out, matches, context, hasContext, rel,
		strings.SplitAfter(text.String(), "\n"), lineNo, outLines) {
		return errSearchCapReached
	}
	if *outLines >= grepMaxLines {
		return errSearchCapReached
	}
	return nil
}

// emitContextBlocks renders path:line: text rows for the merged
// match+context windows of one file, stopping at the global line
// cap. Returns false when the cap hit mid-file (the caller aborts
// the walk). lines is the SplitAfter slice of the file text (one
// entry per newline); lineCount is the real line number of the last
// line. Block high edges are clamped so no phantom trailing row can
// print.
func emitContextBlocks(out *strings.Builder, matches []int, context int, hasContext bool,
	rel string, lines []string, lineCount int, outLines *int) bool {
	if !hasContext || context == 0 {
		for _, m := range matches {
			printLine(out, rel, m, lines, outLines)
			if *outLines >= grepMaxLines {
				return false
			}
		}
		return true
	}
	// Merge overlapping [m-context, m+context] windows into blocks.
	type span struct{ lo, hi int }
	var blocks []span
	for _, m := range matches {
		lo, hi := m-context, m+context
		if lo < 1 {
			lo = 1
		}
		if hi > lineCount {
			hi = lineCount
		}
		if n := len(blocks); n > 0 && blocks[n-1].hi >= lo-1 {
			if hi > blocks[n-1].hi {
				blocks[n-1].hi = hi
			}
			continue
		}
		blocks = append(blocks, span{lo, hi})
	}
	for bi, b := range blocks {
		if bi > 0 {
			out.WriteString("--\n")
		}
		for ln := b.lo; ln <= b.hi; ln++ {
			printLine(out, rel, ln, lines, outLines)
			if *outLines >= grepMaxLines {
				return false
			}
		}
	}
	return true
}

// printLine emits one content row and bumps the global line counter.
func printLine(out *strings.Builder, rel string, ln int, lines []string, outLines *int) {
	body := ""
	if ln-1 < len(lines) {
		body = strings.TrimRight(lines[ln-1], "\n")
	}
	fmt.Fprintf(out, "%s:%d: %s\n", rel, ln, body)
	*outLines++
}

func scopy(b []byte) []byte { return append(make([]byte, 0, len(b)), b...) }

// stringArg reads an optional string argument's value (absent = "").
func stringArg(args map[string]any, key string) string {
	v, ok := args[key].(string)
	if !ok {
		return ""
	}
	return v
}

// RunGlob lists paths under a root whose slash-separated relative
// path matches a glob pattern.
func (p *SearchProvider) RunGlob(ctx context.Context, args map[string]any) (string, error) {
	pattern, err := argString(args, "pattern")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(pattern) == "" {
		return "", fmt.Errorf("pattern must not be empty")
	}
	re, err := globToRegexp(filepath.ToSlash(filepath.Clean(pattern)))
	if err != nil {
		return "", err
	}
	root := searchRoot(ctx, stringArg(args, "path"))
	if _, statErr := os.Stat(root); statErr != nil {
		return "", fmt.Errorf("root %s: %v", root, statErr)
	}
	if p.guard.DenyFile(root) {
		return "", datadir.DBDenied(root)
	}
	kind := stringArg(args, "type")
	switch kind {
	case "", "files", "dirs":
	default:
		return "", fmt.Errorf("type must be files or dirs, got %q", kind)
	}

	var out strings.Builder
	n := 0
	truncated := false
	skipped := map[string]int{}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if path == root {
				return walkErr
			}
			skipped["walk errors"]++
			return nil
		}
		if path == root {
			return nil
		}
		isDir := d.IsDir()
		if kind == "files" && isDir {
			return nil
		}
		if kind == "dirs" && !isDir {
			return nil
		}
		if !relMatch(re, root, path) {
			return nil
		}
		// The db family stays invisible to the name match too: the
		// ENDO'S LAW deny set is a filesystem door, not a read door.
		if p.guard.DenyFile(path) {
			skipped["the instance database's door"]++
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		out.WriteString(filepath.ToSlash(rel))
		out.WriteByte('\n')
		n++
		if n >= globMaxPaths {
			truncated = true
			return errSearchCapReached
		}
		return nil
	})
	if err != nil && !errors.Is(err, errSearchCapReached) {
		return "", fmt.Errorf("glob walk: %v", err)
	}
	if n == 0 {
		return "no matches", nil
	}
	if truncated {
		out.WriteString("[...result cap reached]\n")
	}
	for reason, cnt := range skipped {
		fmt.Fprintf(&out, "[skipped %d %s]\n", cnt, reason)
	}
	return strings.TrimSuffix(out.String(), "\n"), nil
}

// NewSearchProvider builds the search tool provider.
func NewSearchProvider(guard *datadir.Guard) *SearchProvider { return &SearchProvider{guard: guard} }
