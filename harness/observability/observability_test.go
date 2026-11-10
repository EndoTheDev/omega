package observability

import (
	"context"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// fixedClock returns a settable time source for the recorder's pairing
// math: durations are derived from two observations, so a real clock
// would make the assertions flaky by design.
type fixedClock struct{ t time.Time }

func (c *fixedClock) now() time.Time      { return c.t }
func (c *fixedClock) add(d time.Duration) { c.t = c.t.Add(d) }

func newTestRecorder(capacity int) (*Recorder, *Ring, *fixedClock) {
	ring := NewRing(capacity)
	rec := NewRecorder(ring)
	clock := &fixedClock{t: time.Unix(1700000000, 0).UTC()}
	rec.now = clock.now
	return rec, ring, clock
}

// TestRingDropsOldestWhenFull pins the bound itself: a full ring
// overwrites its oldest slot, never grows, and the surviving records are
// the newest ones with their original sequence numbers.
func TestRingDropsOldestWhenFull(t *testing.T) {
	ring := NewRing(3)
	for i := 0; i < 5; i++ {
		ring.Append(Event{Type: "test", Level: LevelInfo, Summary: fmt.Sprintf("e%d", i)})
	}
	if got := ring.Len(); got != 3 {
		t.Fatalf("ring len = %d, want 3 (the cap)", got)
	}
	rows := ring.Page("", "", 10)
	if len(rows) != 3 {
		t.Fatalf("page returned %d rows, want 3: %v", len(rows), rows)
	}
	want := []string{"e2", "e3", "e4"}
	for i, w := range want {
		if rows[i].Summary != w {
			t.Fatalf("row %d = %q, want %q (oldest must be the one dropped)", i, rows[i].Summary, w)
		}
	}
	if rows[0].Seq != 3 || rows[2].Seq != 5 {
		t.Fatalf("sequence numbers drifted: %d..%d, want 3..5", rows[0].Seq, rows[2].Seq)
	}
}

// TestPageOrdersOldestFirstWhileKeepingNewest pins the page contract: a
// bounded page selects the NEWEST matches and returns them in reading
// order, so the last row of any page is the most recent record.
func TestPageOrdersOldestFirstWhileKeepingNewest(t *testing.T) {
	ring := NewRing(16)
	for i := 0; i < 10; i++ {
		ring.Append(Event{Type: "test", Level: LevelInfo, Summary: fmt.Sprintf("e%d", i)})
	}
	rows := ring.Page("", "", 4)
	if len(rows) != 4 {
		t.Fatalf("page returned %d rows, want 4", len(rows))
	}
	if rows[0].Summary != "e6" || rows[3].Summary != "e9" {
		t.Fatalf("page = %q..%q, want e6..e9 (newest four, oldest first)", rows[0].Summary, rows[3].Summary)
	}
	if got := ring.Page("", "", PageMax*2); len(got) != 10 {
		t.Fatalf("page with an oversized limit returned %d rows, want every buffered row (10)", len(got))
	}
}

// TestSessionScoping feeds two conversations' runs through one recorder
// and pins that a scoped read sees exactly its own session - the filter
// the tab's default scope rides.
func TestSessionScoping(t *testing.T) {
	rec, ring, _ := newTestRecorder(32)
	rec.Observe("sess-a", core.AgentStart{Type: "agent_start", ModelName: "m"})
	rec.Observe("sess-b", core.AgentStart{Type: "agent_start", ModelName: "m"})
	rec.Observe("sess-a", core.TurnStart{Type: "turn_start", Turn: 1})
	rec.Observe("sess-b", core.TurnEnd{Type: "turn_end", Turn: 1, ToolCalls: 2})

	a := ring.Page("sess-a", "", 10)
	if len(a) != 2 {
		t.Fatalf("sess-a rows = %d, want 2: %v", len(a), a)
	}
	for _, row := range a {
		if row.Session != "sess-a" {
			t.Fatalf("session leak: row %q carries session %q", row.Type, row.Session)
		}
	}
	all := ring.Page("", "", 10)
	if len(all) != 4 {
		t.Fatalf("unscoped rows = %d, want 4", len(all))
	}
	if none := ring.Page("sess-c", "", 10); len(none) != 0 {
		t.Fatalf("unknown session returned %d rows, want 0", len(none))
	}
}

// TestLevelFilter pins the errors-only view and the warn floor.
func TestLevelFilter(t *testing.T) {
	rec, ring, _ := newTestRecorder(32)
	rec.Observe("s1", core.AgentStart{Type: "agent_start"})
	rec.Observe("s1", core.StreamEvent{Event: core.ToolCallEvent{
		ToolCall: core.ToolCall{ID: "c1", Name: "shell.run"},
	}})
	rec.Observe("s1", core.ToolResultEvent{Message: core.ToolResult{
		ToolCallID: "c1", Content: "boom: exit status 1", IsError: true,
	}})
	rec.Observe("s1", core.AgentEnd{Type: "agent_end", FinishReason: "cancelled", Turns: 3})

	errs := ring.Page("", LevelError, 10)
	if len(errs) != 1 {
		t.Fatalf("errors-only returned %d rows, want 1: %v", len(errs), errs)
	}
	if errs[0].Type != "tool_result" || errs[0].Error == "" {
		t.Fatalf("error row = %+v, want the tool_result with its error text", errs[0])
	}
	if warn := ring.Page("", LevelWarn, 10); len(warn) != 2 {
		t.Fatalf("warn floor returned %d rows, want error + cancelled run (2): %v", len(warn), warn)
	}
	if got := ring.Page("", "bogus", 10); len(got) != 0 {
		t.Fatalf("unknown level returned %d rows, want 0", len(got))
	}
}

// TestDerivedDurations pins the two pairings the probe said must be
// derived: a tool call's call->result span and a turn's start->end span.
// Both are asserted to the millisecond against a pinned clock.
func TestDerivedDurations(t *testing.T) {
	rec, ring, clock := newTestRecorder(32)

	rec.Observe("s1", core.TurnStart{Type: "turn_start", Turn: 1})
	rec.Observe("s1", core.StreamEvent{Event: core.ToolCallEvent{
		ToolCall: core.ToolCall{ID: "call-1", Name: "files.read"},
	}})
	clock.add(1500 * time.Millisecond)
	rec.Observe("s1", core.ToolResultEvent{Message: core.ToolResult{ToolCallID: "call-1", Content: "ok"}})
	clock.add(500 * time.Millisecond)
	rec.Observe("s1", core.TurnEnd{Type: "turn_end", Turn: 1, ToolCalls: 1})

	rows := ring.Page("s1", "", 10)
	if len(rows) != 4 {
		t.Fatalf("rows = %d, want 4: %v", len(rows), rows)
	}
	if rows[2].Type != "tool_result" || rows[2].Tool != "files.read" || rows[2].DurationMs != 1500 {
		t.Fatalf("tool row = %+v, want files.read at 1500ms", rows[2])
	}
	if rows[3].Type != "turn_end" || rows[3].DurationMs != 2000 {
		t.Fatalf("turn row = %+v, want 2000ms", rows[3])
	}
}

// TestUnpairedResultStillReports pins the honesty rule: a result whose
// call the ring never saw is still an event - named "unknown", with no
// fabricated duration.
func TestUnpairedResultStillReports(t *testing.T) {
	rec, ring, _ := newTestRecorder(8)
	rec.Observe("s1", core.ToolResultEvent{Message: core.ToolResult{ToolCallID: "never-seen", Content: "ok"}})
	rows := ring.Page("s1", "", 10)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].Tool != "unknown" || rows[0].DurationMs != 0 {
		t.Fatalf("row = %+v, want tool=unknown with no duration", rows[0])
	}
}

