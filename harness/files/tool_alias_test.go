package files

// Regression pins for card t_b9f74bea, carried onto the dotted wire
// (t_d8fbad57): the pre-split shims went schema-less, models emitted
// mistyped args (live repro 2026-11-07: the wire carried "limit":"5"
// as a string and the handler refused it with an error that read as
// a lie), and nothing validated the type first. The dotted files.read
// declares the typed schema and the read path accepts a numeric
// string so an already-mistyped argument reads rather than lies.

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestReadDeclaresTypedParamSchema pins the schema contract: the
// dotted read tool carries its own params with integer offset/limit
// (the t_b9f74bea drift fix - a schema-less tool gives the model no
// type contract).
func TestReadDeclaresTypedParamSchema(t *testing.T) {
	p := &FileProvider{}
	tools := p.Tools()
	read, ok := tools["files.read"]
	if !ok {
		t.Fatal("files.read missing from the provider map")
	}
	props, ok := read.Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatal("files.read has no properties schema")
	}
	for _, param := range []string{"path", "offset", "limit"} {
		if _, ok := props[param]; !ok {
			t.Fatalf("files.read schema is missing param %q", param)
		}
	}
	for _, param := range []string{"offset", "limit"} {
		if typ, _ := props[param].(map[string]any)["type"]; typ != "integer" {
			t.Fatalf("files.read param %q type = %q, want integer", param, typ)
		}
	}
}

// TestReadWithLimitAndOffsetWindowsExactly is the acceptance probe:
// files.read with limit + offset returns the exact window.
func TestReadWithLimitAndOffsetWindowsExactly(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/f.txt"
	var sb strings.Builder
	for i := 1; i <= 120; i++ {
		fmt.Fprintf(&sb, "line %d\n", i)
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	p := &FileProvider{}

	out, err := p.Tools()["files.read"].Run(context.Background(), map[string]any{
		"path": path, "offset": 11, "limit": 60,
	})
	if err != nil {
		t.Fatalf("read with offset+limit: %v", err)
	}
	if !strings.Contains(out, "11|line 11") || !strings.Contains(out, "70|line 70") || strings.Contains(out, "71|") {
		t.Fatalf("offset+limit window wrong: %q", out)
	}
}

// TestReadAcceptsStringifiedLimit pins the convenience half: the
// tool accepts the transcript's mistyped shape ("limit":"35" was the
// recorded wire form) and parses it; a non-numeric token still fails
// with an error that names the offending value.
func TestReadAcceptsStringifiedLimit(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/f.txt"
	var sb strings.Builder
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&sb, "line %d\n", i)
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	p := &FileProvider{}

	out, err := p.Tools()["files.read"].Run(context.Background(), map[string]any{
		"path": path, "limit": "5",
	})
	if err != nil || !strings.Contains(out, "5|line 5") || strings.Contains(out, "6|") {
		t.Fatalf("stringified limit: out=%q err=%v", out, err)
	}
	_, err = p.Tools()["files.read"].Run(context.Background(), map[string]any{
		"path": path, "limit": "many",
	})
	if err == nil || !strings.Contains(err.Error(), `"limit" must be an integer, got "many"`) {
		t.Fatalf("non-numeric limit: %v (want an honest rejection)", err)
	}
}
