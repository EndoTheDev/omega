package files

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// rawGet runs a GET request against the file surface's raw route and
// returns the response plus (for the JSON error paths) the decoded body.
func rawGet(t *testing.T, path string) (*http.Response, []byte) {
	t.Helper()
	s := filesRouteMux(t)
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	res := rec.Result()
	return res, rec.Body.Bytes()
}

// pngFixture is a real PNG signature plus a IHDR-ish payload of filler;
// DetectContentType only needs the magic.
var pngFixture = append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, make([]byte, 32)...)

// jpgFixture is a real JPEG SOI + APP0-ish payload.
var jpgFixture = append([]byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0}, make([]byte, 32)...)

// svgFixture is the text an .svg file actually carries.
const svgFixture = `<?xml version="1.0" encoding="UTF-8"?><svg xmlns="http://www.w3.org/2000/svg" width="4" height="4"><rect width="4" height="4" fill="red"/></svg>`

func TestDetectFileMime(t *testing.T) {
	png := append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0, 0, 0, 13}, make([]byte, 16)...)
	jpg := append([]byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F'}, make([]byte, 16)...)
	gif := []byte("GIF89a\x01\x00\x01\x00\x80\x00\x00\x00\x00\x00\xff\xff\xff,\x00\x00\x00\x00")
	riffWebp := []byte("RIFF\x0c\x00\x00\x00WEBPVP8 ")
	text := []byte("package main\n\nfunc main() {}\n")

	cases := []struct {
		name string
		rel  string
		head []byte
		want string
	}{
		{"png magic", "shot.png", png, "image/png"},
		{"jpg magic", "shot.jpg", jpg, "image/jpeg"},
		{"gif magic", "anim.gif", gif, "image/gif"},
		{"webp magic", "pic.webp", riffWebp, "image/webp"},
		{"svg text + svg name", "icon.svg", []byte(svgFixture), "image/svg+xml"},
		{"svg text, non-svg name", "icon.txt", []byte(svgFixture), "text/xml; charset=utf-8"},
		{"bare svg text, non-svg name", "icon.txt", []byte("<svg xmlns=\"\"><rect/></svg>"), "text/plain; charset=utf-8"},
		{"go source", "main.go", text, "text/plain; charset=utf-8"},
		{"binary octet-stream", "omega.bin", []byte{0x7F, 'E', 'L', 'F', 0x02, 0x01, 0x01, 0x00}, "application/octet-stream"},
	}
	for _, c := range cases {
		if got := detectFileMime(c.head, c.rel); got != c.want {
			t.Errorf("detectFileMime(%q) = %q; want %q", c.rel, got, c.want)
		}
	}
}

