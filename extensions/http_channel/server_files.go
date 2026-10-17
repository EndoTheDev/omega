package http_channel

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// Read-only file surface for the webui inspector: browse and preview the
// process working directory. Strictly GET - no write, delete, or exec
// path exists on this surface. Path safety has three layers enforced on
// every request: absolute paths and empty segments are rejected, ANY
// segment starting with "." is rejected (hidden files and .. traversal
// die under one rule), and the cleaned path is symlink-resolved and
// verified to stay under the root before the disk is touched.

// fileReadCap is the preview size cap: contents are cut at 64 KiB and
// the truncated flag tells the UI the cut happened.
const fileReadCap = 64 * 1024

// fileEntry is one /files/browse row.
type fileEntry struct {
	Name      string `json:"name"`
	SizeBytes int64  `json:"size_bytes"`
	IsDir     bool   `json:"is_dir"`
	ModTime   string `json:"mod_time"`
}

// validateRelPath checks a user-supplied relative path against the
// segment rules and returns the cleaned relative path ("" = the root).
// Absolute paths, empty segments, and dot-prefixed segments are rejected.
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
// is checked against the returned realRoot, never the raw root: the
// working directory itself may sit behind a symlink (/tmp on macOS,
// /var/run/...), and comparing a realRoot-resolved path against the
// raw root would fail every request in such environments. A missing
// leaf is not an error here - the resolved parent is returned and the
// caller's disk operation reports not-found. Symlink escapes are
// caught by ensureUnderRoot.
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

// fileError writes the structured JSON error envelope for the file
// surface - failures on this endpoint are data, not panic material.
func fileError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// handleFilesBrowse serves GET /files/browse?path=<rel>, listing one
// directory of the process working directory. Empty or missing path
// lists the root.
func (s *Server) handleFilesBrowse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rel, err := validateRelPath(r.URL.Query().Get("path"))
	if err != nil {
		fileError(w, http.StatusBadRequest, err.Error())
		return
	}
	root, err := os.Getwd()
	if err != nil {
		fileError(w, http.StatusInternalServerError, "resolve working directory: "+err.Error())
		return
	}
	realRoot, resolved, rerr := resolveUnderRoot(root, rel)
	if rerr != nil {
		// fail closed: no containment verdict, no disk access
		fileError(w, http.StatusInternalServerError, "resolve path: "+rerr.Error())
		return
	}
	if err := ensureUnderRoot(realRoot, resolved); err != nil {
		fileError(w, http.StatusBadRequest, err.Error())
		return
	}
	if info, serr := os.Stat(resolved); serr != nil {
		if os.IsNotExist(serr) {
			fileError(w, http.StatusNotFound, "not found")
			return
		}
		fileError(w, http.StatusInternalServerError, "stat path: "+serr.Error())
		return
	} else if !info.IsDir() {
		fileError(w, http.StatusBadRequest, "not a directory")
		return
	}
	dirEntries, err := os.ReadDir(resolved)
	if err != nil {
		fileError(w, http.StatusInternalServerError, "read directory: "+err.Error())
		return
	}
	entries := make([]fileEntry, 0, len(dirEntries))
	for _, de := range dirEntries {
		entry := fileEntry{
			Name:  de.Name(),
			IsDir: de.IsDir(),
		}
		info, ierr := de.Info()
		if ierr == nil {
			entry.SizeBytes = info.Size()
			entry.ModTime = info.ModTime().UTC().Format(time.RFC3339)
		}
		entries = append(entries, entry)
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": sortFileEntries(entries)})
}

// handleFilesRead serves GET /files/read?path=<rel>, returning up to 64
// KiB of a file's content. Non-UTF-8 bytes come back as binary=true with
// empty content - no binary previews on this surface.
func (s *Server) handleFilesRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rel, err := validateRelPath(r.URL.Query().Get("path"))
	if err != nil {
		fileError(w, http.StatusBadRequest, err.Error())
		return
	}
	root, err := os.Getwd()
	if err != nil {
		fileError(w, http.StatusInternalServerError, "resolve working directory: "+err.Error())
		return
	}
	realRoot, resolved, rerr := resolveUnderRoot(root, rel)
	if rerr != nil {
		// fail closed: no containment verdict, no disk access
		fileError(w, http.StatusInternalServerError, "resolve path: "+rerr.Error())
		return
	}
	if err := ensureUnderRoot(realRoot, resolved); err != nil {
		fileError(w, http.StatusBadRequest, err.Error())
		return
	}
	info, err := os.Stat(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			fileError(w, http.StatusNotFound, "not found")
			return
		}
		fileError(w, http.StatusInternalServerError, "stat file: "+err.Error())
		return
	}
	if info.IsDir() {
		fileError(w, http.StatusBadRequest, "not a file")
		return
	}
	f, err := os.Open(resolved)
	if err != nil {
		fileError(w, http.StatusInternalServerError, "open file: "+err.Error())
		return
	}
	defer f.Close()
	// one byte over the cap, so a cut is detectable without a second stat
	data := make([]byte, fileReadCap+1)
	n, err := f.Read(data)
	if err != nil && n == 0 && !errors.Is(err, io.EOF) {
		fileError(w, http.StatusInternalServerError, "read file: "+err.Error())
		return
	}
	data = data[:n]
	truncated := false
	if len(data) > fileReadCap {
		data = data[:fileReadCap]
		truncated = true
	}
	binary := !utf8.Valid(data)
	content := ""
	if !binary {
		content = string(data)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"content":   content,
		"truncated": truncated,
		"binary":    binary,
	})
}
