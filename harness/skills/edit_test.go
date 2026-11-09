package skills

// edit_test.go: the webui editor's write-face self-check (card
// t_0a108f16). The provider laws the three operations carry: the
// loader's parse of what was written is the shape proof; the body
// owns the markdown while the operator's frontmatter keys survive the
// update; a removal is backed up + receipted + loud about unknown
// names.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveSkillCreateComposesFrontmatter(t *testing.T) {
	dir := t.TempDir()
	sp := NewSkillsProvider(dir)
	rows, err := sp.SaveSkill("probe", "A probe skill for the tab.", "# probe\n\nsay nothing.\n")
	if err != nil {
		t.Fatalf("SaveSkill: %v", err)
	}
	if len(rows) != 1 || rows[0].Name != "probe" {
		t.Fatalf("listing = %v, want one probe row", rows)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "probe", "SKILL.md"))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	content := string(raw)
	if !strings.HasPrefix(content, "---\ndescription: A probe skill for the tab.\n---\n") {
		t.Errorf("composed frontmatter missing: %q", content)
	}
	if strings.Contains(content, "author") {
		// the editor is the human's voice - no agent authorship stamp
		t.Errorf("the tab editor never composes an author line: %q", content)
	}
	if !strings.HasSuffix(content, "say nothing.\n") {
		t.Errorf("body not seated under the fence: %q", content)
	}
}

func TestSaveSkillUpdateKeepsFrontmatterKeys(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "deploy")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	before := "---\ndescription: old one-liner\nauthor: agent\nversion: 4.0.0\n---\n# deploy\n\nold body\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	sp := NewSkillsProvider(dir)
	if _, err := sp.SaveSkill("deploy", "new one-liner", "# deploy\n\nnew body\n"); err != nil {
		t.Fatalf("SaveSkill update: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(raw)
	if strings.Contains(content, "old one-liner") || !strings.Contains(content, "new one-liner") {
		t.Errorf("description not rewritten: %q", content)
	}
	for _, keep := range []string{"author: agent", "version: 4.0.0", "new body\n"} {
		if !strings.Contains(content, keep) {
			t.Errorf("lost operator content %q in: %q", keep, content)
		}
	}
	if !strings.HasPrefix(content, "---\n") {
		t.Errorf("frontmatter fence lost: %q", content)
	}
}

func TestSaveSkillFrontmatterlessFileGainsOne(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "scout")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# bare\n\nno frontmatter at all\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sp := NewSkillsProvider(dir)
	if _, err := sp.SaveSkill("scout", "a bare file gains its fence", "# bare\n\nnew body\n"); err != nil {
		t.Fatalf("SaveSkill: %v", err)
	}
	s, err := sp.ReadSkill("scout")
	if err != nil {
		t.Fatalf("ReadSkill: %v", err)
	}
	if s.Description != "a bare file gains its fence" || strings.Contains(s.Content, "no frontmatter at all") {
		t.Errorf("read back = %q / %q", s.Description, s.Content)
	}
}

func TestSaveSkillLawsRefuse(t *testing.T) {
	dir := t.TempDir()
	sp := NewSkillsProvider(dir)
	for _, tc := range []struct {
		name, description, body string
		want                    string
	}{
		{"../evil", "x", "body", "single folder component"},
		{".hidden", "x", "body", "single folder component"},
		// (the "spaced " name case is NOT a refusal: SaveSkill trims
		// the name first, exactly the way the write action does)
		{"ok", "", "body", "description"},
		{"ok", "x\nmultiline", "body", "one line"},
		{"ok", "x", "   ", "body is required"},
		{"ok", "x", "---\nsteals the fence", "---"},
	} {
		if _, err := sp.SaveSkill(tc.name, tc.description, tc.body); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("SaveSkill(%q) error = %v, want containing %q", tc.name, err, tc.want)
		}
	}
}

func TestSaveSkillRefusesExistingFolder(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "taken"), 0o755); err != nil {
		t.Fatal(err)
	}
	sp := NewSkillsProvider(dir)
	if _, err := sp.SaveSkill("taken", "x", "body"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("SaveSkill collision error = %v", err)
	}
}