func TestFilesReadCarriesMime(t *testing.T) {
	chdirToWorkdir(t)
	if err := os.WriteFile("shot.png", pngFixture, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := os.WriteFile("main.go", []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	_, body := filesGet(t, "/files/read?path=shot.png")
	if body["binary"] != true {
		t.Errorf("read(shot.png).binary = %v; want true", body["binary"])
	}
	if mime, _ := body["mime"].(string); mime != "image/png" {
		t.Errorf("read(shot.png).mime = %q; want image/png", mime)
	}
	if size, _ := body["size"].(float64); size != float64(len(pngFixture)) {
		t.Errorf("read(shot.png).size = %v; want %d", body["size"], len(pngFixture))
	}
	_, body = filesGet(t, "/files/read?path=main.go")
	if mime, _ := body["mime"].(string); mime != "text/plain; charset=utf-8" {
		t.Errorf("read(main.go).mime = %q; want text/plain; charset=utf-8", mime)
	}
}

func TestFilesRawServesImage(t *testing.T) {
	chdirToWorkdir(t)
	if err := os.WriteFile("shot.png", pngFixture, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := os.WriteFile("icon.svg", []byte(svgFixture), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := os.WriteFile("shot.jpg", jpgFixture, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	res, raw := rawGet(t, "/files/raw?path=shot.png")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("raw(shot.png) = %d; want 200 (body %s)", res.StatusCode, raw)
	}
	if ct := res.Header.Get("Content-Type"); ct != "image/png" {
		t.Errorf("raw(shot.png) Content-Type = %q; want image/png", ct)
	}
	if ct := res.Header.Get("X-Content-Type-Options"); ct != "nosniff" {
		t.Errorf("raw(shot.png) missing nosniff guard")
	}
	if cl := res.Header.Get("Content-Length"); cl == "" {
		t.Errorf("raw(shot.png) Content-Length missing")
	}
	res.Body.Close()
	if len(raw) != len(pngFixture) {
		t.Errorf("raw(shot.png) served %d bytes; want %d", len(raw), len(pngFixture))
	}

	res, raw = rawGet(t, "/files/raw?path=icon.svg")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("raw(icon.svg) = %d; want 200", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); ct != "image/svg+xml" {
		t.Errorf("raw(icon.svg) Content-Type = %q; want image/svg+xml", ct)
	}
	res.Body.Close()

	res, raw = rawGet(t, "/files/raw?path=shot.jpg")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("raw(shot.jpg) = %d; want 200", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("raw(shot.jpg) Content-Type = %q; want image/jpeg", ct)
	}
	res.Body.Close()
}

func TestFilesRawRefusesNonImage(t *testing.T) {
	chdirToWorkdir(t)
	// a text file sniffs text/plain - not an image, refused
	if err := os.WriteFile("notes.txt", []byte("plain text"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	// an ELF binary sniffs application/octet-stream - refused too
	bin := append([]byte{0x7F, 'E', 'L', 'F', 0x02, 0x01, 0x01, 0x00}, make([]byte, 64)...)
	if err := os.WriteFile("omega.bin", bin, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	for _, rel := range []string{"notes.txt", "omega.bin"} {
		res, _ := rawGet(t, "/files/raw?path="+rel)
		if res.StatusCode != http.StatusBadRequest {
			t.Errorf("raw(%q) = %d; want 400 (renderable only)", rel, res.StatusCode)
		}
		res.Body.Close()
	}
}

func TestFilesRawServesPdf(t *testing.T) {
	chdirToWorkdir(t)
	// a minimal pdf: DetectContentType sniffs the %PDF- magic to
	// application/pdf, the browser's native viewer needs nothing more
	pdf := append([]byte("%PDF-1.7\n1 0 obj<</Type/Catalog>>endobj\ntrailer<</Root 1 0 R>>"), make([]byte, 32)...)
	if err := os.WriteFile("doc.pdf", pdf, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	// a text file with a .pdf name sniffs text/plain - no magic, no pdf
	if err := os.WriteFile("fake.pdf", []byte("plain text"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	res, raw := rawGet(t, "/files/raw?path=doc.pdf")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("raw(doc.pdf) = %d; want 200 (body %s)", res.StatusCode, raw)
	}
	if ct := res.Header.Get("Content-Type"); ct != "application/pdf" {
		t.Errorf("raw(doc.pdf) Content-Type = %q; want application/pdf", ct)
	}
	if ct := res.Header.Get("X-Content-Type-Options"); ct != "nosniff" {
		t.Errorf("raw(doc.pdf) missing nosniff guard")
	}
	res.Body.Close()
	if len(raw) != len(pdf) {
		t.Errorf("raw(doc.pdf) served %d bytes; want %d", len(raw), len(pdf))
	}
	// the sniff is the gate, not the extension: a text-named-pdf refuses
	res, _ = rawGet(t, "/files/raw?path=fake.pdf")
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("raw(fake.pdf) = %d; want 400 (sniff, not extension, decides)", res.StatusCode)
	}
	res.Body.Close()
}

func TestFilesRawDownloadLane(t *testing.T) {
	chdirToWorkdir(t)
	bin := append([]byte{0x7F, 'E', 'L', 'F', 0x02, 0x01, 0x01, 0x00}, make([]byte, 64)...)
	if err := os.WriteFile("omega.bin", bin, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := os.WriteFile("notes.txt", []byte("plain text"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	// download=1 is the second lane: ANY mime serves, but always as an
	// attachment - never rendered as a document
	for _, rel := range []string{"omega.bin", "notes.txt"} {
		res, raw := rawGet(t, "/files/raw?path="+rel+"&download=1")
		if res.StatusCode != http.StatusOK {
			t.Fatalf("raw(%q, download) = %d; want 200", rel, res.StatusCode)
		}
		if cd := res.Header.Get("Content-Disposition"); cd != `attachment; filename="`+rel+`"` {
			t.Errorf("raw(%q, download) Content-Disposition = %q; want attachment", rel, cd)
		}
		if ct := res.Header.Get("X-Content-Type-Options"); ct != "nosniff" {
			t.Errorf("raw(%q, download) missing nosniff guard", rel)
		}
		res.Body.Close()
		if len(raw) == 0 {
			t.Errorf("raw(%q, download) served no bytes", rel)
		}
	}
	// the render lane still refuses the unrenderable pair
	for _, rel := range []string{"omega.bin", "notes.txt"} {
		res, _ := rawGet(t, "/files/raw?path="+rel)
		if res.StatusCode != http.StatusBadRequest {
			t.Errorf("raw(%q) = %d; want 400 (mime gate survives beside the download lane)", rel, res.StatusCode)
		}
		res.Body.Close()
	}
}

func TestFilesRawRidesTheReadJail(t *testing.T) {
	dir := chdirToWorkdir(t)
	if err := os.WriteFile("shot.png", pngFixture, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	// a symlink pointing OUTSIDE the root must be refused, exactly like
	// /files/read: resolved-target containment, not name tricks
	if err := os.Symlink(dir, "inside"); err != nil { // a self symlink is legal and stays contained
		t.Fatalf("symlink inside: %v", err)
	}
	outside := filepath.Join(os.TempDir(), "raw-escape-"+t.Name())
	if err := os.WriteFile(outside+".png", pngFixture, 0o644); err != nil {
		t.Skipf("cannot write outside fixture: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(outside + ".png") })
	if err := os.Symlink(outside+".png", "escape.png"); err != nil {
		t.Fatalf("symlink escape: %v", err)
	}

	// the traversal + absolute cases are 400s with the error envelope
	for _, rel := range []string{"a/../x.png", "a/./x.png", ".", "..", "/etc/passwd"} {
		res, body := rawGet(t, "/files/raw?path="+rel)
		res.Body.Close()
		if rel == ".." {
			// "path=..&..." may url-escape; run it escaped too
			res, body = rawGet(t, "/files/raw?path=a%2F..%2Fx")
		}
		if res.StatusCode != http.StatusBadRequest {
			t.Errorf("raw(%q) = %d; want 400", rel, res.StatusCode)
		}
		var env map[string]string
		if len(body) > 0 {
			_ = json.Unmarshal(body, &env)
			if env["error"] == "" {
				t.Errorf("raw(%q) carries no error envelope", rel)
			}
		}
	}
	// the symlink ESCAPE must refuse with the containment verdict
	res, _ := rawGet(t, "/files/raw?path=escape.png")
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("raw(escape.png) = %d; want 400 (symlink out of root)", res.StatusCode)
	}
	res.Body.Close()
	// a legal in-root symlink to an image serves (jail passes it)
	res, _ = rawGet(t, "/files/raw?path=inside/shot.png")
	if res.StatusCode != http.StatusOK {
		t.Errorf("raw(inside/shot.png) = %d; want 200 (in-root symlink ok)", res.StatusCode)
	}
	res.Body.Close()
	// the root itself is a directory: 400 "not a file", not a serve
	res, _ = rawGet(t, "/files/raw?path=")
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("raw(root) = %d; want 400", res.StatusCode)
	}
	res.Body.Close()
	// a missing leaf is a 404, the same shape /files/read reports
	res, _ = rawGet(t, "/files/raw?path=missing.png")
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("raw(missing) = %d; want 404", res.StatusCode)
	}
	res.Body.Close()
}

func TestFilesRawMethodAndSessionRooting(t *testing.T) {
	chdirToWorkdir(t)
	if err := os.WriteFile("shot.png", pngFixture, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	// the raw route is GET-only, like its read sibling
	s := filesRouteMux(t)
	req := httptest.NewRequest(http.MethodPost, "/files/raw?path=shot.png", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /files/raw = %d; want 405", rec.Code)
	}
	// an unknown session id falls through the root chain - the cwd
	// fallback roots the fetch (resolution law), the image still serves
	res, _ := rawGet(t, "/files/raw?path=shot.png&session=i-do-not-exist")
	if res.StatusCode != http.StatusOK {
		t.Errorf("raw with unknown session = %d; want 200", res.StatusCode)
	}
	res.Body.Close()
}
