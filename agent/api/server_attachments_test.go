package api

// Non-image attachment uploads (card t_1ffd07de): POST /attach-file
// writes the bytes into the session's upload dir, /chat carries the
// typed reference and persists it, and nothing outside the uploads tree
// is ever accepted as a reference.

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// attachFilePOST builds the multipart request the composer sends: an
// optional session_id field plus one file part.
func attachFilePOST(t *testing.T, sessionID, filename string, content []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if sessionID != "" {
		if err := mw.WriteField("session_id", sessionID); err != nil {
			t.Fatalf("write session field: %v", err)
		}
	}
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("create file part: %v", err)
	}
	if _, err := fw.Write(content); err != nil {
		t.Fatalf("write file part: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/attach-file", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

// attachFile uploads one file and returns the reference record.
func attachFile(t *testing.T, s *Server, sessionID, filename string, content []byte) core.Attachment {
	t.Helper()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, attachFilePOST(t, sessionID, filename, content))
	if rec.Code != http.StatusOK {
		t.Fatalf("attach-file %s status = %d, want 200 (%s)", filename, rec.Code, rec.Body.String())
	}
	var att core.Attachment
	if err := json.Unmarshal(rec.Body.Bytes(), &att); err != nil {
		t.Fatalf("decode attachment: %v", err)
	}
	return att
}

// TestAttachFileDraftUploadLandsInSessionDir is the card's receipt: a .md
// and a .csv attached through the API both land in the session's upload
// dir, and the persisted user message carries both reference records.
// The .md rides an existing session id; the .csv is a draft attach (no
// session yet), so it starts in the _pending staging dir and /chat moves
// it into the session's dir.
func TestAttachFileDraftUploadLandsInSessionDir(t *testing.T) {
	uploads := t.TempDir()
	t.Setenv("OMEGA_UPLOADS_DIR", uploads)
	s, store := newTestServerWithStore(t)
	ctx := context.Background()
	if err := store.CreateSession(ctx, "s1", "", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}

	mdBytes := []byte("# Notes\n\nan attached markdown file\n")
	csvBytes := []byte("id,name\n1,alpha\n2,beta\n")
	md := attachFile(t, s, "s1", "notes.md", mdBytes)
	csv := attachFile(t, s, "", "rows.csv", csvBytes)

	if md.Kind != "md" || md.Name != "notes.md" || md.Size != int64(len(mdBytes)) {
		t.Fatalf("md reference = %+v, want kind md / name notes.md / size %d", md, len(mdBytes))
	}
	if want := filepath.Join(uploads, "s1"); filepath.Dir(md.Path) != want {
		t.Fatalf("md path = %q, want it under %q", md.Path, want)
	}
	if csv.Kind != "csv" || csv.Name != "rows.csv" || csv.Size != int64(len(csvBytes)) {
		t.Fatalf("csv reference = %+v, want kind csv / name rows.csv / size %d", csv, len(csvBytes))
	}
	if rel, _ := filepath.Rel(filepath.Join(uploads, pendingDirName), csv.Path); strings.HasPrefix(rel, "..") {
		t.Fatalf("draft csv path = %q, want it staged under %s/", csv.Path, pendingDirName)
	}

	// The send: both references ride the user message.
	body, _ := json.Marshal(map[string]any{
		"session_id": "s1",
		"messages": []map[string]any{{
			"role":        "user",
			"content":     "what is in these files",
			"attachments": []core.Attachment{md, csv},
		}},
	})
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/chat", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("chat status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}

	// Both files now live in the SESSION's upload dir, bytes intact.
	sessionDir := filepath.Join(uploads, "s1")
	for name, want := range map[string][]byte{"notes.md": mdBytes, "rows.csv": csvBytes} {
		got, err := os.ReadFile(filepath.Join(sessionDir, name))
		if err != nil {
			t.Fatalf("read %s from the session upload dir: %v", name, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s bytes = %q, want %q", name, got, want)
		}
	}
	// The staging dir is drained: no file survives under _pending.
	pending := filepath.Join(uploads, pendingDirName)
	entries, _ := os.ReadDir(pending)
	if len(entries) != 0 {
		t.Fatalf("staging dir still carries %d entries, want the draft file relocated", len(entries))
	}

	// The persisted user message carries both reference records, pointing
	// into the session's own dir.
	messages, err := store.GetMessages(ctx, "s1")
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(messages) == 0 {
		t.Fatal("no messages persisted")
	}
	user, ok := messages[0].(core.User)
	if !ok {
		t.Fatalf("messages[0] = %T, want core.User", messages[0])
	}
	if len(user.Attachments) != 2 {
		t.Fatalf("persisted attachments = %d, want 2 (%+v)", len(user.Attachments), user.Attachments)
	}
	for i, want := range []struct{ kind, name string }{{"md", "notes.md"}, {"csv", "rows.csv"}} {
		got := user.Attachments[i]
		if got.Kind != want.kind || got.Name != want.name {
			t.Fatalf("attachment %d = %+v, want kind %s name %s", i, got, want.kind, want.name)
		}
		if filepath.Dir(got.Path) != sessionDir {
			t.Fatalf("attachment %d path = %q, want it under %q", i, got.Path, sessionDir)
		}
		if got.Size <= 0 {
			t.Fatalf("attachment %d size = %d, want the on-disk byte count", i, got.Size)
		}
	}
}

// TestAttachFileRejectsOversize pins the cap: one byte past
// maxChatFileBytes is a 413 and nothing is left on disk.
func TestAttachFileRejectsOversize(t *testing.T) {
	uploads := t.TempDir()
	t.Setenv("OMEGA_UPLOADS_DIR", uploads)
	s := newTestServer()

	big := bytes.Repeat([]byte("a"), maxChatFileBytes+1)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, attachFilePOST(t, "s1", "big.txt", big))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413 (%s)", rec.Code, rec.Body.String())
	}
	dir := filepath.Join(uploads, "s1")
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("oversize upload left %d files behind, want none", len(entries))
	}
}

