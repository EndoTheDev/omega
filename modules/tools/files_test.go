package tools

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// TestFilesToolRegisteredAsDomainTool pins the consolidation: the
// provider serves one "files" domain tool with Actions metadata; the
// legacy names survive only as AliasOf-marked shims.
func TestFilesToolRegisteredAsDomainTool(t *testing.T) {
	p := &ToolProvider{}
	tools := p.Tools()

	files, ok := tools["files"]
	if !ok {
		t.Fatal("files domain tool not registered")
	}
	want := fileActionNames()
	if want[0] != "read" || want[1] != "write" || want[2] != "edit" {
		t.Fatalf("actions metadata wrong: %+v", files.Actions)
	}
	domainNames := core.Tool{Actions: files.Actions}.ActionNames()
	if !reflect.DeepEqual(domainNames, fileActionNames()) {
		t.Fatalf("action names %v != metadata names %v", domainNames, fileActionNames())
	}
	// The schema enum derives from the metadata.
	enum := files.SchemaParameters()["properties"].(map[string]any)["action"].(map[string]any)["enum"].([]string)
	if !reflect.DeepEqual(enum, fileActionNames()) {
		t.Fatalf("schema enum %v not derived from Actions", enum)
	}

	for _, legacy := range []string{"files.read", "files.write", "files.edit"} {
		alias, ok := tools[legacy]
		if !ok {
			t.Fatalf("legacy alias %s missing", legacy)
		}
		if alias.AliasOf != "files" {
			t.Fatalf("%s not marked AliasOf=files: %q", legacy, alias.AliasOf)
		}
	}
}

// TestRunFilesActionEnumCoversAllVerbs is the consolidation gate: every
// pre-fold verb's behavior is reachable through the folded tool's
// action enum.
func TestRunFilesActionEnumCoversAllVerbs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")

	p := &ToolProvider{}

	// write
	_, err := p.RunFiles(context.Background(), map[string]any{"action": "write", "path": path, "content": "alpha\nbeta\n"})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	// read
	out, err := p.RunFiles(context.Background(), map[string]any{"action": "read", "path": path})
	if err != nil || !strings.Contains(out, "1|alpha") {
		t.Fatalf("read: %q %v", out, err)
	}
	// edit
	_, err = p.RunFiles(context.Background(), map[string]any{"action": "edit", "path": path, "old_string": "beta", "new_string": "gamma"})
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	out, err = p.RunFiles(context.Background(), map[string]any{"action": "read", "path": path})
	if err != nil || !strings.Contains(out, "gamma") {
		t.Fatalf("read after edit: %q %v", out, err)
	}
	// unknown / empty
	if _, err := p.RunFiles(context.Background(), map[string]any{}); err == nil || !strings.Contains(err.Error(), "action is required") {
		t.Fatalf("empty action: %v", err)
	}
	if _, err := p.RunFiles(context.Background(), map[string]any{"action": "delete"}); err == nil || !strings.Contains(err.Error(), "unknown action") {
		t.Fatalf("unknown action: %v", err)
	}
}

// TestRunFilesAliasesCallDomainTool is the compat gate: the old tool
// names still work for the model, routed through the folded handler.
func TestRunFilesAliasesCallDomainTool(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")

	p := &ToolProvider{}
	tools := p.Tools()

	if _, err := tools["files.write"].Run(context.Background(), map[string]any{"path": path, "content": "hello\n"}); err != nil {
		t.Fatalf("files.write alias: %v", err)
	}
	out, err := tools["files.read"].Run(context.Background(), map[string]any{"path": path})
	if err != nil || !strings.Contains(out, "hello") {
		t.Fatalf("files.read alias: %q %v", out, err)
	}
	if _, err := tools["files.edit"].Run(context.Background(), map[string]any{"path": path, "old_string": "hello", "new_string": "goodbye"}); err != nil {
		t.Fatalf("files.edit alias: %v", err)
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

	p := &ToolProvider{}
	ops := []map[string]any{
		{"action": "write", "path": fresh, "content": "new bytes\n"},
		// Second op fails on its own terms: edit old_string not found.
		{"action": "edit", "path": existing, "old_string": "nope", "new_string": "x"},
	}
	_, err := p.RunFiles(context.Background(), map[string]any{"operations": ops})
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

	p := &ToolProvider{}
	out, err := p.RunFiles(context.Background(), map[string]any{"operations": []map[string]any{
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
	p := &ToolProvider{}
	_, err := p.RunFiles(context.Background(), map[string]any{"operations": []map[string]any{}})
	if err == nil {
		t.Fatal("empty operations accepted")
	}
	_, err = p.RunFiles(context.Background(), map[string]any{"operations": []map[string]any{{"action": "read", "path": "/tmp/x"}}})
	if err == nil || !strings.Contains(err.Error(), "batch actions are write and edit") {
		t.Fatalf("read op accepted into batch: %v", err)
	}
	_, err = p.RunFiles(context.Background(), map[string]any{"operations": []map[string]any{{"action": "write"}}})
	if err == nil || !strings.Contains(err.Error(), "path is required") {
		t.Fatalf("missing path accepted: %v", err)
	}
}