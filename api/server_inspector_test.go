package api

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

	"github.com/EndoTheDev/omega/agent"
	agentapi "github.com/EndoTheDev/omega/agent/api"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/logging"
)

// The inspector endpoint surface (card t_db430fea, read-only v1):
// /cron/jobs, /skills, /logs. Every test here also re-asserts the
// read-only posture - the handlers never write.

// --- test doubles ---

// cronRow decodes one /cron/jobs wire row (the capsule pre-marshals;
// the shape is the row the shared type always produced).
type cronRow struct {
	Name     string `json:"name"`
	Schedule string `json:"schedule"`
	Enabled  bool   `json:"enabled"`
	Next     string `json:"next"`
}

// fakeCronRows is the cron capsule's wire face (part 3): the route
// resolves "cron" and asserts JobRows - std types only.
type fakeCronRows struct{ rows []json.RawMessage }

func (f *fakeCronRows) JobRows() []json.RawMessage { return f.rows }

// cronNoRows offers a "cron" service without the row face - the
// handler must degrade to an empty list, never panic.
type cronNoRows struct{}

func (cronNoRows) Stop() error { return nil }

// fakeSkillEntries serves the std-typed inventory face (part 3).
type fakeSkillEntries struct {
	index map[string]string
	err   error
}

func (f *fakeSkillEntries) SkillsEntries() (map[string]string, map[string]string, error) {
	return f.index, map[string]string{}, f.err
}

// fakePathLogger satisfies agent.LoggerProvider with a readable path.
type fakePathLogger struct{ path string }

func (l *fakePathLogger) Printf(string, ...any) {}
func (l *fakePathLogger) Warnf(string, ...any)  {}
func (l *fakePathLogger) Errorf(string, ...any) {}
func (*fakePathLogger) Close() error            { return nil }
func (l *fakePathLogger) Path() string          { return l.path }

// ---

func inspectorServer(t *testing.T) *Server {
	t.Helper()
	return NewServer(nil, nil, nil, nil, nil, nil)
}

func serveInspector(t *testing.T, s *Server) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts
}

// wireInspector offers the fake supplies into a fresh Context and
// rewires the server against it - the same rewire the live toggle runs
// (one code path, no test-only seam plumbing).
func wireInspector(t *testing.T, s *Server, offers map[string]any, logger agent.LoggerProvider) {
	t.Helper()
	ctx := &core.Context{}
	for k, v := range offers {
		ctx.ApplyOffer(k, "fake", v)
	}
	ctx.ApplyOffer("logger", "test", logger)
	if err := s.RewireSeams(ctx, nil, nil); err != nil {
		t.Fatalf("rewire: %v", err)
	}
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
	var got []cronRow
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("jobs = %+v, want empty", got)
	}
}

func TestCronJobsListsRowsFromSeam(t *testing.T) {
	s := inspectorServer(t)
	row, mErr := json.Marshal(cronRow{Name: "daily-journal", Schedule: "0 8 * * *", Enabled: true, Next: "2026-10-19T08:00:00+07:00"})
	if mErr != nil {
		t.Fatal(mErr)
	}
	wireInspector(t, s, map[string]any{"cron": &fakeCronRows{rows: []json.RawMessage{row}}}, nil)
	ts := serveInspector(t, s)
	res, err := http.Get(ts.URL + "/cron/jobs")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var got []cronRow
	json.NewDecoder(res.Body).Decode(&got)
	if len(got) != 1 || got[0].Name != "daily-journal" ||
		got[0].Schedule != "0 8 * * *" || !got[0].Enabled ||
		got[0].Next != "2026-10-19T08:00:00+07:00" {
		t.Fatalf("jobs = %+v", got)
	}
}

func TestCronJobsProviderWithoutListerIsEmpty(t *testing.T) {
	s := inspectorServer(t)
	wireInspector(t, s, map[string]any{"cron": cronNoRows{}}, nil)
	ts := serveInspector(t, s)
	res, err := http.Get(ts.URL + "/cron/jobs")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("cron/jobs = %d", res.StatusCode)
	}
	var got []cronRow
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
	wireInspector(t, s, map[string]any{"skills": &fakeSkillEntries{index: map[string]string{
		"a-skill": "first",
		"b-skill": "second",
	}}}, nil)
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
	wireInspector(t, s, map[string]any{"skills": &fakeSkillEntries{err: os.ErrPermission}}, nil)
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
	wireInspector(t, s, nil, &fakePathLogger{path: path})
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
	// NopLogger twin: LoggerProvider, no Path
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
	wireInspector(t, s, nil, &fakePathLogger{path: filepath.Join(t.TempDir(), "gone.log")})
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

// nopTestLogger mirrors harness/logging.NopLogger: LoggerProvider only.
type nopTestLogger struct{}

func (nopTestLogger) Printf(string, ...any) {}
func (nopTestLogger) Warnf(string, ...any)  {}
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

// --- the logs topic's delta follower (card t_0e7bc0bb) ---

// readLogFrame reads one pushed frame off a hub subscriber, failing on
// timeout. The logs topic is the only one under test here.
func readLogFrame(t *testing.T, sub *agentapi.SocketSub) agentapi.SocketMessage {
	t.Helper()
	select {
	case raw, ok := <-sub.Messages():
		if !ok {
			t.Fatal("subscriber channel closed, want a logs frame")
		}
		var msg agentapi.SocketMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			t.Fatalf("decode frame: %v", err)
		}
		return msg
	case <-time.After(2 * time.Second):
		t.Fatal("no logs frame within 2s")
		return agentapi.SocketMessage{}
	}
}

