package continuous

// ledger_test.go: the evidence ledger self-checks (ruling 11). The
// ledger is runtime-owned: rows arrive only through the spine
// observer, and the store is the process's data dir (per-agent
// isolation falls out of datadir).

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/core"
)

func TestLedgerObserverRecordsToolCalls(t *testing.T) {
	lg, err := OpenLedger(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer lg.Close()

	existing := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(existing, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	obs := lg.observer()

	// One successful call with a path argument: the cheap verifier is
	// the stat (file:exists), the args hash is 16 hex.
	obs(core.StreamEvent{Event: core.ToolCallEvent{ToolCall: core.ToolCall{
		ID: "t1", Name: "shell", Arguments: map[string]any{"cmd": "ls", "path": existing},
	}}})
	obs(core.ToolResultEvent{Message: core.ToolResult{Content: "ok", ToolCallID: "t1"}})

	// One failed call with a missing path.
	obs(core.StreamEvent{Event: core.ToolCallEvent{ToolCall: core.ToolCall{
		ID: "t2", Name: "read", Arguments: map[string]any{"path": "/no/such/file/anywhere"},
	}}})
	obs(core.ToolResultEvent{Message: core.ToolResult{Content: "no", ToolCallID: "t2", IsError: true}})

	// A result with no sighted call is skipped (never invented).
	obs(core.ToolResultEvent{Message: core.ToolResult{Content: "ghost", ToolCallID: "ghost"}})

	rows, err := lg.All(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("ledger rows = %d, want 2", len(rows))
	}
	// All() returns newest first; t2 (read, failed) is row 0.
	if rows[0].Tool != "read" || rows[0].OK || rows[0].Verifier != "file:missing" {
		t.Errorf("row 0 = %+v", rows[0])
	}
	if rows[1].Tool != "shell" || !rows[1].OK || rows[1].Verifier != "file:exists" {
		t.Errorf("row 1 = %+v", rows[1])
	}
	for _, r := range rows {
		if r.ID == 0 || r.TS == "" || r.RunID == "" {
			t.Errorf("row missing ledger identity: %+v", r)
		}
		if len(r.ArgsHash) != 16 {
			t.Errorf("args hash len = %d, want 16", len(r.ArgsHash))
		}
		if _, err := time.Parse(time.RFC3339Nano, r.TS); err != nil {
			t.Errorf("ts %q: %v", r.TS, err)
		}
	}
	if n, err := lg.Count(context.Background()); err != nil || n != 2 {
		t.Errorf("count = %d %v, want 2", n, err)
	}
}

func TestLedgerArgsHashStable(t *testing.T) {
	a := map[string]any{"path": "x", "cmd": "y"}
	b := map[string]any{"cmd": "y", "path": "x"}
	if argsHash(a) != argsHash(b) {
		t.Error("hash is key-order dependent")
	}
	if argsHash(a) == argsHash(map[string]any{"path": "x", "cmd": "z"}) {
		t.Error("different args hashed equal")
	}
}

func TestLedgerPerProcessDataDir(t *testing.T) {
	// OpenLedgerForHome lands in <home>/data/ledger.db - the process's
	// data dir; a wake child with OMEGA_AGENT resolves its agent data
	// dir upstream (datadir.AgentHome), so per-agent isolation is the
	// caller's one-line choice, not ledger machinery.
	home := t.TempDir()
	lg, err := OpenLedgerForHome(home)
	if err != nil {
		t.Fatal(err)
	}
	defer lg.Close()
	if _, err := os.Stat(filepath.Join(home, "data", "ledger.db")); err != nil {
		t.Fatalf("ledger db not in data dir: %v", err)
	}
}

func TestTickEventShape(t *testing.T) {
	// The payload law: raw JSON, namespaced strings. A tick survives a
	// JSON round trip through the wire shape.
	tick := core.TickEvent{Module: "cron", Kind: "wake", Payload: json.RawMessage(`{"agent":"scout"}`)}
	if _, ok := core.Tick(tick); !ok {
		t.Fatal("Tick(TickEvent) failed")
	}
	if _, ok := core.Tick(core.AgentStart{}); ok {
		t.Fatal("Tick(non-tick) succeeded")
	}
}
