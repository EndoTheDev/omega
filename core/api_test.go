package core

// The universal-mount vocabulary tests (v0.12 phase B): identity
// parsing (Q10), the split-panic error shape (Q11), and the Refusal
// ledger plumbing (Q12).

import (
	"errors"
	"testing"
)

func TestParseSeamName(t *testing.T) {
	cases := []struct {
		name   string
		module string
		verb   string
		bad    bool
	}{
		{name: "kanban.view", module: "kanban", verb: "view"},
		{name: "web.search", module: "web", verb: "search"},
		{name: "a.b", module: "a", verb: "b"},
		{name: "nodot", bad: true},
		{name: "two.dots.here", bad: true},
		{name: ".emptymodule", bad: true},
		{name: "emptyverb.", bad: true},
		{name: "", bad: true},
	}
	for _, c := range cases {
		module, verb, err := ParseSeamName(c.name)
		if c.bad {
			if err == nil {
				t.Errorf("%q: must reject", c.name)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: %v", c.name, err)
			continue
		}
		if module != c.module || verb != c.verb {
			t.Errorf("%q: got %q/%q", c.name, module, verb)
		}
	}
}

func TestModulePanicError(t *testing.T) {
	err := &ModulePanicError{Name: "kanban", Cause: "boom", Suggestion: "add to disabled"}
	if err.Error() != `module "kanban" panicked: boom (hint: add to disabled)` {
		t.Errorf("message shape: %q", err.Error())
	}
	var asError error = err
	var panicked *ModulePanicError
	if !errors.As(asError, &panicked) || panicked.Name != "kanban" {
		t.Errorf("errors.As must recover the module name")
	}
}

func TestRefusalLedgerPlumbing(t *testing.T) {
	var logged Refusal
	sink := RefusalSinkFunc(func(r Refusal) { logged = r })
	refusal := Refusal{Module: "kanban", Seam: "panels", Predicate: "kanban.move"}
	sink.Log(refusal)
	if logged.Module != "kanban" || logged.Seam != "panels" || logged.Predicate != "kanban.move" {
		t.Errorf("the sink lost the refusal's identity: %+v", logged)
	}
}
