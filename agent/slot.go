package agent

import "strings"

// The per-subsystem model slot. The resolution rule is ONE and must stay one: a prefix
// before the first colon that names a CONFIGURED provider instance (or the reserved
// "default") routes the consult to that instance; anything else is the WHOLE slot as a bare
// model on the default instance, failing loudly provider-side when the model is missing.

// DefaultModelSlot is the reserved word a picker offers as "default (inherit from the main
// model)": the lane follows the model its consumer serves, resolved at the call boundary.
const DefaultModelSlot = "default"

// ParseModelSlot splits a slot into its instance prefix and model name, grammar only - no
// configured-instance knowledge: "" and "bare" give an empty prefix, "name:model" gives both.
func ParseModelSlot(slot string) (instance, model string) {
	slot = strings.TrimSpace(slot)
	name, rest, hasColon := strings.Cut(slot, ":")
	if !hasColon || name == "" {
		return "", slot
	}
	return name, rest
}

// ResolveModelSlot resolves a slot against a Provider. The reserved word alone
// (DefaultModelSlot) answers an empty model on the receiver: "default" means the model this
// consumer already serves, which is the empty-slot branch - never a model by that name.
func ResolveModelSlot(prov Provider, slot string) (Provider, string) {
	name, model := ParseModelSlot(slot)
	if name == "" {
		// The reserved word on its own: answering the empty model keeps the whole slot rule in
		// one place, so the word never reaches a provider as a model literally named "default".
		if model == DefaultModelSlot {
			return prov, ""
		}
		return prov, slot
	}
	ir, ok := prov.(ProviderInstances)
	if !ok {
		return prov, slot
	}
	if name == "default" {
		return prov, model
	}
	inst, ok := ir.Instance(name)
	if !ok {
		return prov, slot
	}
	return inst, model
}