// TestStreamChunksAreDropped keeps the ring structural: a burst of token
// deltas must not evict the rows the tab exists to show.
func TestStreamChunksAreDropped(t *testing.T) {
	rec, ring, _ := newTestRecorder(8)
	rec.Observe("s1", core.AgentStart{Type: "agent_start"})
	for i := 0; i < 100; i++ {
		rec.Observe("s1", core.StreamEvent{Event: core.ResponseChunk{Content: "tok"}})
		rec.Observe("s1", core.StreamEvent{Event: core.ThinkingChunk{Content: "thk"}})
	}
	rec.Observe("s1", core.AssistantMessageEvent{Message: core.Assistant{Content: "done"}})
	if got := ring.Len(); got != 1 {
		t.Fatalf("ring holds %d rows, want only the agent_start (chunks and the assistant copy are dropped)", got)
	}
}

// TestStreamEndErrorsAndTokenCounts pins the stream row: an error is an
// error row with its text, a token ceiling is a warning, and the usage
// numbers ride the summary.
func TestStreamEndErrorsAndTokenCounts(t *testing.T) {
	rec, ring, _ := newTestRecorder(8)
	prompt, eval := 120, 34
	rec.Observe("s1", core.StreamEvent{Event: core.StreamEnd{
		FinishReason: "stop", PromptEvalCount: &prompt, EvalCount: &eval,
	}})
	rec.Observe("s1", core.StreamEvent{Event: core.StreamEnd{FinishReason: "length"}})
	rec.Observe("s1", core.StreamEvent{Event: core.StreamEnd{FinishReason: "error", Error: "provider 500"}})

	rows := ring.Page("s1", "", 10)
	if rows[0].Level != LevelInfo || !strings.Contains(rows[0].Summary, "120 prompt") {
		t.Fatalf("clean stream row = %+v, want info with the token counts", rows[0])
	}
	if rows[1].Level != LevelWarn {
		t.Fatalf("length-capped stream row level = %q, want %q", rows[1].Level, LevelWarn)
	}
	if rows[2].Level != LevelError || rows[2].Error != "provider 500" {
		t.Fatalf("stream error row = %+v, want an error carrying the provider text", rows[2])
	}
}

