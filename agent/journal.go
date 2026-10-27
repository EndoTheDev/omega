// The contribution journal (v0.12 phase E1, implementation sketch 1,
// grill Q11): every write a mount makes through the ExtensionAPI is
// journaled. Boot applies directly and journals for the later
// dismount; a runtime remount applies to a scoped Context proxy - the
// only write path during remount - and commits by replaying the
// journal forward onto the live Context. A panic during remount plays
// the registry side backward and discards the proxy: the live Context
// never saw the write, the rejected toggle leaves zero contributions.
// That split is the load-bearing guarantee of live toggling.
package agent

import (
	"reflect"

	"github.com/EndoTheDev/omega/core"
)

// contribution is one journaled write. ctxApply/ctxUndo are the
// forward and backward Context writes (nil for registry-only
// contributions); regUndo removes a registry-side record
// (claims, routes, subcommands, emitters). applied reports whether
// the Context-side write is already on the live Context - boot
// writes apply directly; remount writes apply to the scoped proxy
// and turn live only at commit.
type contribution struct {
	ctxApply func(*core.Context)
	ctxUndo  func(*core.Context)
	regUndo  func(*Registry)
	applied  bool
}

// journal is one module's ordered contributions, mount order.
// apiMounted marks that the mount went through the ExtensionAPI -
// classic Mount writes are invisible to the journal and the module
// must prove its teardown with a Dismount implementation instead.
type journal struct {
	module     string
	apiMounted bool
	entries    []contribution
}

func newJournal(module string) *journal {
	return &journal{module: module}
}

// add records one contribution, preserving write order.
func (j *journal) add(c contribution) {
	j.entries = append(j.entries, c)
}

// commit replays every contribution whose Context write is not yet
// live onto live, in order (the sketch's "commit replays forward").
// Registry-side records are already live: they land during
// registration and undo individually on rollback.
func (j *journal) commit(live *core.Context) {
	for i := range j.entries {
		e := &j.entries[i]
		if e.ctxApply != nil && !e.applied {
			e.ctxApply(live)
			e.applied = true
		}
	}
}

// undo plays the journal backward: Context writes reversed in reverse
// order, then registry records removed. This is the boot-recorded
// dismount path (Dismount, DismountAll). The Context undos remove by
// identity (removeOne): a dismount deletes only this module's own
// appends, never a slice truncation that eats a later module's
// surface when dismounts leave mount order.
func (j *journal) undo(ctx *core.Context, reg *Registry) {
	for i := len(j.entries) - 1; i >= 0; i-- {
		e := j.entries[i]
		if e.applied && e.ctxUndo != nil {
			e.ctxUndo(ctx)
		}
		if e.regUndo != nil {
			e.regUndo(reg)
		}
	}
}

// playBackRegistry is the remount-panic path (Q11, "panic plays
// backward"): the Context contributions died with the scoped proxy -
// the live Context never saw them - so only the registry side, which
// landed live during registration, needs unwinding.
func (j *journal) playBackRegistry(reg *Registry) {
	for i := len(j.entries) - 1; i >= 0; i-- {
		if e := j.entries[i]; e.regUndo != nil {
			e.regUndo(reg)
		}
	}
}

// removeOne deletes the first element of s identical to want and
// returns the result. Not-found returns s unchanged: the undo removes
// exactly this module's own contribution, never a slice truncation -
// a truncation deletes a later module's append whenever dismounts
// leave strict reverse mount order (the gate audit, live toggles).
func removeOne[T any](s []T, want T) []T {
	for i, v := range s {
		if identical(v, want) {
			out := make([]T, 0, len(s)-1)
			out = append(out, s[:i]...)
			out = append(out, s[i+1:]...)
			return out
		}
	}
	return s
}

// identical reports element identity: reference identity for pointer,
// map, channel and unsafe-pointer values, deep equality otherwise
// (structs and their payloads).
func identical(a, b any) bool {
	pa, pb := reflect.ValueOf(a), reflect.ValueOf(b)
	switch pa.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Chan, reflect.UnsafePointer:
		if pb.Kind() == pa.Kind() {
			return pa.Pointer() == pb.Pointer()
		}
		return false
	}
	return reflect.DeepEqual(a, b)
}
