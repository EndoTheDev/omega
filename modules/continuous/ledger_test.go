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

	obs := lg.observer()
	call := func(id, name string, args map[string]any, isError bool) {
		obs(core.StreamEvent{Event: core.ToolCallEvent{ToolCall: core.ToolCall{
			ID: id, Name: name, Arguments: args,
		}}})
		obs(core.ToolResultEvent{Message: core.ToolResult{
			Content: "seen", ToolCallID: id, IsError: isError,
		}})
	}

	// A successful call whose path exists at RESULT time - the shell
	// row's file was never absent.
	existing := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(existing, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	call("t1", "shell", map[string]any{"cmd": "ls", "path": existing}, false)

	// A failed call on a missing path.
	call("t2", "read", map[string]any{"path": "/no/such/file/anywhere"}, true)

	// A write whose file materializes between call and result: the
	// stat runs at result time, so a creator reads file:exists after.
	created := filepath.Join(t.TempDir(), "fresh.md")
	obs(core.StreamEvent{Event: core.ToolCallEvent{ToolCall: core.ToolCall{
		ID: "t3", Name: "write", Arguments: map[string]any{"path": created},
	}}})
	if _, err := os.Stat(created); err == nil {
		t.Fatal("created must not exist before its result lands")
	}
	if err := os.WriteFile(created, []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}
	obs(core.ToolResultEvent{Message: core.ToolResult{Content: "wrote", ToolCallID: "t3"}})

	// A result with no sighted call is skipped (never invented).
	obs(core.ToolResultEvent{Message: core.ToolResult{Content: "ghost", ToolCallID: "ghost"}})

	// A call whose file is deleted BEFORE its result lands reads
	// missing even though the write succeeds - the end state is truth.
	gone := filepath.Join(t.TempDir(), "gone.txt")
	obs(core.StreamEvent{Event: core.ToolCallEvent{ToolCall: core.ToolCall{
		ID: "t4", Name: "write", Arguments: map[string]any{"path": gone},
	}}})
	if err := os.WriteFile(gone, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(gone)
	obs(core.ToolResultEvent{Message: core.ToolResult{Content: "wrote", ToolCallID: "t4"}})

	rows, err := lg.All(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatalf("ledger rows = %d, want 4", len(rows))
	}
	// All() returns newest first.
	if rows[0].Tool != "write" || !rows[0].OK || rows[0].Verifier != "file:missing:"+gone {
		t.Errorf("row 0 (deleted end state) = %+v", rows[0])
	}
	if rows[1].Tool != "write" || !rows[1].OK || rows[1].Verifier != "file:exists:"+created {
		t.Errorf("row 1 (creator) = %+v", rows[1])
	}
	if rows[2].Tool != "read" || rows[2].OK || rows[2].Verifier != "file:missing:/no/such/file/anywhere" {
		t.Errorf("row 2 = %+v", rows[2])
	}
	if rows[3].Tool != "shell" || !rows[3].OK || rows[3].Verifier != "file:exists:"+existing {
		t.Errorf("row 3 = %+v", rows[3])
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
	if n, err := lg.Count(context.Background()); err != nil || n != 4 {
		t.Errorf("count = %d %v, want 4", n, err)
	}
}

func TestLedgerObserverRecordsURLLocator(t *testing.T) {
	lg, err := OpenLedger(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer lg.Close()
	obs := lg.observer()

	// web.fetch with a url argument: the verifier carries the url
	// LOCATOR (no stat, no fetch - the ok flag is the HTTP outcome).
	mk := func(id string, ok bool) {
		obs(core.StreamEvent{Event: core.ToolCallEvent{ToolCall: core.ToolCall{
			ID: id, Name: "web.fetch", Arguments: map[string]any{"url": "https://example.com/feed"},
		}}})
		obs(core.ToolResultEvent{Message: core.ToolResult{Content: "seen", ToolCallID: id, IsError: !ok}})
	}
	mk("f1", true)
	mk("f2", false)
	// a path-arg call wins the file stat; no url locator leaks in.
	obs(core.StreamEvent{Event: core.ToolCallEvent{ToolCall: core.ToolCall{
		ID: "f3", Name: "web.fetch", Arguments: map[string]any{"url": "https://example.com/a", "file_path": "/tmp/a.bin"},
	}}})
	obs(core.ToolResultEvent{Message: core.ToolResult{Content: "dl", ToolCallID: "f3"}})

	rows, err := lg.All(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}
	if rows[2].Verifier != "url:https://example.com/feed" || !rows[2].OK {
		t.Errorf("ok fetch row = %+v", rows[2])
	}
	if rows[1].Verifier != "url:https://example.com/feed" || rows[1].OK {
		t.Errorf("failed fetch row = %+v", rows[1])
	}
	if rows[0].Verifier != "file:missing:/tmp/a.bin" || !rows[0].OK {
		t.Errorf("path+url row = %+v", rows[0])
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
