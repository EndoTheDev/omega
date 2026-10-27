package continuous

// report_test.go: the wake report's shape self-checks (ruling 11's
// second half). The shaper is a pure function of content + rows; the
// sink's gating (OMEGA_AGENT) and its closing-message rule ride those
// checks. Fail-closed is the asserted direction: no evidence, no claim.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/engine/agent"
)

// row is a ledger fixture helper.
func row(id int64, ok bool, verifier string) record {
	return record{ID: id, RunID: spawnRunID, Tool: "shell", ArgsHash: "0123456789abcdef", OK: ok, Verifier: verifier}
}

func TestShapeReportCitesVerifiedClaim(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(log, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	rows := []record{
		row(1, true, "cmd:ls"),           // the call carried no path
		row(2, true, "file:exists:"+log), // the claim's backing row
		row(3, false, "file:missing:/no/such/other.txt"),
	}

	out := shapeReport("I created notes.md and moved on.", rows)
	if !strings.Contains(out, "I created notes.md") {
		t.Errorf("verified claim stripped: %q", out)
	}
	if !strings.Contains(out, "[ledger #2]") {
		t.Errorf("citation missing: %q", out)
	}
	if !strings.Contains(out, "_Evidence (runtime ledger): 3 tool calls, 2 ok") {
		t.Errorf("evidence tail missing: %q", out)
	}
}

func TestShapeReportStripsUnbackedClaim(t *testing.T) {
	rows := []record{row(7, true, "file:exists:/tmp/other.txt")}
	// A checkable object with no ledger row strips; a claim verb over
	// a non-statable noun ("the config") cannot be cheaply verified
	// either way, so it stays - the ruling's verifier is the file stat.
	out := shapeReport("I updated the config. Nothing else happened.", rows)
	if !strings.Contains(out, "I updated the config") {
		t.Errorf("verbless-object claim stripped: %q", out)
	}
	out = shapeReport("I updated the config.yaml. Nothing else happened.", rows)
	if strings.Contains(out, "config.yaml") {
		t.Errorf("unbacked path claim survived: %q", out)
	}
	if !strings.Contains(out, "Nothing else happened.") {
		t.Errorf("narrative lost: %q", out)
	}
}

func TestShapeReportStripsContradictedClaim(t *testing.T) {
	rows := []record{row(9, false, "file:missing:/etc/omega/hotfix.yaml")}
	out := shapeReport("Wrote /etc/omega/hotfix.yaml successfully.", rows)
	if strings.Contains(out, "hotfix.yaml") {
		t.Errorf("contradicted claim survived: %q", out)
	}
}

func TestShapeReportKeepsVerblessNarrative(t *testing.T) {
	out := shapeReport("The queue was quiet today. v0.12.0 is the pinned release.", nil)
	if !strings.Contains(out, "The queue was quiet today.") {
		t.Errorf("verbless sentence stripped: %q", out)
	}
}

func TestShapeReportNoToolsNoTailNoClaim(t *testing.T) {
	out := shapeReport("I created report.md as asked.", nil)
	if strings.Contains(out, "_Evidence") {
		t.Errorf("tail on a tool-free run: %q", out)
	}
	if strings.Contains(out, "created report.md") {
		t.Errorf("unbacked claim survived: %q", out)
	}
}

func TestShapeReportAllStrippedLeavesStamp(t *testing.T) {
	rows := []record{row(4, true, "file:exists:/tmp/elsewhere.txt")}
	out := shapeReport("Fetched https://example.invalid/feed and saved it.", rows)
	if !strings.Contains(out, "[report body stripped: no claim carried ledger evidence]") {
		t.Errorf("empty shape left no honest stamp: %q", out)
	}
	if strings.Contains(out, "https://") {
		t.Errorf("unbackable URL claim survived: %q", out)
	}
}

func TestShapeReportPreservesParagraphs(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "a.log")
	if err := os.WriteFile(log, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	rows := []record{row(5, true, "file:exists:"+log)}
	content := "Morning run.\n\nI created a.log at " + log + "\nDone."
	out := shapeReport(content, rows)
	if !strings.HasPrefix(out, "Morning run.\n\n") {
		t.Errorf("paragraph break lost: %q", out)
	}
}

func TestWakeReportSinkShapesOnlyClosing(t *testing.T) {
	home := t.TempDir()
	if err := agent.ScaffoldAgent(home, "scout", ""); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OMEGA_HOME", home)
	t.Setenv("OMEGA_AGENT", "scout")
	data := filepath.Join(agent.AgentDir(home, "scout"), "data")
	lg, err := OpenLedger(data)
	if err != nil {
		t.Fatal(err)
	}
	notes := filepath.Join(data, "notes.md")
	if err := os.WriteFile(notes, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := lg.append(record{TS: core.NowISO(), RunID: spawnRunID, Tool: "write",
		ArgsHash: "0123456789abcdef", OK: true, Verifier: "file:exists:" + notes}); err != nil {
		t.Fatal(err)
	}
	defer lg.Close()

	var got []core.Message
	inner := &captureSink{messages: &got}
	sink := &wakeReportSink{inner: inner}
	ctx := context.Background()

	mid := core.NewAssistant("Working on it now.")
	mid.ToolCalls = []core.ToolCall{{ID: "t1", Name: "write", Arguments: map[string]any{"path": notes}}}
	if err := sink.AppendTurnMessage(ctx, mid); err != nil {
		t.Fatal(err)
	}
	if err := sink.AppendTurnMessage(ctx, core.ToolResult{Content: "ok", ToolCallID: "t1"}); err != nil {
		t.Fatal(err)
	}
	closing := core.NewAssistant("I created notes.md in the data dir. I also created ghost.md.")
	if err := sink.AppendTurnMessage(ctx, closing); err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("sink saw %d messages, want 3", len(got))
	}
	if last, ok := got[2].(core.Assistant); !ok {
		t.Fatalf("closing message type: %T", got[2])
	} else {
		if !strings.Contains(last.Content, "[ledger #1]") {
			t.Errorf("closing message missing citation: %q", last.Content)
		}
		if strings.Contains(last.Content, "ghost.md") {
			t.Errorf("unbacked claim survived the sink: %q", last.Content)
		}
		if strings.Contains(last.Content, "notes.md") && !strings.Contains(last.Content, "[ledger #1]") {
			t.Errorf("claim kept without citation: %q", last.Content)
		}
	}
	// the intermediate assistant persisted exactly as produced
	if a, ok := got[0].(core.Assistant); !ok || a.Content != "Working on it now." || len(a.ToolCalls) != 1 {
		t.Errorf("intermediate message was reshaped: %+v", got[0])
	}
}

// captureSink collects persisted messages in order.
type captureSink struct {
	messages *[]core.Message
}

func (c *captureSink) AppendTurnMessage(_ context.Context, msg core.Message) error {
	*c.messages = append(*c.messages, msg)
	return nil
}

func TestWrapWakeSinkGatesOnAgentEnv(t *testing.T) {
	// No OMEGA_AGENT: the sink comes back untouched - a manual run is
	// never reshaped.
	inner := &captureSink{}
	if WrapWakeSink(inner) != agent.TurnSink(inner) {
		t.Fatal("WrapWakeSink reshaped a main-process run")
	}
	// Main named explicitly ("default" runs as root): untouched too.
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	t.Setenv("OMEGA_AGENT", "default")
	if WrapWakeSink(inner) != agent.TurnSink(inner) {
		t.Fatal("WrapWakeSink reshaped main")
	}
	// A continuous agent profile: shaped.
	if err := agent.ScaffoldAgent(home, "scout", ""); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OMEGA_AGENT", "scout")
	if WrapWakeSink(inner) == agent.TurnSink(inner) {
		t.Fatal("WrapWakeSink left the wake child unshaped")
	}
}

func TestEvidenceSetsFoldsRows(t *testing.T) {
	ev := evidenceSets([]record{
		row(1, true, "file:exists:/tmp/a.txt"),
		row(2, false, "file:missing:/tmp/b.txt"),
		row(3, true, "cmd:ls"),
		row(4, false, "file:exists:/tmp/c.txt"),
	})
	if len(ev.fileVerified["/tmp/a.txt"]) != 1 || ev.fileVerified["/tmp/a.txt"][0] != 1 {
		t.Errorf("verified table: %v", ev.fileVerified)
	}
	if !ev.fileFailed["/tmp/b.txt"] || !ev.fileFailed["/tmp/c.txt"] {
		t.Errorf("failed table: %v", ev.fileFailed)
	}
	if !ev.fileSeen["/tmp/a.txt"] || !ev.fileSeen["/tmp/b.txt"] || ev.fileSeen["x"] {
		t.Errorf("seen table: %v", ev.fileSeen)
	}
}

func TestURLClaimSurvivesWhenTheFetchVerifiedIt(t *testing.T) {
	rows := []record{
		row(11, true, "cmd:curl"), // no object, no locator
		row(12, true, "url:https://example.com/feed"),
	}
	out := shapeReport("I fetched https://example.com/feed and read it.", rows)
	if !strings.Contains(out, "I fetched https://example.com/feed") {
		t.Errorf("verified URL claim stripped: %q", out)
	}
	if !strings.Contains(out, "[ledger #12]") {
		t.Errorf("URL citation missing: %q", out)
	}
}

func TestURLClaimStripsWhenTheFetchFailed(t *testing.T) {
	rows := []record{row(13, false, "url:https://example.com/feed")}
	out := shapeReport("I fetched https://example.com/feed and read it.", rows)
	if strings.Contains(out, "example.com") {
		t.Errorf("contradicted URL claim survived: %q", out)
	}
}

func TestURLClaimStripsWhenTheRunNeverFetchedIt(t *testing.T) {
	rows := []record{row(14, true, "url:https://other.example/article")}
	out := shapeReport("I fetched https://example.com/article and read it.", rows)
	if strings.Contains(out, "example.com") {
		t.Errorf("unfetched URL claim survived: %q", out)
	}
}

func TestURLTrailingSlashAgrees(t *testing.T) {
	rows := []record{row(15, true, "url:https://example.com/guide/")}
	out := shapeReport("I fetched https://example.com/guide without trouble.", rows)
	if !strings.Contains(out, "[ledger #15]") {
		t.Errorf("trailing-slash URL claim stripped: %q", out)
	}
}

func TestURLLocatorWithoutCheckStaysOutOfFileTables(t *testing.T) {
	ev := evidenceSets([]record{
		row(16, true, "url:https://example.com/feed"),
		row(17, false, "url:https://example.com/dead"),
	})
	if ev.fileSeen["https://example.com/feed"] || ev.fileFailed["https://example.com/dead"] {
		t.Errorf("url rows leaked into the file tables: %+v", ev)
	}
	if len(ev.urlVerified["https://example.com/feed"]) != 1 {
		t.Errorf("ok fetch row not verified: %v", ev.urlVerified)
	}
	if !ev.urlFailed["https://example.com/dead"] {
		t.Errorf("failed fetch row not failed: %v", ev.urlFailed)
	}
}

func TestMixedClaimStripsUntilEveryTokenIsBacked(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "feed.md")
	if err := os.WriteFile(log, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// url ok + file missing: the whole claim fails closed
	rows := []record{
		row(18, true, "url:https://example.com/feed"),
		row(19, false, "file:missing:"+log),
	}
	out := shapeReport("I fetched https://example.com/feed and wrote it to "+filepath.Base(log)+".", rows)
	if strings.Contains(out, "[ledger") {
		t.Errorf("half-backed claim got citations: %q", out)
	}
	if strings.Contains(out, filepath.Base(log)) {
		t.Errorf("half-backed claim survived: %q", out)
	}
	rows = []record{
		row(18, true, "url:https://example.com/feed"),
		row(20, true, "file:exists:"+log),
	}
	out = shapeReport("I fetched https://example.com/feed and wrote it to "+filepath.Base(log)+".", rows)
	if !strings.Contains(out, "[ledger #18, #20]") {
		t.Errorf("fully-backed mixed claim uncited: %q", out)
	}
}
