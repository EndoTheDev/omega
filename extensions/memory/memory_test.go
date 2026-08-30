package memory

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/EndoTheDev/omega/agent"
)

// newTestMemory creates a FileMemory with temp files and small limits.
func newTestMemory(t *testing.T) *FileMemory {
	t.Helper()
	dir := t.TempDir()
	cfg := Config{
		Enabled:              true,
		UserProfileEnabled:   true,
		CharLimit:            200,
		UserProfileCharLimit: 100,
		File:                 filepath.Join(dir, "memory.md"),
		UserProfileFile:      filepath.Join(dir, "user.md"),
	}
	return NewFileMemory(cfg)
}

func TestFileMemoryAddLoad(t *testing.T) {
	fm := newTestMemory(t)

	if _, err := fm.Add(targetMemory, "Project uses Go 1.26."); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := fm.Add(targetMemory, "Build with go build ./..."); err != nil {
		t.Fatalf("Add 2: %v", err)
	}

	entries, err := fm.List(targetMemory)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0] != "Project uses Go 1.26." {
		t.Errorf("entry[0] = %q", entries[0])
	}
	if entries[1] != "Build with go build ./..." {
		t.Errorf("entry[1] = %q", entries[1])
	}

	// Verify file format uses § delimiters.
	data, _ := os.ReadFile(fm.memoryFile)
	if !strings.Contains(string(data), "§") {
		t.Errorf("file does not contain § delimiter: %s", string(data))
	}
}

func TestFileMemoryAddUserTarget(t *testing.T) {
	fm := newTestMemory(t)

	if _, err := fm.Add(targetUser, "Prefers concise responses."); err != nil {
		t.Fatalf("Add user: %v", err)
	}

	entries, err := fm.List(targetUser)
	if err != nil {
		t.Fatalf("List user: %v", err)
	}
	if len(entries) != 1 || entries[0] != "Prefers concise responses." {
		t.Fatalf("unexpected user entries: %v", entries)
	}
}

func TestFileMemoryReplace(t *testing.T) {
	fm := newTestMemory(t)
	fm.Add(targetMemory, "User prefers dark mode in all editors")
	fm.Add(targetMemory, "Project uses Python 3.12")

	if _, err := fm.Replace(targetMemory, "dark mode", "User prefers light mode in VS Code, dark mode in terminal"); err != nil {
		t.Fatalf("Replace: %v", err)
	}

	entries, _ := fm.List(targetMemory)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if !strings.Contains(entries[0], "light mode") {
		t.Errorf("entry[0] not replaced: %q", entries[0])
	}
	if entries[1] != "Project uses Python 3.12" {
		t.Errorf("entry[1] changed: %q", entries[1])
	}
}

func TestFileMemoryReplaceAmbiguous(t *testing.T) {
	fm := newTestMemory(t)
	fm.Add(targetMemory, "User likes Go")
	fm.Add(targetMemory, "User likes Go a lot")

	_, err := fm.Replace(targetMemory, "User likes Go", "User loves Go")
	if err == nil {
		t.Fatal("expected ambiguous match error")
	}
	if !strings.Contains(err.Error(), "multiple entries") {
		t.Errorf("expected 'multiple entries' error, got: %v", err)
	}
}

func TestFileMemoryReplaceNotFound(t *testing.T) {
	fm := newTestMemory(t)
	fm.Add(targetMemory, "Some fact")

	_, err := fm.Replace(targetMemory, "nonexistent", "new fact")
	if err == nil {
		t.Fatal("expected not found error")
	}
	if !strings.Contains(err.Error(), "no entry matching") {
		t.Errorf("expected 'no entry matching' error, got: %v", err)
	}
}

func TestFileMemoryRemove(t *testing.T) {
	fm := newTestMemory(t)
	fm.Add(targetMemory, "Keep this")
	fm.Add(targetMemory, "Delete this")
	fm.Add(targetMemory, "Also keep")

	if _, err := fm.Remove(targetMemory, "Delete this"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	entries, _ := fm.List(targetMemory)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0] != "Keep this" || entries[1] != "Also keep" {
		t.Errorf("unexpected entries: %v", entries)
	}
}

