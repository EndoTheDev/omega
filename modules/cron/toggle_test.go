package cron

// toggle_test.go: the per-job `enabled:` flag write (card t_561dc4e0).
// The write is the fold-write pattern: parse at the yaml.Node level,
// mutate the one scalar, marshal the whole document - so every test
// that flips a flag also pins the file's surviving style (comments,
// key order, untouched siblings). The route test pins the wire: the
// fresh job set rides the response body, unknown keys are the loud
// 404 lane.

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/engine/agent"
)

const toggleTestCrons = `# the human-authored file: a comment that must survive every write
jobs:
  - name: daily-journal   # the owner's note rides the entry
    schedule: "0 8 * * *"
    prompt: journal
  - name: nightly-lock
    schedule: "0 23 * * *"
    prompt: lock
    enabled: false
`

func newToggleTestRunner(t *testing.T, mainYAML string) (*Runner, string) {
	t.Helper()
	home := t.TempDir()
	path := filepath.Join(home, "crons.yaml")
	if err := os.WriteFile(path, []byte(mainYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadAllConfigs(home)
	if err != nil {
		t.Fatal(err)
	}
	return NewRunner(cfg, home), path
}

// TestToggleJobWritesFlagAndKeepsStyle: disabling a job appends the
// `enabled: false` flag to that one entry, comments and siblings
// survive, and the fresh listing reports the landed state (Jobs is the
// re-read after the reload).
func TestToggleJobWritesFlagAndKeepsStyle(t *testing.T) {
	r, path := newToggleTestRunner(t, toggleTestCrons)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	jobs, err := r.ToggleJob("daily-journal", false)
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]core.CronJobInfo{}
	for _, j := range jobs {
		byKey[j.Name] = j
	}
	if j := byKey["daily-journal"]; j.Enabled {
		t.Error("daily-journal enabled after the disable write")
	}
	if j := byKey["nightly-lock"]; j.Enabled {
		t.Error("nightly-lock enabled state moved (the untouched sibling keeps its flag)")
	}
	if j := byKey["daily-journal"]; j.Next != "" {
		t.Errorf("disabled job promises a next fire: %q", j.Next)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(before, after) {
		t.Error("the disable write touched nothing")
	}
	text := string(after)
	for _, want := range []string{
		"# the human-authored file: a comment that must survive every write",
		"# the owner's note rides the entry",
		`schedule: "0 8 * * *"`,
		"enabled: false",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("written file lost %s:\n%s", want, text)
		}
	}
	if got := strings.Count(text, "enabled:"); got != 2 {
		t.Errorf("enabled: count = %d, want 2 (one per entry, no duplicate)", got)
	}
	// the flag belongs to the daily-journal entry's own key set - the
	// exact indentation is yaml's marshal call (the node style re-flows
	// sequences); the entry location is what the fold-write owns
	lines := strings.Split(text, "\n")
	inJournal := false
	found := false
	for _, ln := range lines {
		if strings.Contains(ln, "name: daily-journal") {
			inJournal = true
		} else if strings.HasPrefix(strings.TrimSpace(ln), "- name:") {
			inJournal = false
		}
		if inJournal && strings.TrimSpace(ln) == "enabled: false" {
			found = true
		}
	}
	if !found {
		t.Errorf("daily-journal entry lacks the written flag:\n%s", text)
	}
}

// TestToggleJobEnableWritesTrue: an explicit `enabled: false` flips to
// true in place (the scalar is mutated, not duplicated).
func TestToggleJobEnableWritesTrue(t *testing.T) {
	r, path := newToggleTestRunner(t, toggleTestCrons)
	if _, err := r.ToggleJob("nightly-lock", true); err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(text), "enabled:"); got != 1 {
		t.Errorf("enabled: count = %d, want 1 (the flip mutates nightly's flag; daily never carries one)", got)
	}
	lines := strings.Split(string(text), "\n")
	inLock := false
	found := false
	for _, ln := range lines {
		if strings.Contains(ln, "name: nightly-lock") {
			inLock = true
		} else if strings.HasPrefix(ln, "  - name:") {
			inLock = false
		}
		if inLock && strings.TrimSpace(ln) == "enabled: true" {
			found = true
		}
	}
	if !found {
		t.Errorf("nightly-lock entry lacks the enabled: true scalar:\n%s", text)
	}
}