// appendLog appends raw bytes to the followed file (the logger's own
// write shape: whole lines, newline-terminated).
func appendLog(t *testing.T, path, text string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	defer f.Close()
	if _, err := f.WriteString(text); err != nil {
		t.Fatalf("append log: %v", err)
	}
}

// TestPushLogDelta: the follower publishes ONLY the bytes appended since
// the last offset, as whole lines - the snapshot half stays GET /logs.
// It also pins the two skip laws: no subscriber means no read (the
// offset advances anyway), and a partial write is carried to the tick
// that completes the line.
func TestPushLogDelta(t *testing.T) {
	path := filepath.Join(t.TempDir(), "omega.log")
	if err := os.WriteFile(path, []byte("boot line\n"), 0o644); err != nil {
		t.Fatalf("seed log: %v", err)
	}
	s := newTestServer()

	// nobody holds the topic: the offset walks to the end, nothing is
	// published and no bytes are read
	off, carry := s.pushLogDelta(path, 0, "")
	if off != int64(len("boot line\n")) || carry != "" {
		t.Fatalf("no-subscriber tick = (%d, %q), want (%d, \"\")", off, carry, len("boot line\n"))
	}
	if s.Sockets().Wants(agentapi.TopicLogs) {
		t.Fatal("a bare hub reports the logs topic as wanted")
	}

	sub := s.Sockets().Attach()
	defer sub.Close()
	if !s.Sockets().Wants(agentapi.TopicLogs) {
		t.Fatal("an attached subscriber (subscribed to every topic) is not wanted on logs")
	}

	// a write that lands mid-line is held, not published
	appendLog(t, path, "second line\nhalf a li")
	off, carry = s.pushLogDelta(path, off, carry)
	if carry != "half a li" {
		t.Fatalf("carry = %q, want the incomplete tail", carry)
	}
	msg := readLogFrame(t, sub)
	if msg.Topic != agentapi.TopicLogs || msg.Session != "" {
		t.Fatalf("frame = %+v, want a session-less logs frame", msg)
	}
	var delta agentapi.LogLines
	if err := json.Unmarshal(msg.Data, &delta); err != nil {
		t.Fatalf("decode logs payload: %v", err)
	}
	if len(delta.Lines) != 1 || delta.Lines[0] != "second line" {
		t.Fatalf("delta = %v, want exactly [second line]", delta.Lines)
	}

	// the completing write emits the whole line, carry cleared
	appendLog(t, path, "ne\n")
	off, carry = s.pushLogDelta(path, off, carry)
	if carry != "" {
		t.Fatalf("carry after the completing write = %q, want empty", carry)
	}
	if err := json.Unmarshal(readLogFrame(t, sub).Data, &delta); err != nil {
		t.Fatalf("decode second delta: %v", err)
	}
	if len(delta.Lines) != 1 || delta.Lines[0] != "half a line" {
		t.Fatalf("delta = %v, want exactly [half a line]", delta.Lines)
	}

	// no new bytes: no frame (the follower is silent, never repetitive)
	off, carry = s.pushLogDelta(path, off, carry)
	select {
	case raw := <-sub.Messages():
		t.Fatalf("frame with no appended bytes: %s", raw)
	case <-time.After(100 * time.Millisecond):
	}

	// truncation/rotation resyncs to the end instead of replaying
	if err := os.WriteFile(path, []byte("rotated\n"), 0o644); err != nil {
		t.Fatalf("rotate log: %v", err)
	}
	off, carry = s.pushLogDelta(path, off, carry)
	if off != int64(len("rotated\n")) || carry != "" {
		t.Fatalf("post-truncation tick = (%d, %q), want the new file size and no carry", off, carry)
	}
}

// TestFollowLogsPublishesAppends: the follower goroutine wires the file
// to the topic end to end - a line appended while a subscriber holds the
// topic arrives without any REST read in between, and it stops when the
// server context ends.
func TestFollowLogsPublishesAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "omega.log")
	if err := os.WriteFile(path, []byte("boot line\n"), 0o644); err != nil {
		t.Fatalf("seed log: %v", err)
	}
	logger, err := logging.NewFileLogger(path)
	if err != nil {
		t.Fatalf("logger: %v", err)
	}
	t.Cleanup(func() { _ = logger.Close() })
	s := newTestServer()
	s.BindInspectSeams(logger)

	sub := s.Sockets().Attach()
	defer sub.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.followLogs(ctx)
	}()
	// the first tick seeds the offset to the current end (the socket
	// never replays history) - let it pass before appending the line
	// this test expects to be pushed
	time.Sleep(logPushInterval + 200*time.Millisecond)

	appendLog(t, path, "pushed line\n")
	msg := readLogFrame(t, sub)
	var delta agentapi.LogLines
	if err := json.Unmarshal(msg.Data, &delta); err != nil {
		t.Fatalf("decode logs payload: %v", err)
	}
	if len(delta.Lines) != 1 || delta.Lines[0] != "pushed line" {
		t.Fatalf("delta = %v, want [pushed line]", delta.Lines)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(testWaitTimeout):
		t.Fatal("followLogs did not exit on context cancellation")
	}
}
