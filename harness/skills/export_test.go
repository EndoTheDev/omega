package skills

// The two row verbs that leave the editable surface: Export (a tar.gz download) and
// Archive (the folder moved out of the listing, backed up and ledgered).

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillsExportRoundtrip(t *testing.T) {
	rt, dir := editorRoutes(t)
	// a references file rides the folder export, not just SKILL.md
	refs := filepath.Join(dir, "existing", "references")
	if err := os.MkdirAll(refs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(refs, "notes.md"), []byte("ref body"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/skills/export?name=existing", nil)
	rt.handleSkillsExport(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("export = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/gzip" {
		t.Errorf("content type = %q, want application/gzip", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "existing.tar.gz") {
		t.Errorf("content disposition = %q, want the existing.tar.gz attachment", cd)
	}
	zr, err := gzip.NewReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	tr := tar.NewReader(zr)
	seen := map[string]string{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar next: %v", err)
		}
		if hdr.Typeflag == tar.TypeReg {
			b, _ := io.ReadAll(tr)
			seen[hdr.Name] = string(b)
		}
	}
	skill, ok := seen["existing/SKILL.md"]
	if !ok {
		t.Fatalf("archive carries no existing/SKILL.md: files %v", keysOf(seen))
	}
	if !strings.Contains(skill, "body of existing") {
		t.Errorf("SKILL.md bytes = %q", skill)
	}
	if seen["existing/references/notes.md"] != "ref body" {
		t.Errorf("references/notes.md = %q, want ref body", seen["existing/references/notes.md"])
	}
}

func TestSkillsExportRefusals(t *testing.T) {
	rt, _ := editorRoutes(t)
	for _, c := range []struct {
		target string
		want   int
		why    string
	}{
		{"/skills/export", http.StatusBadRequest, "missing name"},
		{"/skills/export?name=../evil", http.StatusBadRequest, "traversal name"},
		{"/skills/export?name=ghost", http.StatusNotFound, "unknown skill"},
		{"/skills/export?name=existing", http.StatusMethodNotAllowed, "POST refused"},
	} {
		method := http.MethodGet
		if c.want == http.StatusMethodNotAllowed {
			method = http.MethodPost
		}
		rec := httptest.NewRecorder()
		rt.handleSkillsExport(rec, httptest.NewRequest(method, c.target, nil))
		if rec.Code != c.want {
			t.Errorf("%s %s (%s) = %d, want %d", method, c.target, c.why, rec.Code, c.want)
		}
	}
}

func TestSkillsArchiveRoundtrip(t *testing.T) {
	rt, dir := editorRoutes(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/skills/archive", strings.NewReader(`{"name":"existing"}`))
	rt.handleSkillsArchive(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("archive = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	var rows []skillListingRow
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("listing decode: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("listing after archive = %+v, want empty", rows)
	}
	if _, err := os.Stat(filepath.Join(dir, "existing")); !os.IsNotExist(err) {
		t.Errorf("archived skill still in the listing dir: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, archiveDirName))
	if err != nil || len(entries) != 1 {
		t.Fatalf("archive dir = %v, %v; want one archived folder", entries, err)
	}
	archived := filepath.Join(dir, archiveDirName, entries[0].Name(), "SKILL.md")
	if _, err := os.Stat(archived); err != nil {
		t.Fatalf("archived SKILL.md missing at %s: %v", archived, err)
	}
	// the sidecar backup and the ledger row are both written before the move
	backups, err := os.ReadDir(filepath.Join(dir, curatorDirName, backupsDirName))
	if err != nil || len(backups) == 0 {
		t.Errorf("no sidecar backup after archive: %v, %v", backups, err)
	}
	ledger, err := os.ReadFile(filepath.Join(dir, curatorDirName, ledgerFileName))
	if err != nil || !strings.Contains(string(ledger), `"action":"archive"`) {
		t.Errorf("ledger carries no archive row: %q, %v", ledger, err)
	}
}

func TestSkillsArchiveRefusals(t *testing.T) {
	rt, _ := editorRoutes(t)
	for _, c := range []struct {
		body   string
		method string
		want   int
		why    string
	}{
		{`{"name":"ghost"}`, http.MethodPost, http.StatusNotFound, "unknown skill"},
		{`{"name":"../evil"}`, http.MethodPost, http.StatusBadRequest, "traversal name"},
		{`{}`, http.MethodPost, http.StatusBadRequest, "missing name"},
		{`{"name":"existing"}`, http.MethodGet, http.StatusMethodNotAllowed, "GET refused"},
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(c.method, "/skills/archive", strings.NewReader(c.body))
		rt.handleSkillsArchive(rec, req)
		if rec.Code != c.want {
			t.Errorf("%s %s (%s) = %d, want %d", c.method, c.body, c.why, rec.Code, c.want)
		}
	}
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
