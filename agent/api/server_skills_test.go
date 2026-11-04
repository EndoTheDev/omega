package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// The skills content-view surface (card t_36ceb99b): GET /skills/content
// rides the std-typed SkillRead face the same way /skills rides
// SkillsEntries (part 3: the consumer asserts its own interface). The
// tests pin the wire contract: one skill's name+description+body with the
// truncation flag, loud refusals for illegal names, a 404 for an unknown
// name, a 501 when the host carries no reader, and a read-only posture
// (405 on POST).

// fakeSkillReader serves the serving surface's own need face: the
// inventory view plus one read by name. The capsule's load law (merge
// winner, dir) is the capsule's business - the fake carries only what
// the route reads.
type fakeSkillReader struct {
	index map[string]string
	desc  map[string]string
	body  map[string]string
}

func (f *fakeSkillReader) SkillsEntries() (map[string]string, map[string]string, error) {
	return f.index, f.body, nil
}

func (f *fakeSkillReader) SkillRead(name string) (description, content string, err error) {
	d, ok := f.desc[name]
	if !ok {
		return "", "", &errNotFound{}
	}
	return d, f.body[name], nil
}

// skillsEntriesOnly offers "skills" WITHOUT the read face - the handler
// must answer 501, never panic.
type skillsEntriesOnly struct{}

func (*skillsEntriesOnly) SkillsEntries() (map[string]string, map[string]string, error) {
	return map[string]string{}, map[string]string{}, nil
}

// errNotFound is the capsule's plain not-found phrasing (the real
// provider's resolveSkillFile error text names the skill and its
// availability - the handler routes it by the "not found" phrase)
type errNotFound struct{}

func (*errNotFound) Error() string { return `skill %q not found. Available: none` }

func TestSkillsContentHappyPath(t *testing.T) {
	s := inspectorServer(t)
	offerServices(t, s, map[string]any{"skills": &fakeSkillReader{
		index: map[string]string{"worker-onboarding": "Use when running an omega card beside other workers."},
		desc:  map[string]string{"worker-onboarding": "Use when running an omega card beside other workers."},
		body:  map[string]string{"worker-onboarding": "# Worker Onboarding\n\nOwn your worktree.\n"},
	}})
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
	body := strings.Repeat("a", skillReadCap) + "tail-marker"
	s := inspectorServer(t)
	offerServices(t, s, map[string]any{"skills": &fakeSkillReader{
		desc: map[string]string{"big": ""},
		body: map[string]string{"big": body},
	}})
	ts := serveInspector(t, s)
	res, err := http.Get(ts.URL + "/skills/content?name=big")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var got skillsContentRow
	json.NewDecoder(res.Body).Decode(&got)
	if !got.Truncated || len(got.Content) != skillReadCap {
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
		offerServices(t, s, map[string]any{"skills": &fakeSkillReader{
			desc: map[string]string{}, body: map[string]string{},
		}})
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
	offerServices(t, s, map[string]any{"skills": &fakeSkillReader{
		desc: map[string]string{}, body: map[string]string{},
	}})
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
	offerServices(t, s, map[string]any{"skills": &skillsEntriesOnly{}})
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