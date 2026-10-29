package http_channel

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// The skills content-view surface (card t_36ceb99b): GET /skills/content
// rides the SkillsReader seam like /skills rides SkillsLister. The tests
// pin the wire contract: one skill's name+description+body with the
// truncation flag, loud refusals for illegal names, a 404 for an unknown
// name, a 501 when the host carries no reader, and a read-only posture
// (405 on POST).

// fakeSkillReader satisfies core.SkillsProvider + core.SkillsReader.
type fakeSkillReader struct {
	skills map[string]core.Skill
	err    error
}

func (*fakeSkillReader) LoadSkills(string) ([]core.Skill, error) { return nil, nil }

func (f *fakeSkillReader) ReadSkill(name string) (core.Skill, error) {
	if f.err != nil {
		return core.Skill{}, f.err
	}
	sk, ok := f.skills[name]
	if !ok {
		return core.Skill{}, &errNotFound{}
	}
	return sk, nil
}

// skillsProviderNoReader satisfies core.SkillsProvider ONLY - the handler
// must answer 501, never panic.
type skillsProviderNoReader struct{}

func (*skillsProviderNoReader) LoadSkills(string) ([]core.Skill, error) {
	return nil, nil
}

// errNotFound is the seam's plain not-found phrasing (the real provider's
// resolveSkillFile error text names the skill and its availability - the
// handler routes it by the "not found" phrase)
type errNotFound struct{}

func (*errNotFound) Error() string { return "skill %q not found. Available: none" }

func TestSkillsContentHappyPath(t *testing.T) {
	s := inspectorServer(t)
	s.seams.skills = &fakeSkillReader{skills: map[string]core.Skill{
		"worker-onboarding": {
			Name:        "worker-onboarding",
			Description: "Use when running an omega card beside other workers.",
			Content:     "# Worker Onboarding\n\nOwn your worktree.\n",
		},
	}}
	ts := serveInspector(t, s)
	res, err := http.Get(ts.URL + "/skills/content?name=worker-onboarding")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("skills/content = %d, want 200", res.StatusCode)
	}
	var got skillsContentRow
	json.NewDecoder(res.Body).Decode(&got)
	if got.Name != "worker-onboarding" ||
		got.Description != "Use when running an omega card beside other workers." ||
		got.Content != "# Worker Onboarding\n\nOwn your worktree.\n" ||
		got.Truncated {
		t.Fatalf("skills/content row = %+v", got)
	}
}

func TestSkillsContentCapMarksTruncated(t *testing.T) {
	body := strings.Repeat("a", fileReadCap) + "tail-marker"
	s := inspectorServer(t)
	s.seams.skills = &fakeSkillReader{skills: map[string]core.Skill{
		"big": {Name: "big", Content: body},
	}}
	ts := serveInspector(t, s)
	res, err := http.Get(ts.URL + "/skills/content?name=big")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var got skillsContentRow
	json.NewDecoder(res.Body).Decode(&got)
	if !got.Truncated || len(got.Content) != fileReadCap {
		t.Fatalf("content len = %d truncated = %v, want cap+flag", len(got.Content), got.Truncated)
	}
	if strings.Contains(mustJSON(t, got), "tail-marker") {
		t.Fatal("tail leaked past the cap")
	}
}

func TestSkillsContentRefusesIllegalNames(t *testing.T) {
	cases := []string{"", "a/b", "../escape", "..", ".", ".hidden", " spaced ", `back\slash`}
	for _, name := range cases {
		s := inspectorServer(t)
		s.seams.skills = &fakeSkillReader{skills: map[string]core.Skill{}}
		ts := serveInspector(t, s)
		res, err := http.Get(ts.URL + "/skills/content?name=" + strings.ReplaceAll(name, " ", "%20"))
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("skills/content?name=%q = %d, want 400", name, res.StatusCode)
		}
	}
}

func TestSkillsContentUnknownIs404(t *testing.T) {
	s := inspectorServer(t)
	s.seams.skills = &fakeSkillReader{skills: map[string]core.Skill{}}
	ts := serveInspector(t, s)
	res, err := http.Get(ts.URL + "/skills/content?name=nosuch")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown skill = %d, want 404", res.StatusCode)
	}
}

func TestSkillsContentNoReaderIs501(t *testing.T) {
	s := inspectorServer(t)
	s.seams.skills = &skillsProviderNoReader{}
	ts := serveInspector(t, s)
	res, err := http.Get(ts.URL + "/skills/content?name=any")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNotImplemented {
		t.Fatalf("no reader = %d, want 501", res.StatusCode)
	}
}

func TestSkillsContentRejectsNonGet(t *testing.T) {
	ts := serveInspector(t, inspectorServer(t))
	res, err := http.Post(ts.URL+"/skills/content?name=any", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST /skills/content = %d, want 405", res.StatusCode)
	}
}
