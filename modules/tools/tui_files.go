package tools

// Module-owned TUI surface (card t_88bb7dcf): /files belongs to the
// tools module. The walk/validation rules below mirror
// modules/http_channel server_files.go one to one (validateRelPath,
// resolveUnderRoot, ensureUnderRoot, sortFileEntries, the 64 KiB cap)
// - both frontends answer the same request the same way. The http
// surface is http-only and the TUI does not import it, so the mirror
// lives here, next to the files domain tool. Read-only: nothing
// writes.

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// filesPreviewCap is the preview size cap, matching the webui's:
// contents are cut at 64 KiB and the truncated flag says so.
const filesPreviewCap = 64 * 1024

// validateRelPath checks a user-supplied relative path against the
// segment rules (mirroring the webui) and returns the cleaned relative
// path ("" = the root). Absolute paths, empty segments, and
// dot-prefixed segments are rejected - hidden files and .. traversal
// die under one rule.
func validateRelPath(req string) (string, error) {
	if req == "" {
		return "", nil
	}
	if filepath.IsAbs(req) || strings.HasPrefix(req, "/") {
		return "", errors.New("absolute paths are not allowed")
	}
	parts := strings.Split(req, "/")
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			return "", errors.New("empty path segment")
		}
		if strings.HasPrefix(part, ".") {
			return "", errors.New("dot-prefixed path segment")
		}
		cleaned = append(cleaned, part)
	}
	return strings.Join(cleaned, "/"), nil
}

// resolveUnderRoot joins a validated relative path against root and
// resolves it through symlink evaluation, returning the real root
// (EvalSymlinks-resolved) alongside the resolved target. Containment
// is checked against realRoot, never the raw root: the working
// directory itself may sit behind a symlink. A missing leaf is not an
// error here - the resolved parent is returned and the caller's disk
// operation reports not-found.
func resolveUnderRoot(root, rel string) (realRoot, resolved string, err error) {
	realRoot, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", "", err
	}
	target := filepath.Join(realRoot, rel)
	resolved, err = filepath.EvalSymlinks(target)
	if err != nil {
		// EvalSymlinks fails for a nonexistent leaf; resolve the parent
		// so a missing file still gets the containment check
		realParent, perr := filepath.EvalSymlinks(filepath.Dir(target))
		if perr != nil {
			return "", "", perr
		}
		return realRoot, filepath.Join(realParent, filepath.Base(target)), nil
	}
	return realRoot, resolved, nil
}

// ensureUnderRoot verifies a resolved absolute path stays inside realRoot.
func ensureUnderRoot(realRoot, resolved string) error {
	if resolved == realRoot {
		return nil
	}
	if !strings.HasPrefix(resolved, realRoot+string(filepath.Separator)) {
		return errors.New("path escapes the working directory")
	}
	return nil
}

// fileEntry is one /files row (mirrors the webui's fileEntry).
type fileEntry struct {
	Name      string
	SizeBytes int64
	IsDir     bool
	ModTime   time.Time
}

// sortFileEntries returns the entries ordered: directories first, then
// names, case-insensitive. Pure - sorts a copy, never the caller's slice.
func sortFileEntries(entries []fileEntry) []fileEntry {
	out := append([]fileEntry(nil), entries...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

// filesRoot returns the directory /files walks: the process working
// directory as passed from the host, falling back to os.Getwd (the
// same root the webui file surface walks).
func filesRoot(cwd string) (string, error) {
	if cwd != "" {
		return cwd, nil
	}
	return os.Getwd()
}

// listDirUnderRoot lists one directory of root, applying the same
// validation/containment as the webui. Returns the sorted row set.
func listDirUnderRoot(root, rel string) ([]fileEntry, error) {
	rel, err := validateRelPath(rel)
	if err != nil {
		return nil, err
	}
	realRoot, resolved, rerr := resolveUnderRoot(root, rel)
	if rerr != nil {
		return nil, rerr
	}
	if err := ensureUnderRoot(realRoot, resolved); err != nil {
		return nil, err
	}
	info, serr := os.Stat(resolved)
	if serr != nil {
		return nil, serr
	}
	if !info.IsDir() {
		return nil, errors.New("not a directory: " + rel)
	}
	dirEntries, err := os.ReadDir(resolved)
	if err != nil {
		return nil, err
	}
	entries := make([]fileEntry, 0, len(dirEntries))
	for _, de := range dirEntries {
		entry := fileEntry{Name: de.Name(), IsDir: de.IsDir()}
		if info, ierr := de.Info(); ierr == nil {
			entry.SizeBytes = info.Size()
			entry.ModTime = info.ModTime()
		}
		entries = append(entries, entry)
	}
	return sortFileEntries(entries), nil
}

// readPreviewAt reads up to 64 KiB of the file at rel under root,
// mirroring the webui's /files/read: non-UTF-8 content comes back
// binary=true with empty content, and the truncated flag says when
// the 64 KiB cap cut the read.
func readPreviewAt(root, rel string) (content, realRel string, truncated, binary bool, err error) {
	realRel, err = validateRelPath(rel)
	if err != nil {
		return "", "", false, false, err
	}
	realRoot, resolved, rerr := resolveUnderRoot(root, realRel)
	if rerr != nil {
		return "", "", false, false, rerr
	}
	if err := ensureUnderRoot(realRoot, resolved); err != nil {
		return "", "", false, false, err
	}
	info, serr := os.Stat(resolved)
	if serr != nil {
		return "", "", false, false, serr
	}
	if info.IsDir() {
		return "", "", false, false, errors.New("not a file: " + realRel)
	}
	f, err := os.Open(resolved)
	if err != nil {
		return "", "", false, false, err
	}
	defer f.Close()
	// one byte over the cap, so a cut is detectable without a second stat
	data := make([]byte, filesPreviewCap+1)
	n, rerr := f.Read(data)
	if rerr != nil && n == 0 && !errors.Is(rerr, io.EOF) {
		return "", "", false, false, rerr
	}
	data = data[:n]
	if len(data) > filesPreviewCap {
		data = data[:filesPreviewCap]
		truncated = true
	}
	if !utf8.Valid(data) {
		return "", realRel, truncated, true, nil
	}
	return string(data), realRel, truncated, false, nil
}