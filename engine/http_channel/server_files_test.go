package http_channel

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// chdirToWorkdir points the file surface at a temp directory: the
// handlers root on the process working directory, so the tests chdir
// into it.
func chdirToWorkdir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { os.Chdir("/") }) //nolint:errcheck // test cleanup only
	return dir
}

func filesGet(t *testing.T, path string) (*http.Response, map[string]any) {
	t.Helper()
	s := newTestServer()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	res := rec.Result()
	var body map[string]any
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	return res, body
}

func TestValidateRelPath(t *testing.T) {
	accept := map[string]string{
		"":             "", // empty = root
		"go.mod":       "go.mod",
		"cmd/omega":    "cmd/omega",
		"docs/a b.txt": "docs/a b.txt",
	}
	for in, want := range accept {
		got, err := validateRelPath(in)
		if err != nil || got != want {
			t.Errorf("validateRelPath(%q) = %q, %v; want %q, nil", in, got, err, want)
		}
	}
	reject := []string{
		"/etc/passwd",  // absolute
		"..",           // traversal
		"a/../b",       // traversal segment
		"a//b",         // empty segment
		"a/",           // trailing empty segment
		".git",         // dot-prefixed segment
		"docs/.hidden", // dot-prefixed nested segment
		"./x",          // dot-prefixed segment
	}
	for _, in := range reject {
		if got, err := validateRelPath(in); err == nil {
			t.Errorf("validateRelPath(%q) = %q, nil; want error", in, got)
		}
	}
}

func TestSortFileEntries(t *testing.T) {
	in := []fileEntry{
		{Name: "zeta.txt"},
		{Name: "beta", IsDir: true},
		{Name: "Alpha.txt"},
		{Name: "alpha.txt"},
		{Name: "apple", IsDir: true},
	}
	got := sortFileEntries(in)
	// Alpha.txt sorts equal to alpha.txt (case-insensitive); stable sort
	// keeps the input order for the pair
	want := []string{"apple", "beta", "Alpha.txt", "alpha.txt", "zeta.txt"}
	if len(got) != len(want) {
		t.Fatalf("sortFileEntries returned %d entries; want %d", len(got), len(want))
	}
	for i, e := range got {
		if e.Name != want[i] {
			t.Errorf("entry[%d] = %q; want %q", i, e.Name, want[i])
		}
	}
	// pure: the input slice stays untouched
	if in[3].Name != "alpha.txt" {
		t.Errorf("sortFileEntries mutated its input")
	}
}

func TestEnsureUnderRoot(t *testing.T) {
	if err := ensureUnderRoot("/work", "/work"); err != nil {
		t.Errorf("root itself rejected: %v", err)
	}
	if err := ensureUnderRoot("/work", "/work/sub/file"); err != nil {
		t.Errorf("subpath rejected: %v", err)
	}
	// a literal prefix check - callers pass EvalSymlinks-resolved paths,
	// so a ".." segment can never reach this function (validateRelPath
	// rejects them upstream; the handler tests cover the full request path)
	for _, p := range []string{"/workup", "/etc/passwd", "/worker/file"} {
		if err := ensureUnderRoot("/work", p); err == nil {
			t.Errorf("ensureUnderRoot accepted escape %q", p)
		}
	}
}

func TestFilesBrowseShapeAndSort(t *testing.T) {
	dir := chdirToWorkdir(t)
	for _, d := range []string{"beta", "a-dir"} {
		os.Mkdir(filepath.Join(dir, d), 0o755) //nolint:errcheck // setup
	}
	for _, f := range []string{"zeta.txt", "Alpha.txt"} {
		os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644) //nolint:errcheck // setup
	}
	res, body := filesGet(t, "/files/browse")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("browse status = %d; want 200", res.StatusCode)
	}
	raw, _ := json.Marshal(body["entries"])
	var entries []fileEntry
	json.Unmarshal(raw, &entries)
	want := []string{"a-dir", "beta", "Alpha.txt", "zeta.txt"}
	if len(entries) != len(want) {
		t.Fatalf("got %d entries; want %d", len(entries), len(want))
	}
	for i, e := range entries {
		if e.Name != want[i] {
			t.Errorf("entry[%d] = %q; want %q", i, e.Name, want[i])
		}
		if e.IsDir != (i < 2) {
			t.Errorf("entry[%d] is_dir = %v", i, e.IsDir)
		}
		if e.ModTime == "" {
			t.Errorf("entry[%d] carries no mod_time", i)
		}
		if e.SizeBytes == 0 && !e.IsDir {
			t.Errorf("entry[%d] carries no size", i)
		}
	}
}

