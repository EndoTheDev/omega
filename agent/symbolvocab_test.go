package agent

// agent/symbolvocab_test.go: the binary-vocabulary pin for THIS package
// (card t_61ad4490). The nm-purity law proves a capsule's removal leaves
// no capsule noun in the host binary, but it reads compiled SYMBOLS - so
// the leak it caught was invisible to every source-level check and
// invisible on the dev toolchain: go1.27 stopped printing struct tags in
// the compiler-generated `type:.eq.*` symbols, while the CI lane on go1.26
// still does. `type:.eq.struct { ... "yaml:\"provider\"" ... }` IS the
// symbol that failed the nightly provider rotation, and it came from an
// inline `var raw struct { ... }` in configedit.go.
//
// This pin is the same law one layer earlier and on every toolchain: an
// ANONYMOUS struct's yaml tags are spelled into its type symbols, so a
// capsule noun among them survives the capsule. A NAMED type's symbol is
// its name and carries no tags. Hence: in this package, every struct
// that carries yaml tags is a named type.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"strings"
	"testing"
)

// capsuleNouns is the vocabulary the nm-purity rotation protects: one
// noun per capsule directory (scripts/nm-purity.sh --all derives the same
// set). Read from the tree, so a new capsule is covered without touching
// this test.
func capsuleNouns(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir("../harness")
	if err != nil {
		t.Fatalf("read the capsule set: %v", err)
	}
	var nouns []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		nouns = append(nouns, e.Name())
	}
	if len(nouns) == 0 {
		t.Fatal("no capsule directories under ../harness - the noun set is empty, the pin would prove nothing")
	}
	return nouns
}

// TestNoCapsuleNounInAnonymousYAMLTags fails when an anonymous struct in
// this package carries a yaml tag spelling a capsule noun.
func TestNoCapsuleNounInAnonymousYAMLTags(t *testing.T) {
	nouns := capsuleNouns(t)
	// word-boundary matching mirrors the purity check's `grep -w`: a tag
	// like "provider_instances" or "tools" is not the noun "provider".
	word := func(noun string) *regexp.Regexp {
		return regexp.MustCompile(`\b` + regexp.QuoteMeta(noun) + `\b`)
	}
	nounRes := make(map[string]*regexp.Regexp, len(nouns))
	for _, n := range nouns {
		nounRes[n] = word(n)
	}

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	pkg, err := parser.ParseDir(token.NewFileSet(), dir, func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse the agent package: %v", err)
	}

	findings := 0
	for _, p := range pkg {
		for path, file := range p.Files {
			// a struct that is the type of a TypeSpec is NAMED: its symbol
			// is the type name, tags and all are gone from the binary.
			named := map[*ast.StructType]bool{}
			ast.Inspect(file, func(n ast.Node) bool {
				if ts, ok := n.(*ast.TypeSpec); ok {
					if st, ok := ts.Type.(*ast.StructType); ok {
						named[st] = true
					}
				}
				return true
			})
			ast.Inspect(file, func(n ast.Node) bool {
				st, ok := n.(*ast.StructType)
				if !ok || named[st] || st.Fields == nil {
					return true
				}
				for _, field := range st.Fields.List {
					if field.Tag == nil {
						continue
					}
					tag := field.Tag.Value
					for noun, re := range nounRes {
						if !re.MatchString(tag) {
							continue
						}
						findings++
						t.Errorf("%s: anonymous struct tag %s spells the capsule noun %q - name the struct (a named type's symbol is its name; an anonymous one spells its tags into the binary's type symbols, which nm-purity reads as a survivor)",
							filepathOf(path), tag, noun)
					}
				}
				return true
			})
		}
	}
	if findings > 0 {
		t.Fatalf("%d capsule noun(s) in anonymous struct tags under agent/ - see the findings above", findings)
	}
}

// filepathOf trims the parse directory off a resolved path for readable
// findings.
func filepathOf(path string) string {
	if i := strings.LastIndexAny(path, `/\`); i >= 0 {
		return path[i+1:]
	}
	return path
}
