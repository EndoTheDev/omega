// The file domain's REST face (the webui explorer): browse, read and
// preview the RESOLVED root, the raw image bytes, plus the write half
// (card t_a4504a25, the Files tab toolbar's add-file/add-folder).
// GET routes answer reads; POST /files/write and POST /files/mkdir
// create nodes INSIDE the same root, guarded by the SAME three layers
// the read half enforces: relative paths and empty segments are
// rejected, ONLY the "." and ".." traversal segments die (hidden names
// like .git and .gitignore are ordinary paths - hiding them is the
// listings' job, never the guard's), and the cleaned path is
// symlink-resolved and verified to stay under the root before the disk
// is touched. The routes are capsule vocabulary (the module REST rail,
// RegisterRoute): a disabled files capsule answers 404 at the module
// mux - the explorer is the file tools wearing a second face.

package files

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
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/EndoTheDev/omega/core"
)

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
// Absolute paths and empty segments are rejected, and the ONLY dot
// segments rejected are "." and ".." - the traversal set (card
// t_ccd4a000 narrowed this guard from every dot-prefixed segment: its
// security job is traversal prevention, not name hiding; the earlier
// width made listed dot-folders (.git, .github, .worktrees) and dot
// files (.gitignore) permanently unopenable while hiding nothing).
// Containment never rests on this scan: every caller resolves the
// cleaned path under the root via EvalSymlinks and verifies it with
// ensureUnderRoot.
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
		if part == "." || part == ".." {
			return "", errors.New("traversal path segment")
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

// writeJSON writes v as a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck // the client is gone; nothing to report to
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

// sessionStores is the workspace-pin face the roots chain reads (the
// store capsule offered under the "store" key): the resolved root's
// step 1 - the focused session's pinned workspace. The capsule's own
// local interface per the seam grammar; nil-safe absence = the chain
// falls through to the default knob and the cwd.
type sessionStores interface {
	GetSession(ctx context.Context, id string) (core.Session, error)
}

// Routes is the file domain's REST face: the five explorer routes
// bound under the capsule's /files namespace, the default_workspace
// knob (hot: the resolution law reads it per request), and the live
// store resolver (the seam resolve per request - a toggle swaps the
// offer under the handlers' feet, exactly the Server's seam law).
//
// The knob's default arrives at mount from the capsule's config
// section (cfg.Server.DefaultWorkspace, card t_a5f9d185); the
// settings seam's hot-apply pushes later saves through
// SetDefaultWorkspace, so a settings-pane save re-roots the Files tab
// live, no restart.
type Routes struct {
	// storeFn resolves the workspace-pin face LIVE per request.
	storeFn func() sessionStores
	// defaultWorkspace is the `default_workspace` knob. Written by
	// SetDefaultWorkspace (wire time + the settings seam's hot-apply)
	// while requests read it concurrently, so it rides an atomic
	// value - never a bare field.
	defaultWorkspace atomic.Value
}

// SetDefaultWorkspace hot-sets the default_workspace knob: the mid
// chain step of the root resolution law.
func (r *Routes) SetDefaultWorkspace(ws string) {
	r.defaultWorkspace.Store(ws)
}

// newRoutes builds the route surface against the live store resolver.
func newRoutes(ctx *core.Context) *Routes {
	return &Routes{
		storeFn: func() sessionStores { return seam[sessionStores](ctx, "store") },
	}
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
func (r *Routes) resolveFilesRoot(ctx context.Context, sessionID string) (root, source string, err error) {
	if sessionID != "" {
		sess := r.storeFn()
		if sess != nil {
			if s, ferr := sess.GetSession(ctx, sessionID); ferr == nil &&
				strings.TrimSpace(s.Workspace) != "" {
				return s.Workspace, filesSourceSession, nil
			}
		}
	}
	if ws, _ := r.defaultWorkspace.Load().(string); strings.TrimSpace(ws) != "" {
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
func (r *Routes) handleFilesBrowse(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rel, err := validateRelPath(req.URL.Query().Get("path"))
	if err != nil {
		fileError(w, http.StatusBadRequest, err.Error())
		return
	}
	root, source, err := r.resolveFilesRoot(req.Context(), req.URL.Query().Get("session"))
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
// as binary=true with empty content - the content is not previewed here;
// the mime field tells the viewer whether the file is an image it can
// pull from /files/raw (card t_08d822bb) or a binary it announces.
func (r *Routes) handleFilesRead(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rel, err := validateRelPath(req.URL.Query().Get("path"))
	if err != nil {
		fileError(w, http.StatusBadRequest, err.Error())
		return
	}
	root, _, err := r.resolveFilesRoot(req.Context(), req.URL.Query().Get("session"))
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
		"mime":      detectFileMime(data[:min(512, len(data))], rel),
	})
}

// detectFileMime sniffs a file's leading bytes (the card's law: stdlib
// http.DetectContentType, max 512 bytes, no new dependency) and returns
// the file's mime type. Extension guessing is ONLY the tiebreak the card
// allows: DetectContentType has no SVG magic, so a text-shaped file
// named .svg climbs to image/svg+xml; every other file rides the sniff
// verbatim, extension or not.
func detectFileMime(head []byte, rel string) string {
	sniff := http.DetectContentType(head)
	if strings.HasSuffix(strings.ToLower(rel), ".svg") &&
		(sniff == "text/plain; charset=utf-8" ||
			strings.HasPrefix(sniff, "text/xml") ||
			strings.HasPrefix(sniff, "application/xml")) {
		return "image/svg+xml"
	}
	return sniff
}

// handleFilesRaw serves GET /files/raw?path=<rel>&session=<id>: the raw
// bytes behind the viewer tab's image branch (card t_08d822bb). The
// containment is the EXACT chain /files/read runs - validateRelPath,
// resolveFilesRoot, resolveUnderRoot, ensureUnderRoot, never a copy -
// and one extra gate the read surface does not have: the sniffed mime
// must be image/*, anything else refuses with 400 (the card's v1 scope
// is images only - no pdf/audio/video surface). The file streams with
// the sniffed Content-Type plus nosniff, so a mislabelled payload can
// never render as something it is not.
func (r *Routes) handleFilesRaw(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rel, err := validateRelPath(req.URL.Query().Get("path"))
	if err != nil {
		fileError(w, http.StatusBadRequest, err.Error())
		return
	}
	root, _, err := r.resolveFilesRoot(req.Context(), req.URL.Query().Get("session"))
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
	head := make([]byte, 512)
	n, herr := f.Read(head)
	if herr != nil && n == 0 && !errors.Is(herr, io.EOF) {
		fileError(w, http.StatusInternalServerError, "read file: "+herr.Error())
		return
	}
	mime := detectFileMime(head[:n], rel)
	if !strings.HasPrefix(mime, "image/") {
		fileError(w, http.StatusBadRequest, "not an image - the raw route serves image/* only")
		return
	}
	if _, serr := f.Seek(0, io.SeekStart); serr != nil {
		fileError(w, http.StatusInternalServerError, "rewind file: "+serr.Error())
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	if _, cerr := io.Copy(w, f); cerr != nil {
		// the response already started; nothing structured left to send
		return
	}
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
func (r *Routes) handleFilesWrite(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Path    string `json:"path"`
		Content string `json:"content"`
		Session string `json:"session,omitempty"`
	}
	if !filesPostBody(w, req, &body) {
		return
	}
	if strings.TrimSpace(body.Path) == "" {
		fileError(w, http.StatusBadRequest, "empty path")
		return
	}
	root, _, err := r.resolveFilesRoot(req.Context(), body.Session)
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
	// the write is atomic (v0.14.1 audit, the same tmp+rename class the
	// config writes run): the previous os.WriteFile could truncate an
	// existing user file when the process died mid-write. Same-directory
	// tmp keeps the rename same-filesystem; the leaf exists untouched
	// until the rename lands.
	tmpFile, terr := os.CreateTemp(filepath.Dir(resolved), filepath.Base(resolved)+".omega-write-*")
	if terr != nil {
		fileError(w, http.StatusInternalServerError, "write file: "+terr.Error())
		return
	}
	tmp := tmpFile.Name()
	if _, werr := tmpFile.WriteString(body.Content); werr != nil {
		tmpFile.Close() //nolint:errcheck // best-effort cleanup
		os.Remove(tmp)  //nolint:errcheck // best-effort cleanup
		fileError(w, http.StatusInternalServerError, "write file: "+werr.Error())
		return
	}
	if cerr := tmpFile.Close(); cerr != nil {
		os.Remove(tmp) //nolint:errcheck // best-effort cleanup
		fileError(w, http.StatusInternalServerError, "write file: "+cerr.Error())
		return
	}
	if werr := os.Chmod(tmp, 0o644); werr != nil {
		os.Remove(tmp) //nolint:errcheck // best-effort cleanup
		fileError(w, http.StatusInternalServerError, "write file: "+werr.Error())
		return
	}
	if werr := os.Rename(tmp, resolved); werr != nil {
		os.Remove(tmp) //nolint:errcheck // best-effort cleanup
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
func (r *Routes) handleFilesMkdir(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Path    string `json:"path"`
		Session string `json:"session,omitempty"`
	}
	if !filesPostBody(w, req, &body) {
		return
	}
	if strings.TrimSpace(body.Path) == "" {
		fileError(w, http.StatusBadRequest, "empty path")
		return
	}
	root, _, err := r.resolveFilesRoot(req.Context(), body.Session)
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