func TestFilesBrowseEmptyPathIsRoot(t *testing.T) {
	dir := chdirToWorkdir(t)
	os.WriteFile(filepath.Join(dir, "marker.txt"), []byte("x"), 0o644) //nolint:errcheck // setup
	for _, url := range []string{"/files/browse", "/files/browse?path="} {
		_, body := filesGet(t, url)
		raw, _ := json.Marshal(body["entries"])
		var entries []fileEntry
		json.Unmarshal(raw, &entries)
		found := false
		for _, e := range entries {
			if e.Name == "marker.txt" {
				found = true
			}
		}
		if !found {
			t.Errorf("%s did not list the root", url)
		}
	}
}

func TestFilesBrowseRejections(t *testing.T) {
	dir := chdirToWorkdir(t)
	os.WriteFile(filepath.Join(dir, "plain.txt"), []byte("x"), 0o644) //nolint:errcheck // setup
	cases := map[string]int{
		"/files/browse?path=../outside":  http.StatusBadRequest, // traversal
		"/files/browse?path=a/../b":      http.StatusBadRequest, // traversal segment
		"/files/browse?path=.git":        http.StatusBadRequest, // hidden segment
		"/files/browse?path=docs/.x.txt": http.StatusBadRequest, // hidden nested segment
		"/files/browse?path=/etc/passwd": http.StatusBadRequest, // absolute
		"/files/browse?path=a//b":        http.StatusBadRequest, // empty segment
		"/files/browse?path=missing-dir": http.StatusNotFound,   // absent
		"/files/browse?path=plain.txt":   http.StatusBadRequest, // file, not directory
	}
	for url, want := range cases {
		res, body := filesGet(t, url)
		if res.StatusCode != want {
			t.Errorf("%s status = %d; want %d", url, res.StatusCode, want)
		}
		if msg, ok := body["error"].(string); !ok || msg == "" {
			t.Errorf("%s error envelope missing or empty: %v", url, body)
		}
	}
}

func TestFilesBrowseMethodGate(t *testing.T) {
	chdirToWorkdir(t)
	s := newTestServer()
	req := httptest.NewRequest(http.MethodPost, "/files/browse", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Result().StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST /files/browse status = %d; want 405", rec.Result().StatusCode)
	}
}

func TestFilesRead(t *testing.T) {
	dir := chdirToWorkdir(t)
	content := strings.Repeat("omega\n", 10)
	os.WriteFile(filepath.Join(dir, "small.txt"), []byte(content), 0o644) //nolint:errcheck // setup

	res, body := filesGet(t, "/files/read?path=small.txt")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("read status = %d; want 200", res.StatusCode)
	}
	if body["content"] != content {
		t.Errorf("content mismatch: %q", body["content"])
	}
	if body["truncated"] != false || body["binary"] != false {
		t.Errorf("flags wrong: truncated=%v binary=%v", body["truncated"], body["binary"])
	}
}

func TestFilesReadTruncation(t *testing.T) {
	dir := chdirToWorkdir(t)
	os.WriteFile(filepath.Join(dir, "big.txt"), bytes.Repeat([]byte("a"), fileReadCap+100), 0o644) //nolint:errcheck // setup
	_, body := filesGet(t, "/files/read?path=big.txt")
	if body["truncated"] != true {
		t.Errorf("truncated = %v; want true", body["truncated"])
	}
	if content, _ := body["content"].(string); len(content) != fileReadCap {
		t.Errorf("content length = %d; want %d", len(content), fileReadCap)
	}
}

