// The universal-mount vocabulary (v0.12 phase B, ADR block): the
// types that cross the one typed registration surface (agent's
// ExtensionAPI). stdlib-only by the core law - this is vocabulary,
// never behavior: core records refusals and parses names, modules
// own their types.
//
// Payload contract (settled Q9): payloads crossing a registration
// carry raw JSON (map[string]any / json.RawMessage); the modules own
// the payload's types (binary-purity constraint).
//
// Refusal ledger (settled Q12): every boundary rejection records the
// module, the seam and the failed predicate. Nothing else acts on a
// refusal in phase B - the types only; wiring refusals onto the event
// spine is a later phase, deliberately not started here.
//
// Exclusivity (settled Q5/Q10): every registration identity is a
// namespaced string ("kanban.view"), the prefix enforced at
// registration; one name, one tenant. The same tenant re-registering
// replaces its entry (the clean-idempotent remount, Q7); a different
// tenant on an already-mounted seam is a boot error, fail loud (Q5).
package core

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Refusal is one boundary rejection in the refusal ledger (settled
// Q12). Module names the capsule whose payload was refused, Seam the
// seam family it crossed, Predicate the identity of the failed check,
// Err the failure itself.
type Refusal struct {
	Module    string
	Seam      string
	Predicate string
	Err       error
	At        time.Time
}

// RefusalSink receives every refusal the mount records. The ledger
// (Q12) logs module, seam and failed predicate and nothing else -
// printing, the event spine and the conformance suite all come with
// the phases that consume them.
type RefusalSink interface {
	Log(Refusal)
}

// RefusalSinkFunc adapts a logging func to RefusalSink.
type RefusalSinkFunc func(Refusal)

// Log implements RefusalSink.
func (f RefusalSinkFunc) Log(r Refusal) { f(r) }

// ModulePanicError is the split-panic vocabulary (implementation
// sketch 1, settled Q11): the boot path re-panics with it naming the
// module and the fix-it hint; the remount path rejects the toggle
// with the same error and rolls the mount back. Types only in phase B
// - no panic wiring lands until the split-panic card.
type ModulePanicError struct {
	Name       string // the module that panicked
	Cause      string // the panic value, as text
	Suggestion string // the fix-it hint (usually "add to disabled")
}

// Error names the module and the hint, the shape Q11 requires.
func (e *ModulePanicError) Error() string {
	return fmt.Sprintf("module %q panicked: %s (hint: %s)", e.Name, e.Cause, e.Suggestion)
}

// ParseSeamName splits a namespaced registration identity: exactly one
// dot, "module.verb", both halves non-empty. Every machine-facing
// registration carries this shape; the caller enforces the tenant half
// matches the mounting module. Anything else is a fail-loud error.
func ParseSeamName(name string) (module, verb string, err error) {
	module, verb, ok := strings.Cut(name, ".")
	if !ok || module == "" || verb == "" || strings.Contains(verb, ".") {
		return "", "", fmt.Errorf("registration %q must be one module.verb namespace pair", name)
	}
	return module, verb, nil
}

// ErrSeamForeignTenant marks a name claimed by another module (the
// one-name-one-tenant violation, Q5). Callers wrap it with the
// offending name and both tenants.
var ErrSeamForeignTenant = errors.New("seam already mounted by another module")
