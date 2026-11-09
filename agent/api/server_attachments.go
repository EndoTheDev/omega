package api

// Non-image attachment uploads (card t_1ffd07de): the composer's attach
// strip sends the BYTES to the server once, the server writes them into
// the session's upload directory, and the message carries a typed
// reference (core.Attachment: kind, name, size, path) - never the
// content. No text extraction, no inlining, no parsing happens here;
// the agent reads the file through its own tools (card t_2eb2d274).
//
// The image lane is untouched: /attach-image + core.User.Images still
// ride base64 on the message (server_images.go). This file is the
// non-image sibling of that lane, on the same composer attach surface.

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/EndoTheDev/omega/core"
)

// maxChatFileBytes is the per-file cap for non-image attachments. It is
// the SAME class as the image lane's cap by law (maxChatImageBytes, 10
// MB): one browser-facing ceiling, not two numbers to keep in sync.
const maxChatFileBytes = maxChatImageBytes

// uploadsDirName is the folder inside an omega home that holds every
// uploaded attachment: <home>/uploads.
const uploadsDirName = "uploads"

// pendingDirName is the staging folder for a draft chat, whose upload
// happens BEFORE any session id exists: <uploads>/_pending/<token>/.
// /chat moves the file into the session's own dir the moment the id
// exists (resolveAttachments). The leading underscore keeps the name
// out of the session-id namespace.
const pendingDirName = "_pending"

// uploadsRoot resolves <home>/uploads, the tree every attachment lands
// in: OMEGA_UPLOADS_DIR overrides (tests, odd deployments), else
// OMEGA_HOME/uploads, else ~/.omega/uploads - the same fallback chain
// datadir.Home() uses. An empty answer means no home could be resolved:
// the attach surface answers 501 rather than writing somewhere
// arbitrary.
func uploadsRoot() string {
	if d := os.Getenv("OMEGA_UPLOADS_DIR"); d != "" {
		return filepath.Clean(d)
	}
	home := os.Getenv("OMEGA_HOME")
	if home == "" {
		u, err := os.UserHomeDir()
		if err != nil || u == "" {
			return ""
		}
		home = filepath.Join(u, ".omega")
	}
	return filepath.Join(home, uploadsDirName)
}