func TestFilesReadBinary(t *testing.T) {
	dir := chdirToWorkdir(t)
	os.WriteFile(filepath.Join(dir, "blob.bin"), []byte{0x00, 0xff, 0xfe, 0x80, 0x01}, 0o644) //nolint:errcheck // setup
	_, body := filesGet(t, "/files/read?path=blob.bin")
	if body["binary"] != true {
		t.Errorf("binary = %v; want true", body["binary"])
	}
	if body["content"] != "" {
		t.Errorf("binary content = %q; want empty", body["content"])
	}
}

func TestFilesReadRejections(t *testing.T) {
	dir := chdirToWorkdir(t)
	os.Mkdir(filepath.Join(dir, "adir"), 0o755) //nolint:errcheck // setup
	cases := map[string]int{
		"/files/read?path=../outside":  http.StatusBadRequest, // traversal
		"/files/read?path=.secret":     http.StatusBadRequest, // hidden segment
		"/files/read?path=/etc/passwd": http.StatusBadRequest, // absolute
		"/files/read?path=missing.txt": http.StatusNotFound,   // absent
		"/files/read?path=adir":        http.StatusBadRequest, // a directory
	}
	for url, want := range cases {
		res, body := filesGet(t, url)
		if res.StatusCode != want {
			t.Errorf("%s status = %d; want %d", url, res.StatusCode, want)
		}
		if msg, ok := body["error"].(string); !ok || msg == "" {
			t.Errorf("%s error envelope missing or empty: %v", url, body)
		}
	}
}

func TestFilesReadMethodGate(t *testing.T) {
	chdirToWorkdir(t)
	s := newTestServer()
	req := httptest.NewRequest(http.MethodPut, "/files/read", strings.NewReader("nope"))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Result().StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("PUT /files/read status = %d; want 405", rec.Result().StatusCode)
	}
}

// TestFilesSymlinkEscape pins the full-request escape guard: symlinks
// inside the working directory pointing outside (file and directory
// targets) must get a structured 400 on both endpoints, never a disk
// read of the target.
func TestFilesSymlinkEscape(t *testing.T) {
	dir := chdirToWorkdir(t)
	targets := map[string]string{
		"passwd.lnk": "/etc/passwd", // file target outside the root
		"out.lnk":    "/etc",        // directory target outside the root
	}
	for name, target := range targets {
		if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
			t.Fatalf("symlink: %v", err)
		}
	}
	cases := map[string]int{
		"/files/browse?path=passwd.lnk": http.StatusBadRequest,
		"/files/browse?path=out.lnk":    http.StatusBadRequest,
		"/files/read?path=passwd.lnk":   http.StatusBadRequest,
	}
	for url, want := range cases {
		res, body := filesGet(t, url)
		if res.StatusCode != want {
			t.Errorf("%s status = %d; want %d", url, res.StatusCode, want)
		}
		if msg, ok := body["error"].(string); !ok || msg == "" {
			t.Errorf("%s error envelope missing or empty: %v", url, body)
		}
	}
}

// TestFilesRootBehindSymlink pins the containment base: a working
// directory reached through a symlink (PWD points at the symlink) must
// still serve requests - containment is checked against the
// EvalSymlinks-resolved root, not the raw one.
func TestFilesRootBehindSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "realwork")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	marker := []byte("inspector root\n")
	if err := os.WriteFile(filepath.Join(real, "marker.txt"), marker, 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	// Go's Getwd honors $PWD when it resolves to the current directory,
	// so the process working directory reads back as the symlink path -
	// exactly the environment that broke the raw-root containment check
	t.Setenv("PWD", link)
	os.Chdir(link)                      //nolint:errcheck // verified via Getwd below
	t.Cleanup(func() { os.Chdir("/") }) //nolint:errcheck // test cleanup only
	if wd, err := os.Getwd(); err != nil || wd != link {
		t.Fatalf("Getwd = %q, %v; want %q", wd, err, link)
	}
	res, body := filesGet(t, "/files/browse")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("browse status = %d; want 200 (root behind symlink must serve)", res.StatusCode)
	}
	raw, _ := json.Marshal(body["entries"])
	var entries []fileEntry
	json.Unmarshal(raw, &entries)
	if len(entries) != 1 || entries[0].Name != "marker.txt" {
		t.Fatalf("entries = %v; want marker.txt", entries)
	}
	res, body = filesGet(t, "/files/read?path=marker.txt")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("read status = %d; want 200", res.StatusCode)
	}
	if body["content"] != string(marker) {
		t.Errorf("content = %q; want %q", body["content"], marker)
	}
}