func TestFileMemoryCharLimit(t *testing.T) {
	fm := newTestMemory(t) // limit 200 chars

	// Fill up close to the limit.
	long := strings.Repeat("x", 180)
	if _, err := fm.Add(targetMemory, long); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// This entry should exceed.
	_, err := fm.Add(targetMemory, strings.Repeat("y", 50))
	if err == nil {
		t.Fatal("expected char limit error")
	}
	if !strings.Contains(err.Error(), "exceed the limit") {
		t.Errorf("expected 'exceed the limit' error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "Consolidate") {
		t.Errorf("expected error to mention Consolidate, got: %v", err)
	}
}

func TestFileMemoryDuplicate(t *testing.T) {
	fm := newTestMemory(t)
	fm.Add(targetMemory, "User prefers Go")

	usage, err := fm.Add(targetMemory, "User prefers Go")
	if err != nil {
		t.Fatalf("Add duplicate should not error: %v", err)
	}
	if !strings.Contains(usage, "no duplicate") {
		t.Errorf("expected 'no duplicate' message, got: %q", usage)
	}

	// Verify only 1 entry.
	entries, _ := fm.List(targetMemory)
	if len(entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(entries))
	}
}

func TestFileMemoryBatch(t *testing.T) {
	fm := newTestMemory(t)
	tp := &memoryToolProvider{mem: fm}

	// Batch: add two, remove one.
	result := tp.runBatch([]memoryOp{
		{Action: "add", Target: targetMemory, Content: "First entry"},
		{Action: "add", Target: targetMemory, Content: "Second entry"},
		{Action: "remove", Target: targetMemory, OldText: "First"},
	})

	var res memoryResult
	if err := json.Unmarshal([]byte(result), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !res.Success {
		t.Fatalf("batch failed: %s", res.Error)
	}

	entries, _ := fm.List(targetMemory)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry after batch, got %d", len(entries))
	}
	if entries[0] != "Second entry" {
		t.Errorf("expected 'Second entry', got %q", entries[0])
	}
}

func TestFileMemorySnapshot(t *testing.T) {
	fm := newTestMemory(t)
	fm.Add(targetMemory, "Test fact one")
	fm.Add(targetMemory, "Test fact two")
	fm.Add(targetUser, "User preference")

	snap := fm.Snapshot()

	if !strings.Contains(snap, "MEMORY") {
		t.Error("snapshot missing MEMORY section")
	}
	if !strings.Contains(snap, "USER PROFILE") {
		t.Error("snapshot missing USER PROFILE section")
	}
	if !strings.Contains(snap, "Test fact one") {
		t.Error("snapshot missing entry content")
	}
	if !strings.Contains(snap, "chars") {
		t.Error("snapshot missing usage info")
	}
}

func TestFileMemorySnapshotEmpty(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		Enabled:              true,
		UserProfileEnabled:   true,
		CharLimit:            200,
		UserProfileCharLimit: 100,
		File:                 filepath.Join(dir, "memory.md"),
		UserProfileFile:      filepath.Join(dir, "user.md"),
	}
	fm := NewFileMemory(cfg)

	snap := fm.Snapshot()
	if snap != "" {
		t.Errorf("expected empty snapshot for no files, got %q", snap)
	}
}

func TestFileMemorySnapshotDisabledStore(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		Enabled:              true,
		UserProfileEnabled:   false,
		CharLimit:            200,
		UserProfileCharLimit: 100,
		File:                 filepath.Join(dir, "memory.md"),
		UserProfileFile:      filepath.Join(dir, "user.md"),
	}
	fm := NewFileMemory(cfg)
	fm.Add(targetMemory, "Some fact")
	// Also write to user file directly to verify it's not included.
	os.WriteFile(filepath.Join(dir, "user.md"), []byte("Hidden fact\n"), 0644)

	snap := fm.Snapshot()

	if !strings.Contains(snap, "MEMORY") {
		t.Error("expected MEMORY section")
	}
	if strings.Contains(snap, "USER PROFILE") {
		t.Error("did not expect USER PROFILE section when disabled")
	}
	if strings.Contains(snap, "Hidden fact") {
		t.Error("disabled store content leaked into snapshot")
	}
}

func TestFileMemoryAddDisabled(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		Enabled:              false,
		UserProfileEnabled:   true,
		CharLimit:            200,
		UserProfileCharLimit: 100,
		File:                 filepath.Join(dir, "memory.md"),
		UserProfileFile:      filepath.Join(dir, "user.md"),
	}
	fm := NewFileMemory(cfg)

	_, err := fm.Add(targetMemory, "test")
	if err == nil {
		t.Fatal("expected error adding to disabled store")
	}
	if !strings.Contains(err.Error(), "disabled") {
		t.Errorf("expected 'disabled' error, got: %v", err)
	}
}