// TestAgentEndLevels pins the run-end derivation: a failure is an error,
// a cancelled or max-turns finish is a warning, a clean stop is info.
func TestAgentEndLevels(t *testing.T) {
	cases := []struct {
		end   core.AgentEnd
		level string
	}{
		{core.AgentEnd{FinishReason: "stop", Turns: 2}, LevelInfo},
		{core.AgentEnd{FinishReason: "cancelled", Turns: 2}, LevelWarn},
		{core.AgentEnd{FinishReason: "max_turns", Turns: 8}, LevelWarn},
		{core.AgentEnd{FinishReason: "error", Error: "no loop configured"}, LevelError},
	}
	for _, tc := range cases {
		rec, ring, _ := newTestRecorder(4)
		rec.Observe("s1", tc.end)
		row := ring.Page("s1", "", 1)[0]
		if row.Level != tc.level {
			t.Fatalf("finish %q (error %q): level = %q, want %q", tc.end.FinishReason, tc.end.Error, row.Level, tc.level)
		}
		if tc.end.Error != "" && !strings.Contains(row.Error, tc.end.Error) {
			t.Fatalf("error text lost: row error = %q, want it to carry %q", row.Error, tc.end.Error)
		}
	}
}

// TestStopSilencesRecording pins the lifecycle teardown: a stopped
// recorder writes nothing, so a dismounted capsule's leftover observer
// costs one atomic read per event.
func TestStopSilencesRecording(t *testing.T) {
	rec, ring, _ := newTestRecorder(4)
	rec.Stop()
	rec.Observe("s1", core.AgentStart{Type: "agent_start"})
	if got := ring.Len(); got != 0 {
		t.Fatalf("stopped recorder wrote %d rows, want 0", got)
	}
	rec.Start()
	rec.Observe("s1", core.AgentStart{Type: "agent_start"})
	if got := ring.Len(); got != 1 {
		t.Fatalf("re-armed recorder wrote %d rows, want 1", got)
	}
}

