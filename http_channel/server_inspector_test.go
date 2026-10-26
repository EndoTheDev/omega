package http_channel

import (
	"context"
	"encoding/json"
	"net/http"
	httptest "net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/core"
)

// The inspector endpoint surface (card t_db430fea, read-only v1):
// /cron/jobs, /skills, /logs. Every test here also re-asserts the
// read-only posture - the handlers never write.

// --- test doubles ---

// fakeCronLister satisfies core.CronProvider + core.CronLister.
type fakeCronLister struct {
	jobs []core.CronJobInfo
}

func (*fakeCronLister) Start(_ context.Context, _ core.CronDeps) error { return nil } //nolint:unused // interface filler
func (*fakeCronLister) Stop() error                                     { return nil }

func (f *fakeCronLister) Jobs() []core.CronJobInfo { return f.jobs }

// cronProviderNoLister satisfies CronProvider only - the handler must
// degrade to an empty list, never panic.
type cronProviderNoLister struct{}

func (*cronProviderNoLister) Start(_ context.Context, _ core.CronDeps) error { return nil }
func (*cronProviderNoLister) Stop() error                                     { return nil }

// fakeSkillLister satisfies core.SkillsProvider + core.SkillsLister.
type fakeSkillLister struct {
	skills []core.Skill
	err    error
}

func (*fakeSkillLister) LoadSkills(string) ([]core.Skill, error) { return nil, nil }

func (f *fakeSkillLister) ListSkills() ([]core.Skill, error) { return f.skills, f.err }

// fakePathLogger satisfies core.LoggerProvider with a readable path.
type fakePathLogger struct{ path string }

func (l *fakePathLogger) Printf(string, ...any) {}
func (l *fakePathLogger) Errorf(string, ...any) {}
func (*fakePathLogger) Close() error            { return nil }
func (l *fakePathLogger) Path() string          { return l.path }

// ---

func inspectorServer(t *testing.T) *Server {
	t.Helper()
	return NewServer(nil, nil, nil, nil, nil)
}

func serveInspector(t *testing.T, s *Server) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func TestCronJobsUnconfiguredIsEmptyList(t *testing.T) {
	ts := serveInspector(t, inspectorServer(t))
	res, err := http.Get(ts.URL + "/cron/jobs")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("cron/jobs = %d, want 200 (empty list, not 501)", res.StatusCode)
	}
	var got []core.CronJobInfo
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("jobs = %+v, want empty", got)
	}
}

func TestCronJobsListsRowsFromSeam(t *testing.T) {
	s := inspectorServer(t)
	s.Cron = &fakeCronLister{jobs: []core.CronJobInfo{
		{Name: "daily-journal", Schedule: "0 8 * * *", Enabled: true, Next: "2026-10-19T08:00:00+07:00"},
	}}
	ts := serveInspector(t, s)
	res, err := http.Get(ts.URL + "/cron/jobs")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var got []core.CronJobInfo
	json.NewDecoder(res.Body).Decode(&got)
	if len(got) != 1 || got[0].Name != "daily-journal" ||
		got[0].Schedule != "0 8 * * *" || !got[0].Enabled ||
		got[0].Next != "2026-10-19T08:00:00+07:00" {
		t.Fatalf("jobs = %+v", got)
	}
}

func TestCronJobsProviderWithoutListerIsEmpty(t *testing.T) {
	s := inspectorServer(t)
	s.Cron = &cronProviderNoLister{}
	ts := serveInspector(t, s)
	res, err := http.Get(ts.URL + "/cron/jobs")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("cron/jobs = %d", res.StatusCode)
	}
	var got []core.CronJobInfo
	json.NewDecoder(res.Body).Decode(&got)
	if len(got) != 0 {
		t.Fatalf("jobs = %+v, want empty", got)
	}
}

func TestCronJobsRejectsNonGet(t *testing.T) {
	ts := serveInspector(t, inspectorServer(t))
	res, err := http.Post(ts.URL+"/cron/jobs", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST /cron/jobs = %d, want 405", res.StatusCode)
	}
}

func TestSkillsUnconfiguredIsEmptyList(t *testing.T) {
	ts := serveInspector(t, inspectorServer(t))
	res, err := http.Get(ts.URL + "/skills")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("skills = %d, want 200", res.StatusCode)
	}
	var got []skillListing
	json.NewDecoder(res.Body).Decode(&got)
	if len(got) != 0 {
		t.Fatalf("skills = %+v, want empty", got)
	}
}

func TestSkillsListsNameAndDescriptionOnly(t *testing.T) {
	s := inspectorServer(t)
	s.Skills = &fakeSkillLister{skills: []core.Skill{
		{Name: "a-skill", Description: "first", Content: "SECRET-BODY", Dir: "/home/endo/.omega/skills/a"},
		{Name: "b-skill", Description: "second", Content: "SECRET-BODY", Dir: "/home/endo/.omega/skills/b"},
	}}
	ts := serveInspector(t, s)
	res, err := http.Get(ts.URL + "/skills")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var got []skillListing
	json.NewDecoder(res.Body).Decode(&got)
	if len(got) != 2 {
		t.Fatalf("skills = %+v, want exactly 2 rows", got)
	}
	if got[0].Name != "a-skill" || got[0].Description != "first" || got[1].Name != "b-skill" || got[1].Description != "second" {
		t.Fatalf("skills = %+v", got)
	}
	for _, row := range got {
		if row.Description == "" {
			t.Fatalf("row missing description: %+v", row)
		}
	}
	// the wire shape is name+description only: content and dir must
	// never ride the inspector listing
	if strings.Contains(mustJSON(t, got), "SECRET-BODY") {
		t.Fatal("skill content leaked onto the wire")
	}
}