// TestToggleJobUnknownKeyIsLoudAndUntouched: a key no loaded job knows
// errors as the 404 lane and writes no byte.
func TestToggleJobUnknownKeyIsLoudAndUntouched(t *testing.T) {
	r, path := newToggleTestRunner(t, toggleTestCrons)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.ToggleJob("ghost-job", false)
	if err == nil {
		t.Fatal("unknown key accepted")
	}
	var unknown UnknownJobError
	if !errors.As(err, &unknown) || unknown.Key != "ghost-job" {
		t.Errorf("err = %v, want UnknownJobError(ghost-job)", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("the file moved on a refused toggle")
	}
}

// TestToggleJobAgentHomeWrite: a wake's flag lands in the agent home's
// config/crons.yaml; the main home's file never moves.
func TestToggleJobAgentHomeWrite(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "crons.yaml"),
		[]byte("jobs:\n  - name: main-job\n    schedule: \"0 8 * * *\"\n    prompt: p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := agent.ScaffoldAgent(home, "scout", ""); err != nil {
		t.Fatal(err)
	}
	scoutPath := filepath.Join(home, "agents", "scout", "config", "crons.yaml")
	if err := os.WriteFile(scoutPath,
		[]byte("jobs:\n  - name: wake\n    schedule: \"17 * * * *\"\n    prompt: patrol\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadAllConfigs(home)
	if err != nil {
		t.Fatal(err)
	}
	r := NewRunner(cfg, home)

	jobs, err := r.ToggleJob("scout/wake", false)
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]core.CronJobInfo{}
	for _, j := range jobs {
		byKey[j.Name+"|"+j.Agent] = j
	}
	if j := byKey["wake|scout"]; j.Enabled {
		t.Error("scout/wake enabled after the disable write")
	}
	if j, ok := byKey["main-job|"]; !ok || !j.Enabled {
		t.Error("main-job row lost or moved by an agent-home toggle")
	}
	text, err := os.ReadFile(scoutPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(text), "enabled: false") {
		t.Errorf("scout crons.yaml lacks the written flag:\n%s", text)
	}
}

// TestHandleToggleWire: the route answers the fresh job set as JSON,
// unknown keys are the 404 lane, junk bodies the 400 lane, wrong
// methods the 405 lane.
func TestHandleToggleWire(t *testing.T) {
	r, _ := newToggleTestRunner(t, toggleTestCrons)
	p := &Plugin{runner: r}

	serve := func(method, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/cron/toggle", strings.NewReader(body))
		rec := httptest.NewRecorder()
		p.handleToggle(rec, req)
		return rec
	}

	res := serve("POST", `{"key": "daily-journal", "enabled": false}`)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.Code)
	}
	if !strings.Contains(res.Body.String(), `"enabled":false`) {
		t.Errorf("response does not carry the landed state:\n%s", res.Body.String())
	}
	if res.Header().Get("Content-Type") != "application/json" {
		t.Errorf("content type = %q, want application/json", res.Header().Get("Content-Type"))
	}
	if res2 := serve("POST", `{"key": "ghost", "enabled": true}`); res2.Code != http.StatusNotFound {
		t.Errorf("unknown key status = %d, want 404", res2.Code)
	}
	if res3 := serve("POST", `{}`); res3.Code != http.StatusBadRequest {
		t.Errorf("junk body status = %d, want 400", res3.Code)
	}
	if res4 := serve("PUT", `{"key": "daily-journal", "enabled": false}`); res4.Code != http.StatusMethodNotAllowed {
		t.Errorf("PUT status = %d, want 405", res4.Code)
	}
}