// filesPost exercises the write half the way filesGet exercises the
// read half: a real Server.Handler() round-trip against the chdir'd root.
func filesPost(t *testing.T, path string, jsonBody string) (*http.Response, map[string]any) {
	t.Helper()
	s := newTestServer()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	res := rec.Result()
	var body map[string]any
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	return res, body
}

func TestFilesWriteCreateReadBack(t *testing.T) {
	dir := chdirToWorkdir(t)
	res, body := filesPost(t, "/files/write", `{"path":"new.txt","content":"omega\n"}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("write status = %d; want 200 (%v)", res.StatusCode, body)
	}
	if body["ok"] != true || body["path"] != "new.txt" {
		t.Errorf("response = %v; want ok+path", body)
	}
	got, err := os.ReadFile(filepath.Join(dir, "new.txt"))
	if err != nil || string(got) != "omega\n" {
		t.Errorf("disk read back %q, %v; want the written content", got, err)
	}
	// overwrite goes through the same route
	if _, body := filesPost(t, "/files/write", `{"path":"new.txt","content":"v2"}`); body == nil {
		t.Fatal("rewrite failed")
	}
	got, err = os.ReadFile(filepath.Join(dir, "new.txt"))
	if err != nil || string(got) != "v2" {
		t.Errorf("overwrite read back %q, %v; want v2", got, err)
	}
}

func TestFilesWriteNestedUnderExistingDir(t *testing.T) {
	dir := chdirToWorkdir(t)
	os.Mkdir(filepath.Join(dir, "docs"), 0o755) //nolint:errcheck // setup
	res, body := filesPost(t, "/files/write", `{"path":"docs/notes.md","content":""}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("write status = %d; want 200 (%v)", res.StatusCode, body)
	}
	if _, err := os.Stat(filepath.Join(dir, "docs", "notes.md")); err != nil {
		t.Errorf("empty-content file not created: %v", err)
	}
}

func TestFilesWriteRefusals(t *testing.T) {
	dir := chdirToWorkdir(t)
	os.Mkdir(filepath.Join(dir, "adir"), 0o755) //nolint:errcheck // setup
	cases := []struct {
		json string
		want int
		why  string
	}{
		{`{"path":"","content":"x"}`, http.StatusBadRequest, "empty name"},
		{`{"path":"  ","content":"x"}`, http.StatusBadRequest, "blank name"},
		{`{"path":"../outsidespawn","content":"x"}`, http.StatusBadRequest, "traversal"},
		{`{"path":"a/../b.txt","content":"x"}`, http.StatusBadRequest, "traversal segment"},
		{`{"path":"/etc/passwd","content":"x"}`, http.StatusBadRequest, "absolute"},
		{`{"path":".hidden","content":"x"}`, http.StatusBadRequest, "dot segment"},
		{`{"path":"adir","content":"x"}`, http.StatusBadRequest, "target is a directory"},
		{`{"path":"no-parent/f.txt","content":"x"}`, http.StatusBadRequest, "missing parent"},
		{`{"path":"adir/","content":"x"}`, http.StatusBadRequest, "empty segment"},
		{`not json`, http.StatusBadRequest, "malformed body"},
	}
	for _, c := range cases {
		res, body := filesPost(t, "/files/write", c.json)
		if res.StatusCode != c.want {
			t.Errorf("%s (%s) status = %d; want %d", c.json, c.why, res.StatusCode, c.want)
		}
		if msg, ok := body["error"].(string); !ok || msg == "" {
			t.Errorf("%s (%s) error envelope missing or empty: %v", c.json, c.why, body)
		}
	}
	// loud refusal means loud: nothing landed outside the root
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "outsidespawn")); !os.IsNotExist(err) {
		t.Errorf("traversal wrote outside the root")
	}
	if _, err := os.Stat(filepath.Join(dir, "no-parent")); !os.IsNotExist(err) {
		t.Errorf("missing-parent write created the parent as a side effect")
	}
}