func TestSaveSkillSharedWinnerIsThePatchedFile(t *testing.T) {
	home := t.TempDir()
	shared := filepath.Join(home, "shared")
	own := filepath.Join(home, "own")
	writeSkill(t, shared, "deploy", "shared copy")
	writeSkill(t, own, "deploy", "own copy")
	sp := &SkillsProvider{Dir: own, SharedDir: shared}
	if _, err := sp.SaveSkill("deploy", "edited description", "# deploy\n\nedited\n"); err != nil {
		t.Fatalf("SaveSkill: %v", err)
	}
	s, err := sp.ReadSkill("deploy")
	if err != nil {
		t.Fatal(err)
	}
	if s.Description != "edited description" {
		t.Errorf("shared winner not read back: %q", s.Description)
	}
	sharedRaw, rawErr := os.ReadFile(filepath.Join(shared, "deploy", "SKILL.md"))
	if rawErr != nil {
		t.Fatal(rawErr)
	}
	if !strings.Contains(string(sharedRaw), "edited") {
		t.Errorf("the shared copy must be the winner the write lands in: %q", sharedRaw)
	}
	ownRaw, rawErr := os.ReadFile(filepath.Join(own, "deploy", "SKILL.md"))
	if rawErr != nil {
		t.Fatal(rawErr)
	}
	if !strings.Contains(string(ownRaw), "own copy") {
		t.Errorf("the loser copy must stay byte-true: %q", ownRaw)
	}
}

func TestDeleteSkillBacksUpReceiptsAndRemoves(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "gone", "references"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gone", "SKILL.md"), []byte("---\ndescription: doomed\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gone", "references", "note.md"), []byte("kept by the backup\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sp := NewSkillsProvider(dir)
	rows, err := sp.DeleteSkill("gone")
	if err != nil {
		t.Fatalf("DeleteSkill: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("listing after delete = %v, want empty", rows)
	}
	if _, err := os.Stat(filepath.Join(dir, "gone")); !os.IsNotExist(err) {
		t.Fatalf("the folder must be gone: %v", err)
	}
	// the backup keeps the whole tree
	backupRoot := filepath.Join(dir, ".curator", "backups")
	backupNames, _ := os.ReadDir(backupRoot)
	if len(backupNames) != 1 {
		t.Fatalf("backups = %v", backupNames)
	}
	backupData, err := os.ReadFile(filepath.Join(backupRoot, backupNames[0].Name(), "references", "note.md"))
	if err != nil || !strings.Contains(string(backupData), "kept by the backup") {
		t.Fatalf("multi-file skill backup missing the tree: %v / %q", err, backupData)
	}
	// the ledger carries the receipt (actor webui, action remove)
	ledgerRaw, err := os.ReadFile(filepath.Join(dir, ".curator", "ledger.jsonl"))
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	var entry struct {
		Actor  string `json:"actor"`
		Action string `json:"action"`
		Skill  string `json:"skill"`
	}
	_ = json.Unmarshal([]byte(strings.SplitN(string(ledgerRaw), "\n", 2)[0]), &entry)
	if entry.Actor != ActorWebui || entry.Action != LedgerRemove || entry.Skill != "gone" {
		t.Errorf("ledger entry = %+v", entry)
	}
}

func TestDeleteSkillUnknownIsTheNotFoundLaw(t *testing.T) {
	sp := NewSkillsProvider(t.TempDir())
	_, err := sp.DeleteSkill("ghost")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("DeleteSkill unknown error = %v", err)
	}
}

// --- the wire handlers ---------------------------------------------------

func editorRoutes(t *testing.T) (*Routes, string) {
	t.Helper()
	dir := t.TempDir()
	writeSkill(t, dir, "existing", "the pre-existing row")
	return newRoutes(NewSkillsProvider(dir)), dir
}

func TestSkillsSaveRouteLaws(t *testing.T) {
	rt, _ := editorRoutes(t)
	// POST-only
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/skills/save", nil)
	rt.handleSkillsSave(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /skills/save = %d, want 405", rec.Code)
	}
	// unknown route validation errors are 400s naming the refusal
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/skills/save", strings.NewReader(`{"name":"../evil","description":"x","content":"body"}`))
	rt.handleSkillsSave(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bad name = %d (%s), want 400", rec.Code, rec.Body.String())
	}
	// the happy save answers the fresh listing in GET /skills row shape
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/skills/save",
		strings.NewReader(`{"name":"fresh","description":"fresh row","content":"# fresh\n\nbody\n"}`))
	rt.handleSkillsSave(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("save = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	var rows []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("listing decode: %v", err)
	}
	if len(rows) != 2 || rows[1]["name"] != "fresh" {
		t.Errorf("listing = %s", rec.Body.String())
	}
}

func TestSkillsDeleteRouteLaws(t *testing.T) {
	rt, _ := editorRoutes(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/skills/delete", strings.NewReader(`{"name":"ghost"}`))
	rt.handleSkillsDelete(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown delete = %d (%s), want 404", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/skills/delete", strings.NewReader(`{"name":"existing"}`))
	rt.handleSkillsDelete(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	var rows []skillListingRow
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Errorf("listing = %s, want empty", rec.Body.String())
	}
}
