package files

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestDottedWirePin pins the split's registry shape (t_d8fbad57):
// exactly the four dotted files tools, the flat `files` tool and its
// alias shims gone, each required list its own and no action property
// anywhere - the verb IS the tool name.
func TestDottedWirePin(t *testing.T) {
	p := &FileProvider{}
	tools := p.Tools()
	want := []string{"files.read", "files.write", "files.edit", "files.batch"}
	if len(tools) != len(want) {
		t.Fatalf("provider ships %d tools, want %v: %v", len(tools), want, tools)
	}
	for _, name := range want {
		tl, ok := tools[name]
		if !ok {
			t.Fatalf("%s missing from the provider map", name)
		}
		if len(tl.Actions) != 0 {
			t.Fatalf("%s carries Actions %v - the verb is the name, no action protocol", name, tl.Actions)
		}
		if tl.AliasOf != "" {
			t.Fatalf("%s is still an AliasOf=%q shim - the split made it a real tool", name, tl.AliasOf)
		}
		props := tl.Parameters["properties"].(map[string]any)
		if _, has := props["action"]; has {
			t.Fatalf("%s schema carries an action property - the verb IS the name", name)
		}
	}
	if _, has := tools["files"]; has {
		t.Fatal("the flat `files` tool must be gone (registry unification phase 2)")
	}
	// each verb's required list is exactly its own
	req := func(name string) []string {
		r, _ := tools[name].Parameters["required"].([]string)
		return r
	}
	if !reflect.DeepEqual(req("files.read"), []string{"path"}) {
		t.Fatalf("files.read required = %v, want [path]", req("files.read"))
	}
	if !reflect.DeepEqual(req("files.write"), []string{"path", "content"}) {
		t.Fatalf("files.write required = %v, want [path content]", req("files.write"))
	}
	if !reflect.DeepEqual(req("files.edit"), []string{"path", "old_string", "new_string"}) {
		t.Fatalf("files.edit required = %v, want [path old_string new_string]", req("files.edit"))
	}
	if !reflect.DeepEqual(req("files.batch"), []string{"operations"}) {
		t.Fatalf("files.batch required = %v, want [operations]", req("files.batch"))
	}
}

// TestDottedVerbsCoverAllBehaviors is the reachability gate: every
// pre-split verb's behavior is reachable through its own dotted tool.
func TestDottedVerbsCoverAllBehaviors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")

	p := &FileProvider{}
	tools := p.Tools()

	// write
	if _, err := tools["files.write"].Run(context.Background(), map[string]any{"path": path, "content": "alpha\nbeta\n"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	// read
	out, err := tools["files.read"].Run(context.Background(), map[string]any{"path": path})
	if err != nil || !strings.Contains(out, "1|alpha") {
		t.Fatalf("read: %q %v", out, err)
	}
	// edit
	if _, err := tools["files.edit"].Run(context.Background(), map[string]any{"path": path, "old_string": "beta", "new_string": "gamma"}); err != nil {
		t.Fatalf("edit: %v", err)
	}
	out, err = tools["files.read"].Run(context.Background(), map[string]any{"path": path})
	if err != nil || !strings.Contains(out, "gamma") {
		t.Fatalf("read after edit: %q %v", out, err)
	}
}

// TestRunFilesBatchAtomicRollback is the atomicity gate: a batch whose
// second write fails leaves the first write rolled back. Multi-file
// writes are truly atomic - a half-applied batch is impossible.
func TestRunFilesBatchAtomicRollback(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "exists.txt")
	if err := os.WriteFile(existing, []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fresh := filepath.Join(dir, "fresh.txt")

	p := &FileProvider{}
	ops := []map[string]any{
		{"action": "write", "path": fresh, "content": "new bytes\n"},
		// Second op fails on its own terms: edit old_string not found.
		{"action": "edit", "path": existing, "old_string": "nope", "new_string": "x"},
	}
	_, err := p.Tools()["files.batch"].Run(context.Background(), map[string]any{"operations": ops})
	if err == nil || !strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("expected rollback error, got %v", err)
	}
	// The fresh file must not exist (creation rolled back).
	if _, statErr := os.Stat(fresh); !os.IsNotExist(statErr) {
		t.Fatalf("fresh.txt survived a failed batch: %v", statErr)
	}
	// The existing file must hold its original bytes.
	data, _ := os.ReadFile(existing)
	if string(data) != "original\n" {
		t.Fatalf("existing file not restored: %q", string(data))
	}
}

// TestRunFilesBatchSuccess covers the happy path: both ops apply and
// the result summarises each.
func TestRunFilesBatchSuccess(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")

	p := &FileProvider{}
	out, err := p.Tools()["files.batch"].Run(context.Background(), map[string]any{"operations": []map[string]any{
		{"action": "write", "path": a, "content": "A\n"},
		{"action": "write", "path": b, "content": "B\n"},
	}})
	if err != nil {
		t.Fatalf("batch: %v", err)
	}
	if !strings.Contains(out, "2 operations applied atomically") {
		t.Fatalf("summary missing: %q", out)
	}
	for _, f := range []string{a, b} {
		if _, statErr := os.Stat(f); statErr != nil {
			t.Fatalf("%s missing after batch: %v", f, statErr)
		}
	}
}

// TestRunFilesBatchValidation covers op-shape errors before any write.
func TestRunFilesBatchValidation(t *testing.T) {
	p := &FileProvider{}
	batch := func(args map[string]any) error {
		_, err := p.Tools()["files.batch"].Run(context.Background(), args)
		return err
	}
	if err := batch(map[string]any{"operations": []map[string]any{}}); err == nil {
		t.Fatal("empty operations accepted")
	}
	if err := batch(map[string]any{"operations": []map[string]any{{"action": "read", "path": "/tmp/x"}}}); err == nil || !strings.Contains(err.Error(), "batch actions are write and edit") {
		t.Fatalf("read op accepted into batch: %v", err)
	}
	if err := batch(map[string]any{"operations": []map[string]any{{"action": "write"}}}); err == nil || !strings.Contains(err.Error(), "path is required") {
		t.Fatalf("missing path accepted: %v", err)
	}
}
