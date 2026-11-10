package observability

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// fakeLogger is the logger service's consumer face under test: only the
// path crosses (the capsule's own logPather interface).
type fakeLogger struct{ path string }

func (f fakeLogger) Path() string { return f.path }

// newTestRoutes builds the REST face over a seeded ring; ctx may be nil
// (the log route then answers 404, the honest no-logger state).
func newTestRoutes(ctx *core.Context, events []Event) (*Routes, *Ring) {
	ring := NewRing(32)
	for _, e := range events {
		ring.Append(e)
	}
	return newRoutes(ctx, ring), ring
}

// TestEventsRouteServesSessionScopedPage pins the primary endpoint's
// wire: the JSON envelope, the session filter, the bounded page and the
// errors-only level parameter.
func TestEventsRouteServesSessionScopedPage(t *testing.T) {
	rt, _ := newTestRoutes(nil, []Event{
		{Session: "s1", Type: "agent_start", Level: LevelInfo, Summary: "run started"},
		{Session: "s2", Type: "agent_start", Level: LevelInfo, Summary: "run started"},
		{Session: "s1", Type: "tool_result", Level: LevelError, Tool: "shell.run", Summary: "shell.run failed", Error: "exit 1"},
	})

	decode := func(t *testing.T, url string) eventsResponse {
		t.Helper()
		res := httptest.NewRecorder()
		rt.handleEvents(res, httptest.NewRequest(http.MethodGet, url, nil))
		if res.Code != http.StatusOK {
			t.Fatalf("%s: status %d, want 200 (body %q)", url, res.Code, res.Body.String())
		}
		var got eventsResponse
		if err := json.Unmarshal(res.Body.Bytes(), &got); err != nil {
			t.Fatalf("%s: decode: %v", url, err)
		}
		return got
	}

	all := decode(t, "/observability/events")
	if all.Returned != 3 || all.Buffered != 3 || all.Capacity != 32 {
		t.Fatalf("unscoped page = %+v, want 3 returned of 3 buffered in a 32 ring", all)
	}

	scoped := decode(t, "/observability/events?session=s1")
	if scoped.Returned != 2 {
		t.Fatalf("session=s1 returned %d rows, want 2", scoped.Returned)
	}
	for _, e := range scoped.Events {
		if e.Session != "s1" {
			t.Fatalf("session leak: %+v", e)
		}
	}

	errs := decode(t, "/observability/events?level=error&session=s1")
	if errs.Returned != 1 || errs.Events[0].Tool != "shell.run" {
		t.Fatalf("errors-only page = %+v, want the one failing tool row", errs)
	}

	limited := decode(t, "/observability/events?limit=1")
	if limited.Returned != 1 || limited.Events[0].Summary != "shell.run failed" {
		t.Fatalf("limit=1 page = %+v, want the newest row only", limited)
	}
}

// TestEventsRouteRefusesBadInput pins the guards: an unknown level is a
// 400 (never a silently empty page), a non-GET is a 405, and an empty
// ring serves an empty array rather than a null.
func TestEventsRouteRefusesBadInput(t *testing.T) {
	rt, _ := newTestRoutes(nil, nil)

	res := httptest.NewRecorder()
	rt.handleEvents(res, httptest.NewRequest(http.MethodGet, "/observability/events?level=loud", nil))
	if res.Code != http.StatusBadRequest {
		t.Fatalf("unknown level: status %d, want 400", res.Code)
	}

	res = httptest.NewRecorder()
	rt.handleEvents(res, httptest.NewRequest(http.MethodPost, "/observability/events", nil))
	if res.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST: status %d, want 405", res.Code)
	}

	res = httptest.NewRecorder()
	rt.handleEvents(res, httptest.NewRequest(http.MethodGet, "/observability/events", nil))
	var got eventsResponse
	if err := json.Unmarshal(res.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode empty page: %v", err)
	}
	if got.Events == nil || len(got.Events) != 0 {
		t.Fatalf("empty ring served %v, want an empty array", got.Events)
	}
}

