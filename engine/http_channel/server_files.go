package http_channel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
// process working directory. GET routes only - then the write half (card
// t_a4504a25, the Files tab toolbar's add-file/add-folder): POST
// /files/write and POST /files/mkdir create nodes INSIDE the same root,
// guarded by the SAME three layers the read half enforces: relative
// paths and empty segments are rejected, ANY segment starting with "."
// is rejected (hidden files and .. traversal die under one rule), and
// the cleaned path is symlink-resolved and verified to stay under the
// root before the disk is touched.

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

// files root sources, reported on every /files/browse response so the
// UI knows whether the tree sits on a real workspace (header shows) or
// the bare cwd fallback (header hides entirely - the Endo ruling: no
// dead header row over a rooted-nowhere tree).
const (
	filesSourceSession = "session"
	filesSourceDefault = "default"
	filesSourceCwd     = "cwd"
)

// setDefaultWorkspace stores the `default_workspace` knob. The hot
// setter: wire time carries the boot config, the settings seam's
// hot-apply pushes later saves. atomic.Value because file-surface
// requests read it concurrently with the seam's writes.
func (s *Server) SetDefaultWorkspace(ws string) {
	s.defaultWorkspace.Store(ws)
}

// resolveFilesRoot is the Files tab root resolution law, server-side
// end to end (the client never sends an absolute root; it sends, at
// most, which session it is focused on):
//  1. the focused session's pinned workspace (the store row), live:
//     every request re-reads it, so a re-pin or a session switch
//     re-roots the tab with no cache;
//  2. else the configured default workspace;
//  3. else the process cwd - the fallback the pre-law handlers used,
//     so the tab still shows something on a workspace-less box.
//
// A session id the store does not know (a draft, an unknown id, a
// no-store lane) simply falls through the chain - resolution never
// fails on it.
func (s *Server) resolveFilesRoot(ctx context.Context, sessionID string) (root, source string, err error) {
	if sessionID != "" && s.store != nil {
		if sess, ferr := s.store.GetSession(ctx, sessionID); ferr == nil &&
			strings.TrimSpace(sess.Workspace) != "" {
			return sess.Workspace, filesSourceSession, nil
		}
	}
	if ws, _ := s.defaultWorkspace.Load().(string); strings.TrimSpace(ws) != "" {
		return ws, filesSourceDefault, nil
	}
	wd, werr := os.Getwd()
	if werr != nil {
		return "", "", werr
	}
	return wd, filesSourceCwd, nil
}

// handleFilesBrowse serves GET /files/browse?path=<rel>&session=<id>,
// listing one directory of the RESOLVED root (the resolution law). Empty
// or missing path lists the root. The response carries root + source so
// the tab's header knows exactly what it sits on.
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
	root, source, err := s.resolveFilesRoot(r.Context(), r.URL.Query().Get("session"))
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
	writeJSON(w, http.StatusOK, map[string]any{
		"entries": sortFileEntries(entries),
		"root":    root,
		"source":  source,
	})
}

// handleFilesRead serves GET /files/read?path=<rel>&session=<id>,
// returning up to 64 KiB of a file's content under the RESOLVED root
// (the resolution law, same chain as browse). Non-UTF-8 bytes come back
// as binary=true with empty content - no binary previews on this
// surface.
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
	root, _, err := s.resolveFilesRoot(r.Context(), r.URL.Query().Get("session"))
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

// filesPostCap caps every POST body on the file surface: the path and
// the file content ride one JSON envelope, and an unbounded Decode
// before validation is a DoS seam on the same class as an unjailed path.
const filesPostCap = 1 << 20 // 1 MiB

// filesPostBody decodes the JSON request body of the file surface's
// POST handlers into dst, capped at filesPostCap+1 bytes so an
// oversized body fails loudly instead of silently truncating mid-JSON.
// Writes the method-405 / decode-400 envelope and returns false when
// the request never reaches the handler's real body of work.
func filesPostBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, filesPostCap+1)).Decode(dst); err != nil {
		fileError(w, http.StatusBadRequest, "decode body: "+err.Error())
		return false
	}
	return true
}

