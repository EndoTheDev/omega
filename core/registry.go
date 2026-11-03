package core

import "sort"

// Services is the universal seam registry (restructure part 3, the
// seam constitution): one stringly-keyed store of services capsules
// offer at mount, extending the mount claim machinery's
// one-owner-per-key law. Every seam - the serving surface's, another
// capsule's, the agent's own needs - resolves here by key and
// type-asserts to the consumer's OWN interface. The substrate never
// names a domain interface again: a capsule-shaped type in core is
// residue by the deletability law, but the key string is data (one
// inert trace per consumer's resolve call, accepted and documented).
//
// The registry is an IMMUTABLE value: offer and release build a new
// instance and the Context field swaps the pointer. That is what
// keeps the mount's scoped-proxy law intact - a remount's offers land
// on the proxy Context copy and commit replays them forward, a
// panicked mount leaves nothing behind, exactly like every other
// Context write. A consumer read takes the pointer snapshot; a live
// toggle swaps it under the reader's feet the same way every other
// seam read does (no lazy lookup can observe a half-state).
//
// Offer is claim-checked at mount: one owner per key, a different
// tenant offering a held key fails the mount loud (the same fail-loud
// boot the exclusive slots always had), a same-tenant re-offer
// replaces (the clean-idempotent remount, Q7). The mount journal
// replays offers backward at dismount: the undo restores exactly the
// previous pointer, so after-dismount equals before-mount (the
// census).
type Services struct {
	values map[string]any
	owners map[string]string
}

// NewServices returns an empty registry.
func NewServices() *Services {
	return &Services{values: map[string]any{}, owners: map[string]string{}}
}

// With returns a registry with svc recorded under key for module. A
// foreign offer on a held key errors loud; a same-tenant re-offer
// replaces. The receiver is shared by the mount journal's apply/undo
// closures - never mutated, only swapped.
func (s *Services) With(key, module string, svc any) (*Services, error) {
	if s == nil {
		s = NewServices()
	}
	next := &Services{values: make(map[string]any, len(s.values)+1), owners: make(map[string]string, len(s.owners)+1)}
	for k, v := range s.values {
		next.values[k] = v
		next.owners[k] = s.owners[k]
	}
	if owner, ok := next.owners[key]; ok && owner != module {
		return nil, &foreignOfferError{Key: key, Owner: owner, Module: module}
	}
	next.values[key] = svc
	next.owners[key] = module
	return next, nil
}

// Without returns a registry with key removed - only when module
// still owns it (a re-enabled service's chain never clobbers someone
// else's offer).
func (s *Services) Without(key, module string) *Services {
	if s == nil || s.owners[key] != module {
		return s
	}
	next := &Services{values: map[string]any{}, owners: map[string]string{}}
	for k, v := range s.values {
		if k == key {
			continue
		}
		next.values[k] = v
		next.owners[k] = s.owners[k]
	}
	return next
}

// Resolve returns the live service for key. Nil means not offered -
// the consumer's own nil-safe degradation applies (the floors law:
// absence is a documented state, never a lazy guess). Nil-receiver
// safe: a bare Context with no registry resolves nothing.
func (s *Services) Resolve(key string) any {
	if s == nil {
		return nil
	}
	return s.values[key]
}

// Has reports whether key is currently offered (the census reads
// presence, never the values). Nil-receiver safe.
func (s *Services) Has(key string) bool {
	if s == nil {
		return false
	}
	_, ok := s.values[key]
	return ok
}

// Owner returns the tenant that holds key ("" when free).
func (s *Services) Owner(key string) string {
	if s == nil {
		return ""
	}
	return s.owners[key]
}

// Keys lists the held keys sorted - the census slot inventory and
// registry introspection read it.
func (s *Services) Keys() []string {
	if s == nil {
		return nil
	}
	out := make([]string, 0, len(s.values))
	for k := range s.values {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// foreignOfferError is one wrong-tenant offer on a held key - the
// boot error naming the seam key, the holding module and the claimant.
type foreignOfferError struct {
	Key    string
	Owner  string
	Module string
}

func (e *foreignOfferError) Error() string {
	return "seam key " + e.Key + " offered by " + e.Module + " is already held by " + e.Owner
}