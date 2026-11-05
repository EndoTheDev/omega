package skills

// The skills content-view face's self-check (moved from
// agent/api/server_skills_test.go by the shell-ring shrink wave 3,
// card t_b75df168 - re-aimed at the capsule's Routes over a REAL
// SkillsProvider in a throwaway dir). The wire contract: one skill's
// name+description+body with the truncation flag, the loud refusals
// (illegal names - traversal, hidden, untrimmed), a 404 for an
// unknown name, and the read-only posture (405 on POST). The old
// 501-without-reader probe died with the surface's home: the route is
// a module route now, a disabled capsule answers 404 at the module mux.

import (
	"net/http"
	httptest2 "net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// skillsRoutes builds the capsule Routes over a real provider scanning
// a throwaway skills dir seeded with one skill.
func skillsRoutes(t *testing.T) (*Routes, string) {
	t.Helper()
	dir := t.TempDir()
	body := "# Worker Onboarding\n\nOwn your worktree.\n"
	if err := os.MkdirAll(filepath.Join(dir, "worker-onboarding"), 0o755); err != nil {
		t.Fatal(err)
	}
	skill := "---\ndescription: Use when running an omega card beside other workers.\n---\n" + body
	if err := os.WriteFile(filepath.Join(dir, "worker-onboarding", "SKILL.md"), []byte(skill), 0o644); err != nil {
		t.Fatal(err)
	}
	sp := NewSkillsProvider(dir)
	return newRoutes(sp), dir
}

func skillsGet(t *testing.T, rt *Routes, path string) (int, string) {
	t.Helper()
	rec := httptest2.NewRecorder()
	req := httptest2.NewRequest(http.MethodGet, path, nil)
	rt.handleSkillsContent(rec, req)
	return rec.Code, rec.Body.String()
}

func TestSkillsContentHappyPath(t *testing.T) {
	rt, _ := skillsRoutes(t)
	code, body := skillsGet(t, rt, "/skills/content?name=worker-onboarding")
	if code != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", code, body)
	}
	for _, want := range []string{`"name":"worker-onboarding"`, "Use when running an omega card beside other workers.", "Own your worktree."} {
		if !strings.Contains(body, want) {
			t.Errorf("body = %s, want containing %q", body, want)
		}
	}
	if strings.Contains(body, `"truncated":true`) {
		t.Errorf("a small SKILL.md must not carry the truncation flag: %s", body)
	}
}

func TestSkillsContentCapMarksTruncated(t *testing.T) {
	rt, dir := skillsRoutes(t)
	// grow the body past the 64 KiB cap - many short lines, the loader
	// scans line-wise (one over-long line is a loader error, not a cap)
	p := filepath.Join(dir, "worker-onboarding", "SKILL.md")
	fat := "---\ndescription: fat\n---\n" + strings.Repeat("x line\n", 12000)
	if err := os.WriteFile(p, []byte(fat), 0o644); err != nil {
		t.Fatal(err)
	}
	code, body := skillsGet(t, rt, "/skills/content?name=worker-onboarding")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if !strings.Contains(body, `"truncated":true`) {
		t.Errorf("the over-cap body must carry the truncation flag: %s", body[:80]+"...")
	}
}

func TestSkillsContentRefusesIllegalNames(t *testing.T) {
	rt, _ := skillsRoutes(t)
	for _, bad := range []string{"a/b", "../escape", "..", ".", ".hidden", " spaced ", `back\slash`} {
		code, _ := skillsGet(t, rt, "/skills/content?name="+strings.ReplaceAll(bad, " ", "%20"))
		if code != http.StatusBadRequest {
			t.Errorf("illegal name %q: status = %d, want 400", bad, code)
		}
	}
}

func TestSkillsContentUnknownIs404(t *testing.T) {
	rt, _ := skillsRoutes(t)
	code, _ := skillsGet(t, rt, "/skills/content?name=not-installed")
	if code != http.StatusNotFound {
		t.Errorf("unknown skill: status = %d, want 404", code)
	}
}

func TestSkillsContentRejectsNonGet(t *testing.T) {
	rt, _ := skillsRoutes(t)
	rec := httptest2.NewRecorder()
	req := httptest2.NewRequest(http.MethodPost, "/skills/content", strings.NewReader("{}"))
	rt.handleSkillsContent(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST: status = %d, want 405", rec.Code)
	}
}
