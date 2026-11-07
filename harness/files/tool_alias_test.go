package files

// Regression pins for card t_b9f74bea: the legacy alias shims went
// schema-less, models emitted mistyped args (live repro 2026-11-07:
// the wire carried "limit":"5" as a string and the handler refused it
// with an error that read as a lie), and nothing validated the type
// first. The shims now declare their action's parameter schema, and
// the read path accepts a numeric string so an already-mistyped
// argument reads rather than lies.

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
)

// TestAliasShimsDeclareActionParamSchemas pins the schema contract:
// every alias carries its action's params (path plus the action's
// verb-specific args), the action property pinned to its single verb.
func TestAliasShimsDeclareActionParamSchemas(t *testing.T) {
	p := &FileProvider{}
	tools := p.Tools()
	wantProps := map[string]map[string]bool{
		"files.read":  {"action": true, "path": true, "offset": true, "limit": true},
		"files.write": {"action": true, "path": true, "content": true},
		"files.edit":  {"action": true, "path": true, "old_string": true, "new_string": true},
	}
	for name, wants := range wantProps {
		alias, ok := tools[name]
		if !ok {
			t.Fatalf("alias %s missing from the provider map", name)
		}
		props, ok := alias.Parameters["properties"].(map[string]any)
		if !ok {
			t.Fatalf("alias %s has no properties schema", name)
		}
		for prop := range wants {
			if _, ok := props[prop]; !ok {
				t.Fatalf("alias %s schema is missing param %q", name, prop)
			}
			if prop == "offset" || prop == "limit" {
				if typ, _ := props[prop].(map[string]any)["type"]; typ != "integer" {
					t.Fatalf("alias %s param %q type = %q, want integer", name, prop, typ)
				}
			}
		}
	}
	// The write/edit aliases require their payload params; read only
	// requires the path.
	for name, required := range map[string][]string{
		"files.read":  {"path"},
		"files.write": {"path", "content"},
		"files.edit":  {"path", "old_string", "new_string"},
	} {
		got, _ := tools[name].Parameters["required"].([]string)
		if !reflect.DeepEqual(got, required) {
			t.Fatalf("alias %s required = %v, want %v", name, got, required)
		}
	}
	// The action property is pinned to the shim's single verb.
	readAction, _ := tools["files.read"].Parameters["properties"].(map[string]any)["action"].(map[string]any)
	if enum, _ := readAction["enum"].([]string); len(enum) != 1 || enum[0] != "read" {
		t.Fatalf("files.read action enum = %v, want [read]", enum)
	}
}

// TestAliasReadWithLimitAndOffsetMatchesCore is the acceptance probe:
// the alias with limit + offset returns EXACTLY the content the core
// tool returns for the same call.
func TestAliasReadWithLimitAndOffsetMatchesCore(t *testing.T) {
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
	tools := p.Tools()

	coreOut, err := tools["files"].Run(context.Background(), map[string]any{
		"action": "read", "path": path, "offset": 11, "limit": 60,
	})
	if err != nil {
		t.Fatalf("core tool read: %v", err)
	}
	aliasOut, err := tools["files.read"].Run(context.Background(), map[string]any{
		"path": path, "offset": 11, "limit": 60,
	})
	if err != nil {
		t.Fatalf("alias read with offset+limit: %v", err)
	}
	if aliasOut != coreOut {
		t.Fatalf("alias output (%d bytes) != core output (%d bytes)", len(aliasOut), len(coreOut))
	}
	if !strings.Contains(coreOut, "11|line 11") || !strings.Contains(coreOut, "70|line 70") {
		t.Fatalf("offset+limit window wrong: %q", coreOut)
	}
}

// TestAliasReadAcceptsStringifiedLimit pins the convenience half: the
// alias accepts the transcript's mistyped shape ("limit":"35" was the
// recorded wire form) and parses it; a non-numeric token still fails
// with an error that names the offending value.
func TestAliasReadAcceptsStringifiedLimit(t *testing.T) {
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
	tools := p.Tools()

	out, err := tools["files.read"].Run(context.Background(), map[string]any{
		"path": path, "limit": "5",
	})
	if err != nil || !strings.Contains(out, "5|line 5") || strings.Contains(out, "6|") {
		t.Fatalf("stringified limit: out=%q err=%v", out, err)
	}
	_, err = tools["files.read"].Run(context.Background(), map[string]any{
		"path": path, "limit": "many",
	})
	if err == nil || !strings.Contains(err.Error(), `"limit" must be an integer, got "many"`) {
		t.Fatalf("non-numeric limit: %v (want an honest rejection)", err)
	}
	// The core tool's read path rides the same parser.
	out, err = tools["files"].Run(context.Background(), map[string]any{
		"action": "read", "path": path, "limit": "5",
	})
	if err != nil || !strings.Contains(out, "5|line 5") {
		t.Fatalf("core tool stringified limit: out=%q err=%v", out, err)
	}
}