// TestPluginMountsRoutesAndObserver pins the mount surface end to end:
// both routes registered, one spine subscription installed, events
// flowing through the Context's own snapshot into the ring, and the
// dismount taking the routes away (the journal) while the observer goes
// inert (the lifecycle Stop).
func TestPluginMountsRoutesAndObserver(t *testing.T) {
	ctx := &core.Context{}
	p := NewPlugin()
	reg, err := agent.MountAllAPI([]agent.Plugin{p}, ctx, nil, nil)
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	var patterns []string
	for _, rt := range ctx.ModuleRoutes {
		patterns = append(patterns, rt.Pattern)
	}
	want := []string{"GET /observability/events", "GET /observability/logs"}
	if strings.Join(patterns, ",") != strings.Join(want, ",") {
		t.Fatalf("routes = %v, want %v", patterns, want)
	}
	if got := len(ctx.RunGroup().Snapshot()); got != 1 {
		t.Fatalf("lifecycle handles = %d, want 1 (the ring)", got)
	}
	subs := ctx.SessionEventObservers()
	if len(subs) != 1 {
		t.Fatalf("session observers = %d, want 1 (the recorder)", len(subs))
	}
	// The host's boot sequence, in order: mount, StartAll arms the fence
	// handles, then work happens. The recorder starts ON at construction,
	// so this arm is about teardown - StopAll (from Dismount) only stops
	// handles it started.
	if err := ctx.RunGroup().StartAll(context.Background(), core.StartDeps{}, ""); err != nil {
		t.Fatalf("start handles: %v", err)
	}
	// The Context's own snapshot feeds THIS capsule's ring.
	subs[0]("sess-mount", core.AgentStart{Type: "agent_start", ModelName: "m"})
	if got := p.ring.Page("sess-mount", "", 10); len(got) != 1 {
		t.Fatalf("ring rows after one observed event = %d, want 1", len(got))
	}

	if err := agent.DismountAll(reg, ctx); err != nil {
		t.Fatalf("dismount: %v", err)
	}
	if len(ctx.ModuleRoutes) != 0 {
		t.Fatalf("routes survived the dismount: %v", ctx.ModuleRoutes)
	}
	if got := len(ctx.RunGroup().Snapshot()); got != 0 {
		t.Fatalf("lifecycle handles survived the dismount: %d", got)
	}
	// The leftover observer is inert: the Stop flipped the recorder off.
	rec := p.rec
	rec.Observe("sess-mount", core.AgentStart{Type: "agent_start"})
	if got := p.ring.Len(); got != 1 {
		t.Fatalf("dismounted recorder still wrote (%d rows, want 1) - the Stop did not silence it", got)
	}
}

// TestNoTelemetryDependency is the card's no-export proof: this capsule
// imports no telemetry library, and the repository carries no OTLP or
// exporter module. It parses real imports rather than trusting comments:
// a future exporter cannot land here without turning this test red.
func TestNoTelemetryDependency(t *testing.T) {
	forbidden := []string{"otel", "opentelemetry", "otlp", "jaeger", "zipkin", "prometheus"}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".go" {
			continue
		}
		f, perr := parser.ParseFile(fset, e.Name(), nil, parser.ImportsOnly)
		if perr != nil {
			t.Fatalf("parse %s: %v", e.Name(), perr)
		}
		for _, imp := range f.Imports {
			path := strings.ToLower(strings.Trim(imp.Path.Value, `"`))
			for _, bad := range forbidden {
				if strings.Contains(path, bad) {
					t.Errorf("%s imports %q - the observability capsule exports nothing (ruling 2)", e.Name(), path)
				}
			}
		}
	}

	mod, rerr := os.ReadFile("../../go.mod")
	if rerr != nil {
		t.Fatalf("read go.mod: %v", rerr)
	}
	for _, line := range strings.Split(string(mod), "\n") {
		if !strings.Contains(line, "/") {
			continue // not a require line
		}
		low := strings.ToLower(line)
		for _, bad := range forbidden {
			if strings.Contains(low, bad) {
				t.Errorf("go.mod carries a telemetry dependency: %q", strings.TrimSpace(line))
			}
		}
	}
}