// TestAttachFileRejectsForeignPath pins the security wall: a reference
// path outside the uploads tree is a 400 on /chat, never a read.
func TestAttachFileRejectsForeignPath(t *testing.T) {
	uploads := t.TempDir()
	t.Setenv("OMEGA_UPLOADS_DIR", uploads)
	s, store := newTestServerWithStore(t)
	ctx := context.Background()
	if err := store.CreateSession(ctx, "s1", "", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}

	outside := filepath.Join(t.TempDir(), "secrets.txt")
	if err := os.WriteFile(outside, []byte("nope"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	body, _ := json.Marshal(map[string]any{
		"session_id": "s1",
		"messages": []map[string]any{{
			"role":    "user",
			"content": "hello",
			"attachments": []core.Attachment{
				{Kind: "txt", Name: "secrets.txt", Size: 4, Path: outside},
			},
		}},
	})
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/chat", bytes.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "outside the uploads directory") {
		t.Fatalf("body = %q, want the outside-the-uploads refusal", rec.Body.String())
	}
}

// TestSafeUploadName pins the on-disk name rule: a client-supplied
// filename can never steer the write (no separators, no traversal, no
// hidden names), and the extension survives for the kind label.
func TestSafeUploadName(t *testing.T) {
	cases := map[string]string{
		"notes.md":                  "notes.md",
		"../../etc/passwd":          "passwd",
		`..\..\windows\win.ini`:     "win.ini",
		"/absolute/path/report.pdf": "report.pdf",
		".hidden":                   "hidden",
		"..":                        "file",
		"":                          "file",
		"we ird\x01name.csv":        "we ird_name.csv",
	}
	for raw, want := range cases {
		if got := safeUploadName(raw); got != want {
			t.Errorf("safeUploadName(%q) = %q, want %q", raw, got, want)
		}
	}
}
