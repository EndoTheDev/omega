// The universal-mount vocabulary (v0.12 phase B, ADR block). stdlib-only by the core
// law - this is vocabulary, never behavior: core records refusals and parses names,
// modules own their types. Payload contract (settled Q9): payloads crossing a
// registration carry raw JSON (map[string]any / json.RawMessage); the modules own the
// payload's types (binary-purity constraint). The same tenant re-registering replaces
// its entry (the clean-idempotent remount, Q7); a different tenant on an
// already-mounted seam is a boot error, fail loud (Q5).
package core

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Refusal is one boundary rejection in the refusal ledger (settled Q12).
type Refusal struct {
	Module    string
	Seam      string
	Predicate string
	Err       error
	At        time.Time
}

// RefusalSink receives every refusal the mount records.
type RefusalSink interface {
	Log(Refusal)
}

// RefusalSinkFunc adapts a logging func to RefusalSink.
type RefusalSinkFunc func(Refusal)

// Log implements RefusalSink.
func (f RefusalSinkFunc) Log(r Refusal) { f(r) }

// ModulePanicError is the split-panic vocabulary.
// Types only in phase B - no panic wiring lands until the split-panic card.
type ModulePanicError struct {
	Name       string // the module that panicked
	Cause      string // the panic value, as text
	Suggestion string // the fix-it hint (usually "add to disabled")
}

// Error names the module and the hint, the shape Q11 requires.
func (e *ModulePanicError) Error() string {
	return fmt.Sprintf("module %q panicked: %s (hint: %s)", e.Name, e.Cause, e.Suggestion)
}

// ParseSeamName splits a namespaced registration identity: exactly one dot. Every
// machine-facing registration carries this shape; the caller enforces the tenant half
// matches the mounting module.
func ParseSeamName(name string) (module, verb string, err error) {
	module, verb, ok := strings.Cut(name, ".")
	if !ok || module == "" || verb == "" || strings.Contains(verb, ".") {
		return "", "", fmt.Errorf("registration %q must be one module.verb namespace pair", name)
	}
	return module, verb, nil
}

// ErrSeamForeignTenant marks a name claimed by another module (the. Callers wrap it
// with the offending name and both tenants.
var ErrSeamForeignTenant = errors.New("seam already mounted by another module")