// safeUploadName derives the on-disk name for an uploaded file: the base
// name only (a client can never steer a write outside the target dir),
// path separators and control bytes folded to "_", leading dots dropped
// (no hidden files, no ".."), over-long names trimmed to their tail (the
// extension survives - kind and type icons read it), and an empty result
// replaced by "file".
func safeUploadName(raw string) string {
	name := filepath.Base(strings.ReplaceAll(raw, `\`, "/"))
	name = strings.Map(func(r rune) rune {
		if r == '/' || r < 0x20 || r == 0x7f {
			return '_'
		}
		return r
	}, name)
	name = strings.TrimLeft(name, ".")
	if r := []rune(name); len(r) > 120 {
		name = string(r[len(r)-120:])
	}
	if name == "" {
		name = "file"
	}
	return name
}

// attachmentKind is the wire contract's Kind: the lowercased extension
// without its dot ("pdf", "md", "csv", "json", "txt"), and "file" when
// the name carries none. A label, not a claim about the bytes - nothing
// in this lane inspects content.
func attachmentKind(name string) string {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
	if ext == "" {
		return "file"
	}
	return ext
}

// sessionUploadDir is one session's upload directory:
// <uploads>/<sessionID>. A session id that is empty, reserved
// ("_"-prefixed), or not a plain path segment is refused - the caller
// surfaces it.
func sessionUploadDir(root, sessionID string) (string, error) {
	id := strings.TrimSpace(sessionID)
	if id == "" {
		return "", errors.New("session id is required")
	}
	if strings.HasPrefix(id, "_") {
		return "", fmt.Errorf("session id %q uses the reserved upload namespace", id)
	}
	if id != safeUploadName(id) {
		return "", fmt.Errorf("session id %q is not a plain path segment", id)
	}
	return filepath.Join(root, id), nil
}

// createUniqueUpload creates dir/name without clobbering an existing
// file: two attaches of "notes.md" both survive as "notes.md" and
// "notes-1.md". The O_EXCL create is the dedupe (no stat race).
func createUniqueUpload(dir, name string) (*os.File, error) {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 0; ; i++ {
		candidate := name
		if i > 0 {
			candidate = fmt.Sprintf("%s-%d%s", stem, i, ext)
		}
		f, err := os.OpenFile(filepath.Join(dir, candidate), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			return f, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
	}
}

// handleAttachFile serves POST /attach-file (multipart/form-data): one
// "file" part plus an optional "session_id" text part. The bytes land in
// the session's upload directory - or in the _pending staging dir when
// the request carries no session id (a draft chat; /chat relocates it) -
// and the answer is the core.Attachment reference the client echoes back
// on the message. Content-Type is the client's own claim; this lane
// never sniffs, parses or extracts.
func (s *Server) handleAttachFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	root := uploadsRoot()
	if root == "" {
		http.Error(w, "attachments need an omega home (set OMEGA_HOME)", http.StatusNotImplemented)
		return
	}
	mr, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "invalid request: multipart/form-data required", http.StatusBadRequest)
		return
	}
	var sessionID string
	var saved *core.Attachment
	for {
		part, perr := mr.NextPart()
		if errors.Is(perr, io.EOF) {
			break
		}
		if perr != nil {
			http.Error(w, "invalid request: "+perr.Error(), http.StatusBadRequest)
			return
		}
		switch part.FormName() {
		case "session_id":
			raw, rerr := io.ReadAll(io.LimitReader(part, 256))
			if rerr != nil {
				part.Close() //nolint:errcheck // best-effort
				http.Error(w, "invalid request: "+rerr.Error(), http.StatusBadRequest)
				return
			}
			sessionID = strings.TrimSpace(string(raw))
		case "file":
			if saved != nil {
				part.Close() //nolint:errcheck // best-effort
				http.Error(w, "invalid request: one file per request", http.StatusBadRequest)
				return
			}
			att, serr := saveUpload(root, sessionID, part)
			if serr != nil {
				part.Close() //nolint:errcheck // best-effort
				status := http.StatusBadRequest
				if errors.Is(serr, errUploadTooLarge) {
					status = http.StatusRequestEntityTooLarge
				}
				http.Error(w, serr.Error(), status)
				return
			}
			saved = att
		}
		part.Close() //nolint:errcheck // best-effort
	}
	if saved == nil {
		http.Error(w, "invalid request: file part is required", http.StatusBadRequest)
		return
	}
	encoded, err := json.Marshal(saved)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(encoded) //nolint:errcheck // best-effort response
}

// errUploadTooLarge separates the size refusal from the other write
// errors so the handler can answer 413.
var errUploadTooLarge = errors.New("file too large")

// saveUpload streams one multipart part into the target directory and
// returns its reference. The size cap is enforced WHILE writing (one
// byte over the cap aborts and removes the partial file), so an
// oversized upload never sits on disk.
func saveUpload(root, sessionID string, part multipartFilePart) (*core.Attachment, error) {
	rawName := part.FileName()
	if strings.TrimSpace(rawName) == "" {
		return nil, errors.New("invalid request: file part carries no filename")
	}
	name := safeUploadName(rawName)

	dir := ""
	if sessionID == "" {
		token, err := newPendingToken()
		if err != nil {
			return nil, err
		}
		dir = filepath.Join(root, pendingDirName, token)
	} else {
		d, err := sessionUploadDir(root, sessionID)
		if err != nil {
			return nil, err
		}
		dir = d
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create upload dir: %w", err)
	}
	f, err := createUniqueUpload(dir, name)
	if err != nil {
		return nil, fmt.Errorf("create upload file: %w", err)
	}
	path := f.Name()
	n, copyErr := io.Copy(f, io.LimitReader(part, maxChatFileBytes+1))
	closeErr := f.Close()
	if copyErr != nil {
		_ = os.Remove(path)
		return nil, fmt.Errorf("write upload: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(path)
		return nil, fmt.Errorf("write upload: %w", closeErr)
	}
	if n > maxChatFileBytes {
		_ = os.Remove(path)
		return nil, fmt.Errorf("%w (%d bytes, max %d)", errUploadTooLarge, n, maxChatFileBytes)
	}
	abs, absErr := filepath.Abs(path)
	if absErr != nil {
		abs = path
	}
	return &core.Attachment{
		Kind: attachmentKind(name),
		Name: filepath.Base(path),
		Size: n,
		Path: abs,
	}, nil
}

// multipartFilePart is the sliver of *multipart.Part saveUpload needs
// (FileName + Read), kept narrow so the tests can feed bytes directly.
type multipartFilePart interface {
	io.Reader
	FileName() string
}

// newPendingToken mints the staging dir name for one draft-chat upload.
func newPendingToken() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("mint upload token: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// resolveAttachments validates every attachment reference on the
// incoming user messages against the uploads tree and - when the
// reference still points into the _pending staging dir and a session id
// exists - MOVES the file into <uploads>/<sessionID>/ and rewrites the
// reference. The messages are edited in place (the caller's slice holds
// the rewritten User values).
//
// The path rule is the security wall: the reference must resolve to a
// regular file INSIDE the uploads tree (symlinks resolved, traversal
// refused), and it must fit maxChatFileBytes. The client supplies the
// path, so nothing outside the uploads dir is ever accepted as an
// attachment - a reference cannot become a read of the home, the
// config, or the box.
func resolveAttachments(messages []core.Message, sessionID string) error {
	root := uploadsRoot()
	for i := range messages {
		user, ok := messages[i].(core.User)
		if !ok || len(user.Attachments) == 0 {
			continue
		}
		for j := range user.Attachments {
			if err := resolveAttachment(&user.Attachments[j], root, sessionID); err != nil {
				return fmt.Errorf("message %d attachment %d: %w", i, j, err)
			}
		}
		messages[i] = user
	}
	return nil
}

// resolveAttachment is resolveAttachments' per-reference half: validate,
// then relocate a staged draft upload once the session id exists.
func resolveAttachment(att *core.Attachment, root, sessionID string) error {
	resolved, info, err := validateAttachmentPath(att.Path, root)
	if err != nil {
		return err
	}
	if info.Size() > maxChatFileBytes {
		return fmt.Errorf("%w (%d bytes, max %d)", errUploadTooLarge, info.Size(), maxChatFileBytes)
	}
	att.Path = resolved
	att.Size = info.Size()
	if att.Name == "" {
		att.Name = filepath.Base(resolved)
	}
	if att.Kind == "" {
		att.Kind = attachmentKind(att.Name)
	}
	if sessionID == "" {
		return nil
	}
	// A draft's file lands in <uploads>/_pending/<token>/; the session it
	// was attached to exists only now. Move it home and rewrite the
	// reference so the persisted message names the session's own dir.
	rel, relErr := filepath.Rel(root, resolved)
	if relErr != nil {
		return nil
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if parts[0] != pendingDirName {
		return nil
	}
	dir, err := sessionUploadDir(root, sessionID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create session upload dir: %w", err)
	}
	f, err := createUniqueUpload(dir, safeUploadName(att.Name))
	if err != nil {
		return fmt.Errorf("relocate upload: %w", err)
	}
	dest := f.Name()
	src, err := os.Open(resolved)
	if err != nil {
		_ = f.Close()
		_ = os.Remove(dest)
		return fmt.Errorf("relocate upload: %w", err)
	}
	_, copyErr := io.Copy(f, src)
	_ = src.Close()
	if copyErr != nil {
		_ = f.Close()
		_ = os.Remove(dest)
		return fmt.Errorf("relocate upload: %w", copyErr)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(dest)
		return fmt.Errorf("relocate upload: %w", err)
	}
	if err := os.Remove(resolved); err != nil {
		_ = os.Remove(dest)
		return fmt.Errorf("relocate upload: %w", err)
	}
	_ = os.Remove(filepath.Dir(resolved)) // the token dir, empty now
	abs, absErr := filepath.Abs(dest)
	if absErr == nil {
		dest = abs
	}
	att.Path = dest
	att.Name = filepath.Base(dest)
	return nil
}

// validateAttachmentPath resolves one client-supplied path against the
// uploads tree and returns the real on-disk path plus its FileInfo. The
// rules: absolute or uploads-relative, inside root after symlink
// resolution, an existing regular file.
func validateAttachmentPath(raw, root string) (string, os.FileInfo, error) {
	if root == "" {
		return "", nil, errors.New("attachments need an omega home (set OMEGA_HOME)")
	}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil, errors.New("path is required")
	}
	if !filepath.IsAbs(trimmed) {
		trimmed = filepath.Join(root, trimmed)
	}
	rootReal := root
	if rr, err := filepath.EvalSymlinks(root); err == nil {
		rootReal = rr
	}
	path := filepath.Clean(trimmed)
	if rp, err := filepath.EvalSymlinks(path); err == nil {
		path = rp
	}
	rel, err := filepath.Rel(rootReal, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", nil, fmt.Errorf("path %q is outside the uploads directory", raw)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", nil, fmt.Errorf("stat upload: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", nil, errors.New("upload is not a regular file")
	}
	return path, info, nil
}