func TestFileMemoryConcurrentWrite(t *testing.T) {
	fm := newTestMemory(t)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			fm.Add(targetMemory, string(rune('a'+n)))
		}(i)
	}
	wg.Wait()

	entries, _ := fm.List(targetMemory)
	if len(entries) != 10 {
		t.Errorf("expected 10 entries, got %d", len(entries))
	}
}

func TestMemoryToolAdd(t *testing.T) {
	fm := newTestMemory(t)
	tp := &memoryToolProvider{mem: fm}

	result, _ := tp.runMemory(context.Background(), map[string]any{
		"action":  "add",
		"target":  "memory",
		"content": "Test via tool",
	})

	var res memoryResult
	if err := json.Unmarshal([]byte(result), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !res.Success {
		t.Fatalf("tool add failed: %s", res.Error)
	}
	if !strings.Contains(res.Usage, "chars") {
		t.Errorf("expected usage in result, got %q", res.Usage)
	}

	entries, _ := fm.List(targetMemory)
	if len(entries) != 1 || entries[0] != "Test via tool" {
		t.Errorf("unexpected entries: %v", entries)
	}
}

func TestMemoryToolRemove(t *testing.T) {
	fm := newTestMemory(t)
	fm.Add(targetMemory, "Remove me")
	fm.Add(targetMemory, "Keep me")
	tp := &memoryToolProvider{mem: fm}

	result, _ := tp.runMemory(context.Background(), map[string]any{
		"action":  "remove",
		"target":  "memory",
		"old_text": "Remove",
	})

	var res memoryResult
	if err := json.Unmarshal([]byte(result), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !res.Success {
		t.Fatalf("tool remove failed: %s", res.Error)
	}

	entries, _ := fm.List(targetMemory)
	if len(entries) != 1 || entries[0] != "Keep me" {
		t.Errorf("unexpected entries: %v", entries)
	}
}

func TestMemoryToolReplace(t *testing.T) {
	fm := newTestMemory(t)
	fm.Add(targetMemory, "Old value")
	tp := &memoryToolProvider{mem: fm}

	result, _ := tp.runMemory(context.Background(), map[string]any{
		"action":   "replace",
		"target":   "memory",
		"old_text": "Old",
		"content":  "New value",
	})

	var res memoryResult
	if err := json.Unmarshal([]byte(result), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !res.Success {
		t.Fatalf("tool replace failed: %s", res.Error)
	}

	entries, _ := fm.List(targetMemory)
	if len(entries) != 1 || entries[0] != "New value" {
		t.Errorf("unexpected entries: %v", entries)
	}
}

func TestMemoryToolBatch(t *testing.T) {
	fm := newTestMemory(t)
	tp := &memoryToolProvider{mem: fm}

	result, _ := tp.runMemory(context.Background(), map[string]any{
		"operations": []any{
			map[string]any{"action": "add", "target": "memory", "content": "First"},
			map[string]any{"action": "add", "target": "memory", "content": "Second"},
			map[string]any{"action": "remove", "target": "memory", "old_text": "First"},
		},
	})

	var res memoryResult
	if err := json.Unmarshal([]byte(result), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !res.Success {
		t.Fatalf("batch failed: %s", res.Error)
	}

	entries, _ := fm.List(targetMemory)
	if len(entries) != 1 || entries[0] != "Second" {
		t.Errorf("unexpected entries: %v", entries)
	}
}

func TestMemoryToolMissingAction(t *testing.T) {
	fm := newTestMemory(t)
	tp := &memoryToolProvider{mem: fm}

	result, _ := tp.runMemory(context.Background(), map[string]any{
		"target": "memory",
	})

	var res memoryResult
	if err := json.Unmarshal([]byte(result), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if res.Success {
		t.Error("expected failure with missing action")
	}
}

func TestPluginImplementsInterface(t *testing.T) {
	var _ agent.Plugin = (*Plugin)(nil)
}

func TestPluginMetadata(t *testing.T) {
	p := NewPlugin()
	if p.Name() != "memory" {
		t.Errorf("Name() = %q, want %q", p.Name(), "memory")
	}
	provides := p.Provides()
	if len(provides) != 2 {
		t.Fatalf("Provides() = %v, want 2 items", provides)
	}
	if provides[0] != "memory" || provides[1] != "tools" {
		t.Errorf("Provides() = %v, want [memory tools]", provides)
	}
	if len(p.Requires()) != 0 {
		t.Errorf("Requires() = %v, want empty", p.Requires())
	}
}

func TestErrResultAddEmptyContent(t *testing.T) {
	// errResult is exercised when Add/Replace/Remove returns an error.
	// Add with empty content triggers "content is required".
	fm := newTestMemory(t)
	fm.Add(targetMemory, "Existing fact") // seed so CurrentEntries is populated
	tp := &memoryToolProvider{mem: fm}

	result := tp.runSingle("add", targetMemory, "", "")
	var res memoryResult
	if err := json.Unmarshal([]byte(result), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if res.Success {
		t.Error("expected Success=false for empty content add")
	}
	if res.Error == "" {
		t.Error("expected non-empty Error")
	}
	if len(res.CurrentEntries) == 0 {
		t.Error("expected CurrentEntries populated on error")
	}
}

func TestErrResultReplaceNotFound(t *testing.T) {
	// Replace with a non-existent old_text triggers "no entry matching".
	fm := newTestMemory(t)
	fm.Add(targetMemory, "Keep this")
	tp := &memoryToolProvider{mem: fm}

	result := tp.runSingle("replace", targetMemory, "does not exist", "new content")
	var res memoryResult
	if err := json.Unmarshal([]byte(result), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if res.Success {
		t.Error("expected Success=false for replace with non-existent old_text")
	}
	if res.Error == "" {
		t.Error("expected non-empty Error")
	}
	if len(res.CurrentEntries) == 0 {
		t.Error("expected CurrentEntries populated on error")
	}
}

func TestErrResultRemoveEmptyOldText(t *testing.T) {
	// Remove with empty old_text triggers "old_text is required".
	fm := newTestMemory(t)
	fm.Add(targetMemory, "Keep this")
	tp := &memoryToolProvider{mem: fm}

	result := tp.runSingle("remove", targetMemory, "", "")
	var res memoryResult
	if err := json.Unmarshal([]byte(result), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if res.Success {
		t.Error("expected Success=false for remove with empty old_text")
	}
	if res.Error == "" {
		t.Error("expected non-empty Error")
	}
	if len(res.CurrentEntries) == 0 {
		t.Error("expected CurrentEntries populated on error")
	}
}

func TestExecOpUnknownAction(t *testing.T) {
	fm := newTestMemory(t)
	tp := &memoryToolProvider{mem: fm}

	_, err := tp.execOp(memoryOp{Action: "frobnicate", Target: targetMemory})
	if err == nil {
		t.Fatal("expected error for unknown action")
	}
	if !strings.Contains(err.Error(), "unknown action") {
		t.Errorf("expected error containing 'unknown action', got: %v", err)
	}
}

func TestRunSingleUnknownAction(t *testing.T) {
	fm := newTestMemory(t)
	tp := &memoryToolProvider{mem: fm}

	result := tp.runSingle("frobnicate", targetMemory, "content", "")
	var res memoryResult
	if err := json.Unmarshal([]byte(result), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if res.Success {
		t.Error("expected Success=false for unknown action")
	}
	if !strings.Contains(res.Error, "unknown action") {
		t.Errorf("expected Error containing 'unknown action', got: %q", res.Error)
	}
}

func TestRunBatchError(t *testing.T) {
	// Batch where one operation fails (replace with non-existent old_text).
	fm := newTestMemory(t)
	fm.Add(targetMemory, "Existing entry")
	tp := &memoryToolProvider{mem: fm}

	result := tp.runBatch([]memoryOp{
		{Action: "add", Target: targetMemory, Content: "Batch entry"},
		{Action: "replace", Target: targetMemory, OldText: "nonexistent", Content: "fail"},
	})

	var res memoryResult
	if err := json.Unmarshal([]byte(result), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if res.Success {
		t.Error("expected Success=false for batch with failing operation")
	}
	if res.Error == "" {
		t.Error("expected non-empty Error for failed batch")
	}
}

func TestPluginMount(t *testing.T) {
	dir := t.TempDir()
	cfg := Default()
	cfg.File = filepath.Join(dir, "memory.md")
	cfg.UserProfileFile = filepath.Join(dir, "user.md")

	p := NewPlugin()
	ctx := &agent.Context{Configs: map[string]any{"memory": cfg}}
	if err := p.Mount(ctx); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	if ctx.Memory == nil {
		t.Fatal("ctx.Memory is nil after Mount")
	}

	// Verify memory tool is registered.
	tps := ctx.ToolProviders
	if len(tps) != 1 {
		t.Fatalf("expected 1 tool provider, got %d", len(tps))
	}
	tools := tps[0].Tools()
	if _, ok := tools["memory"]; !ok {
		t.Fatal("memory tool not registered")
	}
}