func TestFilesWriteSymlinkEscape(t *testing.T) {
	dir := chdirToWorkdir(t)
	outside := filepath.Join(t.TempDir(), "victim.txt")
	os.WriteFile(outside, []byte("original"), 0o644) //nolint:errcheck // setup
	os.Symlink(outside, filepath.Join(dir, "esc.lnk")) //nolint:errcheck // setup
	res, _ := filesPost(t, "/files/write", `{"path":"esc.lnk","content":"hijack"}`)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("symlink escape status = %d; want 400", res.StatusCode)
	}
	got, err := os.ReadFile(outside)
	if err != nil || string(got) != "original" {
		t.Errorf("outside target read %q, %v; want untouched", got, err)
	}
}

func TestFilesWriteMethodGate(t *testing.T) {
	chdirToWorkdir(t)
	s := newTestServer()
	// invoked DIRECTLY: the mux registers only the POST pattern, so a
	// wrong verb in production falls to the SPA catch-all - the handler's
	// own gate is the pinned defense here
	req := httptest.NewRequest(http.MethodGet, "/files/write", nil)
	rec := httptest.NewRecorder()
	s.handleFilesWrite(rec, req)
	if rec.Result().StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET (direct) /files/write status = %d; want 405", rec.Result().StatusCode)
	}
}

func TestFilesMkdirCreates(t *testing.T) {
	dir := chdirToWorkdir(t)
	res, body := filesPost(t, "/files/mkdir", `{"path":"a-dir"}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("mkdir status = %d; want 200 (%v)", res.StatusCode, body)
	}
	info, err := os.Stat(filepath.Join(dir, "a-dir"))
	if err != nil || !info.IsDir() {
		t.Errorf("a-dir not created as a directory: %v", err)
	}
}

func TestFilesMkdirRefusals(t *testing.T) {
	dir := chdirToWorkdir(t)
	os.Mkdir(filepath.Join(dir, "here"), 0o755) //nolint:errcheck // setup
	cases := []struct {
		json string
		want int
		why  string
	}{
		{`{"path":""}`, http.StatusBadRequest, "empty name"},
		{`{"path":".."}`, http.StatusBadRequest, "traversal"},
		{`{"path":"/etc"}`, http.StatusBadRequest, "absolute"},
		{`{"path":"here"}`, http.StatusBadRequest, "already exists"},
	}
	for _, c := range cases {
		res, body := filesPost(t, "/files/mkdir", c.json)
		if res.StatusCode != c.want {
			t.Errorf("%s (%s) status = %d; want %d", c.json, c.why, res.StatusCode, c.want)
		}
		if msg, ok := body["error"].(string); !ok || msg == "" {
			t.Errorf("%s (%s) error envelope missing or empty: %v", c.json, c.why, body)
		}
	}
	// mkdir of a path whose file exists: loud, not idempotent-noise
	os.WriteFile(filepath.Join(dir, "afile.txt"), []byte("x"), 0o644) //nolint:errcheck // setup
	res, _ := filesPost(t, "/files/mkdir", `{"path":"afile.txt"}`)
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("mkdir over a file status = %d; want 400", res.StatusCode)
	}
}

func TestFilesMkdirMethodGate(t *testing.T) {
	chdirToWorkdir(t)
	s := newTestServer()
	// direct invocation - see TestFilesWriteMethodGate
	req := httptest.NewRequest(http.MethodPut, "/files/mkdir", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	s.handleFilesMkdir(rec, req)
	if rec.Result().StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("PUT (direct) /files/mkdir status = %d; want 405", rec.Result().StatusCode)
	}
}