// TestLogsRouteServesTail pins the secondary endpoint: the configured
// log's last lines, the path it came from, and the ?lines= bound.
func TestLogsRouteServesTail(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "omega.log")
	var sb strings.Builder
	for i := 1; i <= 5; i++ {
		fmt.Fprintf(&sb, "2026/11/10 10:00:0%d line %d\n", i, i)
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := &core.Context{}
	ctx.ApplyOffer("logger", "test", fakeLogger{path: path})
	rt, _ := newTestRoutes(ctx, nil)

	res := httptest.NewRecorder()
	rt.handleLogs(res, httptest.NewRequest(http.MethodGet, "/observability/logs?lines=2", nil))
	if res.Code != http.StatusOK {
		t.Fatalf("status %d, want 200 (body %q)", res.Code, res.Body.String())
	}
	var got logsResponse
	if err := json.Unmarshal(res.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Path != path {
		t.Fatalf("path = %q, want %q", got.Path, path)
	}
	if len(got.Lines) != 2 || !strings.HasSuffix(got.Lines[1], "line 5") {
		t.Fatalf("lines = %v, want the last two ending at line 5", got.Lines)
	}
}

// TestLogsRouteDegrades pins the two honest failures: no logger offer at
// all, and a logger without a readable file.
func TestLogsRouteDegrades(t *testing.T) {
	rt, _ := newTestRoutes(nil, nil)
	res := httptest.NewRecorder()
	rt.handleLogs(res, httptest.NewRequest(http.MethodGet, "/observability/logs", nil))
	if res.Code != http.StatusNotFound {
		t.Fatalf("no logger offer: status %d, want 404", res.Code)
	}

	ctx := &core.Context{}
	ctx.ApplyOffer("logger", "test", fakeLogger{path: filepath.Join(t.TempDir(), "missing.log")})
	rt, _ = newTestRoutes(ctx, nil)
	res = httptest.NewRecorder()
	rt.handleLogs(res, httptest.NewRequest(http.MethodGet, "/observability/logs", nil))
	if res.Code != http.StatusNotFound {
		t.Fatalf("missing file: status %d, want 404", res.Code)
	}
}

// TestTailLines pins the tail reader itself: the last n lines, the
// whole-file path, the empty file and the cut-off head fragment of a
// window that starts mid-file.
func TestTailLines(t *testing.T) {
	dir := t.TempDir()
	small := filepath.Join(dir, "small.log")
	if err := os.WriteFile(small, []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, err := tailLines(small, 2)
	if err != nil {
		t.Fatalf("tail small: %v", err)
	}
	if len(lines) != 2 || lines[0] != "b" || lines[1] != "c" {
		t.Fatalf("tail = %v, want [b c]", lines)
	}
	if lines, err = tailLines(small, 100); err != nil || len(lines) != 3 {
		t.Fatalf("tail whole file = %v (err %v), want 3 lines", lines, err)
	}

	empty := filepath.Join(dir, "empty.log")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if lines, err = tailLines(empty, 10); err != nil || len(lines) != 0 {
		t.Fatalf("tail empty = %v (err %v), want zero lines", lines, err)
	}

	if _, err := tailLines(filepath.Join(dir, "nope.log"), 10); err == nil {
		t.Fatal("tail of a missing file returned no error")
	}

	// Bigger than the read window: every returned line must be a WHOLE
	// line, never the cut-off fragment the window starts on.
	big := filepath.Join(dir, "big.log")
	var sb strings.Builder
	for i := 0; sb.Len() < logTailWindow+4096; i++ {
		fmt.Fprintf(&sb, "2026/11/10 10:00:00 whole-line-%08d\n", i)
	}
	if err := os.WriteFile(big, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, err = tailLines(big, 50)
	if err != nil {
		t.Fatalf("tail big: %v", err)
	}
	if len(lines) != 50 {
		t.Fatalf("big tail returned %d lines, want 50", len(lines))
	}
	for _, line := range lines {
		if !strings.HasPrefix(line, "2026/11/10 10:00:00 whole-line-") {
			t.Fatalf("partial line leaked out of the window: %q", line)
		}
	}
}
