package cron

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
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
	r2, _ := newSaveTestRunner(t, s)
	if got := len(r2.jobsSnapshot()); got != 2 {
		t.Errorf("job count after replace = %d, want 2:\n%s", got, s)
	}
}

func TestSaveJobRefusesAgentHomeName(t *testing.T) {
	r, _ := newSaveTestRunner(t, saveTestCrons)
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

// A wake that inherits the main agent is a job that would persist and never fire: the
// main agent is the daemon and never wakes as a subprocess (runner.go's "main never
// wakes").
func TestSaveJobRefusesWakeLeaningOnTheMainAgent(t *testing.T) {
	r, path := newSaveTestRunner(t, saveTestCrons)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.SaveJob(Job{
		Name:     "wake",
		Schedule: "17 * * * *",
		Prompt:   "patrol",
		Deliver:  deliverContinuous,
		Enabled:  boolPtr(),
	})
	if err == nil {
		t.Fatal("a wake inheriting the main agent was accepted")
	}
	for _, want := range []string{"needs its own agent", "inherits the main agent", "never wakes"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want it to name %q", err, want)
		}
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("a refused chain moved the file:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	// the SAME job with its own agent lands (the chain is what was refused, not the wake)
	if _, err := r.SaveJob(Job{
		Name:     "wake",
		Schedule: "17 * * * *",
		Prompt:   "patrol",
		Deliver:  deliverContinuous,
		Agent:    "scout",
		Enabled:  boolPtr(),
	}); err != nil {
		t.Fatalf("a wake with its own agent was refused: %v", err)
	}
}

// An AUTHORED reference to the main agent is the chain's absence written out ("the
// default IS the absence"): the combobox lists the main row, and picking it must not
// pin a phantom override that means exactly what an absent field means.
func TestSaveJobWritesAnInheritedAgentAsAbsence(t *testing.T) {
	r, path := newSaveTestRunner(t, saveTestCrons)
	jobs, err := r.SaveJob(Job{
		Name:     "pulse",
		Schedule: "0 * * * *",
		Prompt:   "tick",
		Agent:    agent.DefaultAgentName,
		Enabled:  boolPtr(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if row := listingByKey(jobs)["pulse"]; row.Agent != "" {
		t.Errorf("row agent = %q, want empty (inherited is the absence)", row.Agent)
	}
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(text), "agent: default") {
		t.Errorf("the write pinned a phantom main-agent override:\n%s", text)
	}
	// and the chain keeps resolving at fire time: the reloaded job inherits the main agent
	for _, j := range r.jobsSnapshot() {
		if j.Name == "pulse" && j.ResolveAgent() != agent.DefaultAgentName {
			t.Errorf("pulse resolves to %q, want the main agent", j.ResolveAgent())
		}
	}
}

func TestSaveJobKeepsAnAuthoredWakeAgent(t *testing.T) {
	r, path := newSaveTestRunner(t, saveTestCrons)
	if _, err := r.SaveJob(Job{
		Name:     "pulse",
		Schedule: "0 * * * *",
		Prompt:   "tick",
		Deliver:  deliverContinuous,
		Agent:    "scout",
		Enabled:  boolPtr(),
	}); err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(text), "agent: scout") {
		t.Errorf("an authored wake agent was dropped:\n%s", text)
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