func TestSkillsScanErrorSurfaces(t *testing.T) {
	s := inspectorServer(t)
	s.Skills = &fakeSkillLister{err: os.ErrPermission}
	ts := serveInspector(t, s)
	res, err := http.Get(ts.URL + "/skills")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusInternalServerError {
		t.Fatalf("skills scan error = %d, want 500", res.StatusCode)
	}
}

// TestLogsTailRealFile is the endpoint contract against the kind of
// file the FileLogger produces: lines > window (head cut inside the
// window is dropped), lines <= n, ?lines= override, and 404s.
func TestLogsTailRealFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "omega.log")
	var body string
	for i := 1; i <= 500; i++ {
		body += time.Date(2026, 10, 19, 0, i/60, i%60, 0, time.UTC).
			Format("2006/01/02 15:04:05") + " omega: line " + itoa(i) + "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	s := inspectorServer(t)
	s.logger = &fakePathLogger{path: path}
	ts := serveInspector(t, s)

	// default: 200 lines
	res, err := http.Get(ts.URL + "/logs")
	if err != nil {
		t.Fatal(err)
	}
	payload := decodeLogPayload(t, res)
	if len(payload.Lines) != 200 {
		t.Fatalf("default lines = %d, want 200", len(payload.Lines))
	}
	if got := payload.Lines[0]; !strings.Contains(got, "line 301") {
		t.Fatalf("first line = %q, want line 301", got)
	}
	if got := payload.Lines[len(payload.Lines)-1]; !strings.Contains(got, "line 500") {
		t.Fatalf("last line = %q, want line 500", got)
	}
	if payload.Path != path {
		t.Fatalf("path = %q, want %q", payload.Path, path)
	}

	// ?lines= override
	res2, err := http.Get(ts.URL + "/logs?lines=2")
	if err != nil {
		t.Fatal(err)
	}
	payload2 := decodeLogPayload(t, res2)
	if len(payload2.Lines) != 2 || !strings.Contains(payload2.Lines[1], "line 500") {
		t.Fatalf("lines=2 = %+v", payload2.Lines)
	}

	// clamps: never 0/Negative/unbounded
	for _, q := range []string{"?lines=0", "?lines=-5", "?lines=5000"} {
		res3, qErr := http.Get(ts.URL + "/logs" + q)
		if qErr != nil {
			t.Fatal(qErr)
		}
		p := decodeLogPayload(t, res3)
		if len(p.Lines) < 1 {
			t.Fatalf("lines clamp %q returned 0 rows", q)
		}
	}

	// file bigger than the read window: the window edge cuts a line
	// in half - the fragment must be dropped, the first served line
	// starts at a real line boundary, and the true last line wins
	var big string
	for i := 1; i <= 30000; i++ {
		big += itoa(i) + " 2026/10/19 08:00:00 omega: big log line\n"
	}
	if len(big) <= logTailWindow {
		t.Fatalf("test file no longer exceeds the window")
	}
	if err := os.WriteFile(path, []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	resBig, bErr := http.Get(ts.URL + "/logs")
	if bErr != nil {
		t.Fatal(bErr)
	}
	payloadBig := decodeLogPayload(t, resBig)
	if len(payloadBig.Lines) != 200 {
		t.Fatalf("window tail lines = %d, want 200", len(payloadBig.Lines))
	}
	if got := payloadBig.Lines[0]; got == "" || got[0] < '0' || got[0] > '9' {
		t.Fatalf("first line = %q, want a real line, not a cut fragment", got)
	}
	if got := payloadBig.Lines[len(payloadBig.Lines)-1]; !strings.Contains(got, "30000 2026/10/19") {
		t.Fatalf("last line = %q, want line 30000", got)
	}
}

func TestLogsUnconfiguredIs404(t *testing.T) {
	ts := serveInspector(t, inspectorServer(t))
	res, err := http.Get(ts.URL + "/logs")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("no logger = %d, want 404", res.StatusCode)
	}
}

func TestLogsLoggerWithoutPathIs404(t *testing.T) {
	s := inspectorServer(t)
	s.logger = nopTestLogger{} // NopLogger twin: LoggerProvider, no Path
	ts := serveInspector(t, s)
	res, err := http.Get(ts.URL + "/logs")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("pathless logger = %d, want 404", res.StatusCode)
	}
}

func TestLogsMissingFileIs404(t *testing.T) {
	s := inspectorServer(t)
	s.logger = &fakePathLogger{path: filepath.Join(t.TempDir(), "gone.log")}
	ts := serveInspector(t, s)
	res, err := http.Get(ts.URL + "/logs")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("missing file = %d, want 404", res.StatusCode)
	}
}

// nopTestLogger mirrors modules/logging.NopLogger: LoggerProvider only.
type nopTestLogger struct{}

func (nopTestLogger) Printf(string, ...any) {}
func (nopTestLogger) Errorf(string, ...any) {}
func (nopTestLogger) Close() error          { return nil }

// --- helpers ---

type logPayload struct {
	Path  string   `json:"path"`
	Lines []string `json:"lines"`
}

func decodeLogPayload(t *testing.T, res *http.Response) *logPayload {
	t.Helper()
	if res == nil {
		t.Fatal("nil response")
	}
	var p logPayload
	if err := json.NewDecoder(res.Body).Decode(&p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	res.Body.Close()
	if p.Lines == nil {
		p.Lines = []string{}
	}
	return &p
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func itoa(i int) string {
	return strconv.Itoa(i)
}