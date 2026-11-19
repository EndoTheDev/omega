// The universal-mount vocabulary: stdlib-only by the core law - vocabulary, never behavior.
// Payloads crossing a registration carry raw JSON (map[string]any / json.RawMessage) and the
// modules own the payload types. The same tenant re-registering replaces its entry; a
// different tenant on an already-mounted seam is a boot error, fail loud.
package core

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Refusal is one boundary rejection in the refusal ledger.
type Refusal struct {
	Module    string
	Seam      string
	Predicate string
	Err       error
	At        time.Time
}

type RefusalSink interface {
	Log(Refusal)
}

// RefusalSinkFunc adapts a logging func to RefusalSink.
type RefusalSinkFunc func(Refusal)

func (f RefusalSinkFunc) Log(r Refusal) { f(r) }

// ModulePanicError is the split-panic vocabulary.
type ModulePanicError struct {
	Name       string
	Cause      string
	Suggestion string
}

// Error names the module and the fix-it hint.
func (e *ModulePanicError) Error() string {
	return fmt.Sprintf("module %q panicked: %s (hint: %s)", e.Name, e.Cause, e.Suggestion)
}

// ParseSeamName splits a namespaced registration identity: exactly one dot. The caller
// enforces that the tenant half matches the mounting module.
func ParseSeamName(name string) (module, verb string, err error) {
	module, verb, ok := strings.Cut(name, ".")
	if !ok || module == "" || verb == "" || strings.Contains(verb, ".") {
		return "", "", fmt.Errorf("registration %q must be one module.verb namespace pair", name)
	}
	return module, verb, nil
}

// ErrSeamForeignTenant marks a name already claimed by another module; callers wrap it with
// the offending name and both tenants.
var ErrSeamForeignTenant = errors.New("seam already mounted by another module")
