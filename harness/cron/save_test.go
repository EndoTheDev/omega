package cron

// save_test.go: the cron tab's job editor write face (card t_ecec864e).
// The upsert/delete splice at the yaml.Node level - every test pins the
// surviving style (comments, untouched siblings) the fold-write law
// protects, and the wire lanes (200 carries the landed truth, schema
// rejections 400, unknown delete 404, no runner 501).

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const saveTestCrons = `# the human-authored file
jobs:
  - name: daily-journal   # the owner's note rides the entry
    schedule: "0 8 * * *"
    prompt: journal
    deliver: journal
  - name: pulse
    kind: interval
    minutes: 5
    prompt: tick
`

func newSaveTestRunner(t *testing.T, mainYAML string) (*Runner, string) {
	t.Helper()
	home := t.TempDir()
	path := filepath.Join(home, "crons.yaml")
	if mainYAML != "" {
		if err := os.WriteFile(path, []byte(mainYAML), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := LoadAllConfigs(home)
	if err != nil {
		t.Fatal(err)
	}
	return NewRunner(cfg, home), path
}

func listingByKey(jobs []JobInfo) map[string]JobInfo {
	by := map[string]JobInfo{}
	for _, j := range jobs {
		by[j.Name] = j
	}
	return by
}

func TestSaveJobAppendsNewJobAndReloads(t *testing.T) {
	r, path := newSaveTestRunner(t, saveTestCrons)
	jobs, err := r.SaveJob(Job{
		Name:    "added",
		Kind:    kindInterval,
		Minutes: 30,
		Prompt:  "hello",
		Enabled: boolPtr(),
	})
	if err != nil {
		t.Fatal(err)
	}
	byKey := listingByKey(jobs)
	j, ok := byKey["added"]
	if !ok || !j.Enabled || j.Kind != kindInterval || j.Minutes != 30 {
		t.Fatalf("added job missing or wrong in the returned listing: %+v (all: %+v)", j, byKey)
	}
	if j.Next == "" {
		t.Error("the enabled interval job must report a next fire after the reload")
	}
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"# the human-authored file",
		"# the owner's note rides the entry",
		"minutes: 30",
		"kind: interval",
	} {
		if !strings.Contains(string(text), want) {
			t.Errorf("file lost %q after append:\n%s", want, text)
		}
	}
	// the untouched siblings keep their shape
	if !strings.Contains(string(text), "deliver: journal") {
		t.Errorf("sibling deliver lost:\n%s", text)
	}
}

func TestSaveJobReplacesEntryInPlaceKeepsSiblingStyle(t *testing.T) {
	r, path := newSaveTestRunner(t, saveTestCrons)
	if _, err := r.SaveJob(Job{
		Name:     "daily-journal",
		Schedule: "0 9 * * *",
		Prompt:   "journal v2",
		Deliver:  deliverAppendToFile,
		File:     "journal/daily.md",
		Enabled:  boolPtr(),
	}); err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(text)
	// the FILE-level comment survives the replace; the replaced
	// entry's own inline comment dies with it (the documented cost of
	// swapping a whole entry's mapping) - the sibling entries are
	// untouched and keep their style
	for _, want := range []string{"# the human-authored file", "0 9 * * *", "journal/daily.md", "minutes: 5", "kind: interval"} {
		if !strings.Contains(s, want) {
			t.Errorf("file lost %q after replace:\n%s", want, s)
		}
	}
	if strings.Contains(s, "# the owner's note rides the entry") {
		t.Errorf("a replaced entry kept stale inline style (must re-marshal clean):\n%s", s)
	}
	if strings.Contains(s, "prompt: journal\n") {
		t.Errorf("old prompt survived the replace:\n%s", s)
	}
	// exactly two jobs still
	r2, _ := newSaveTestRunner(t, s)
	if got := len(r2.jobsSnapshot()); got != 2 {
		t.Errorf("job count after replace = %d, want 2:\n%s", got, s)
	}
}

func TestSaveJobRefusesAgentHomeName(t *testing.T) {
	r, _ := newSaveTestRunner(t, saveTestCrons)
	// simulate a wake job the loader owns:
	r.mu.Lock()
	r.jobs = append(r.jobs, Job{Name: "wake", Schedule: "17 * * * *", Prompt: "p", Agent: "scout", AgentOwned: true})
	r.mu.Unlock()
	_, err := r.SaveJob(Job{Name: "wake", Schedule: "17 * * * *", Prompt: "hijack", Enabled: boolPtr()})
	if err == nil {
		t.Fatal("cross-home overwrite accepted")
	}
	if !strings.Contains(err.Error(), "agent home scout") {
		t.Errorf("err = %v, want it to name the owning agent home", err)
	}
	_, derr := r.DeleteJob("wake")
	if derr == nil || !strings.Contains(derr.Error(), "agent home scout") {
		t.Errorf("delete of an agent-home job err = %v", derr)
	}
}

func TestDeleteJobRemovesEntryUnknownIsLoud(t *testing.T) {
	r, path := newSaveTestRunner(t, saveTestCrons)
	jobs, err := r.DeleteJob("pulse")
	if err != nil {
		t.Fatal(err)
	}
	if j := listingByKey(jobs)["pulse"]; j.Kind != "" {
		t.Errorf("deleted job still listed: %+v", j)
	}
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(text), "minutes: 5") || strings.Contains(string(text), "name: pulse") {
		t.Errorf("entry survived delete:\n%s", text)
	}
	if !strings.Contains(string(text), "daily-journal") {
		t.Errorf("sibling lost by delete:\n%s", text)
	}
	// unknown name: the 404 lane, file untouched
	before, _ := os.ReadFile(path)
	_, err = r.DeleteJob("ghost")
	if err == nil {
		t.Fatal("unknown delete accepted")
	}
	var unknown UnknownJobError
	if !errors.As(err, &unknown) || unknown.Key != "ghost" {
		t.Errorf("err = %v, want UnknownJobError(ghost)", err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Error("unknown delete moved the file")
	}
}

func TestSaveJobBootstrapsFreshHome(t *testing.T) {
	home := t.TempDir()
	cfg, err := LoadAllConfigs(home)
	if err != nil {
		t.Fatal(err)
	}
	r := NewRunner(cfg, home)
	path := homeFile(home) // the loader's own resolution: root wins, else config/
	if _, err := os.Stat(path); err == nil {
		t.Fatalf("%s unexpectedly exists", path)
	}
	jobs, err := r.SaveJob(Job{Name: "first", Schedule: "0 8 * * *", Prompt: "p", Enabled: boolPtr()})
	if err != nil {
		t.Fatal(err)
	}
	if j := listingByKey(jobs)["first"]; j.Name == "" {
		t.Fatalf("bootstrap job not in listing: %+v", jobs)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("bootstrap did not write %s: %v", path, err)
	}
}

// --- the wire lanes ---

func TestHandleSaveAndDeleteWire(t *testing.T) {
	r, _ := newSaveTestRunner(t, saveTestCrons)
	p := &Plugin{runner: r}

	serve := func(method, route, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, route, strings.NewReader(body))
		rec := httptest.NewRecorder()
		if route == "/cron/save" {
			p.handleSaveJob(rec, req)
		} else {
			p.handleDeleteJob(rec, req)
		}
		return rec
	}

	res := serve("POST", "/cron/save", `{"name": "wired", "kind": "interval", "minutes": 15, "prompt": "p", "enabled": true}`)
	if res.Code != http.StatusOK {
		t.Fatalf("save status = %d, want 200: %s", res.Code, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), `"name":"wired"`) || !strings.Contains(res.Body.String(), `"minutes":15`) {
		t.Errorf("response does not carry the landed job:\n%s", res.Body.String())
	}

	// schema law applies to the editor too: interval without minutes
	if res2 := serve("POST", "/cron/save", `{"name": "bad", "kind": "interval", "prompt": "p", "enabled": true}`); res2.Code != http.StatusBadRequest {
		t.Errorf("schema-rejected save status = %d, want 400", res2.Code)
	}
	if res3 := serve("POST", "/cron/save", `{}`); res3.Code != http.StatusBadRequest {
		t.Errorf("nameless save status = %d, want 400", res3.Code)
	}
	if res4 := serve("PUT", "/cron/save", `{}`); res4.Code != http.StatusMethodNotAllowed {
		t.Errorf("PUT save status = %d, want 405", res4.Code)
	}

	res5 := serve("POST", "/cron/delete", `{"name": "wired"}`)
	if res5.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want 200: %s", res5.Code, res5.Body.String())
	}
	if strings.Contains(res5.Body.String(), `"name":"wired"`) {
		t.Errorf("deleted job still in the response listing:\n%s", res5.Body.String())
	}
	if res6 := serve("POST", "/cron/delete", `{"name": "ghost"}`); res6.Code != http.StatusNotFound {
		t.Errorf("unknown delete status = %d, want 404", res6.Code)
	}
	if res7 := serve("POST", "/cron/delete", `{}`); res7.Code != http.StatusBadRequest {
		t.Errorf("nameless delete status = %d, want 400", res7.Code)
	}
	if res8 := serve("PUT", "/cron/delete", `{}`); res8.Code != http.StatusMethodNotAllowed {
		t.Errorf("PUT delete status = %d, want 405", res8.Code)
	}
}

func TestSaveDeleteNoRunnerIs501(t *testing.T) {
	p := &Plugin{}
	req := httptest.NewRequest("POST", "/", strings.NewReader(`{"name": "x"}`))
	rec := httptest.NewRecorder()
	p.handleSaveJob(rec, req)
	if rec.Code != http.StatusNotImplemented {
		t.Errorf("save without runner status = %d, want 501", rec.Code)
	}
	rec2 := httptest.NewRecorder()
	p.handleDeleteJob(rec2, httptest.NewRequest("POST", "/", strings.NewReader(`{"name": "x"}`)))
	if rec2.Code != http.StatusNotImplemented {
		t.Errorf("delete without runner status = %d, want 501", rec2.Code)
	}
}