// errUncontained marks the write half's request-fault refusals: a path
// the segment or containment rules reject is the CALLER's malformed
// request (400), while anything else failing to resolve is the server's
// (500) - the same split the browse handler draws on status codes.
var errUncontained = errors.New("path refused")

// resolveForWrite runs the shared root jail over a write request's
// path: segment validation, symlink resolution, containment - the exact
// pair resolveUnderRoot + ensureUnderRoot the browse/read handlers run,
// never a copy. Returns the root-relative path and the resolved
// absolute path.
func resolveForWrite(root, req string) (rel, resolved string, err error) {
	rel, err = validateRelPath(req)
	if err != nil {
		return "", "", fmt.Errorf("%w: %v", errUncontained, err)
	}
	realRoot, resolved, rerr := resolveUnderRoot(root, rel)
	if rerr != nil {
		// fail closed: no containment verdict, no disk write. A missing
		// leaf or parent is the caller's made-up path (loud 400); the
		// vanished-working-directory edge rides the same refusal - the
		// write never happens either way.
		if os.IsNotExist(rerr) {
			return "", "", fmt.Errorf("%w: %v", errUncontained, rerr)
		}
		return "", "", rerr
	}
	if err := ensureUnderRoot(realRoot, resolved); err != nil {
		return "", "", fmt.Errorf("%w: %v", errUncontained, err)
	}
	return rel, resolved, nil
}

// handleFilesWrite serves POST /files/write {path, content}: creates or
// overwrites one file inside the working directory. An empty name is a
// loud 400 - there is no "write the root". The parent directory must
// already exist (the toolbar's add-folder button makes it), the leaf
// must not be a directory, and empty content is a legal empty file.
func (s *Server) handleFilesWrite(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path    string `json:"path"`
		Content string `json:"content"`
		Session string `json:"session,omitempty"`
	}
	if !filesPostBody(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.Path) == "" {
		fileError(w, http.StatusBadRequest, "empty path")
		return
	}
	root, _, err := s.resolveFilesRoot(r.Context(), body.Session)
	if err != nil {
		fileError(w, http.StatusInternalServerError, "resolve working directory: "+err.Error())
		return
	}
	rel, resolved, err := resolveForWrite(root, body.Path)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, errUncontained) {
			status = http.StatusBadRequest
		}
		fileError(w, status, err.Error())
		return
	}
	if info, serr := os.Stat(resolved); serr == nil && info.IsDir() {
		fileError(w, http.StatusBadRequest, "is a directory")
		return
	}
	parent, perr := os.Stat(filepath.Dir(resolved))
	if perr != nil || !parent.IsDir() {
		if perr == nil {
			perr = errors.New("parent is not a directory")
		}
		fileError(w, http.StatusBadRequest, "parent directory: "+perr.Error())
		return
	}
	if werr := os.WriteFile(resolved, []byte(body.Content), 0o644); werr != nil {
		fileError(w, http.StatusInternalServerError, "write file: "+werr.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "path": rel})
}

// handleFilesMkdir serves POST /files/mkdir {path, session}: creates one
// folder inside the RESOLVED root. Empty and existing names are loud
// 400s; the leaf chain must already exist above the leaf - the same
// resolution rule the write half runs under (no made-up parent chains,
// no symlinks smuggled through a half-existing path).
func (s *Server) handleFilesMkdir(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path    string `json:"path"`
		Session string `json:"session,omitempty"`
	}
	if !filesPostBody(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.Path) == "" {
		fileError(w, http.StatusBadRequest, "empty path")
		return
	}
	root, _, err := s.resolveFilesRoot(r.Context(), body.Session)
	if err != nil {
		fileError(w, http.StatusInternalServerError, "resolve working directory: "+err.Error())
		return
	}
	rel, resolved, err := resolveForWrite(root, body.Path)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, errUncontained) {
			status = http.StatusBadRequest
		}
		fileError(w, status, err.Error())
		return
	}
	if _, serr := os.Stat(resolved); serr == nil {
		fileError(w, http.StatusBadRequest, "already exists")
		return
	}
	if err := os.MkdirAll(resolved, 0o755); err != nil {
		fileError(w, http.StatusInternalServerError, "create directory: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "path": rel})
}
