package agent

import "strings"

// The per-subsystem model slot (card t_fc0b2f2d): provider settings
// assigns one named model per subsystem - compaction, summarizer,
// skills, approval gate - and the four call sites resolve it through
// the helpers below. The value is either "instance:model" (a model
// served by a named provider instance, card t_7dcd4280) or a bare
// model served by the default instance; empty = the consumer's own
// default.
//
// The resolution rule is ONE and must stay one: a prefix before the
// first colon that names a CONFIGURED provider instance (or the
// reserved "default") routes the consult to that instance; anything
// else is the WHOLE slot as a bare model on the default instance. An
// instance-shaped prefix without a configured instance stays a bare
// model - the consult reaches the default instance and a missing
// model fails loudly provider-side, never a silent fallback. The
// colon form is unambiguous in practice: ollama model tags qualify
// with a colon but their family prefixes rarely match a configured
// instance id (the id is validated lowercase [a-z0-9_-]).

// DefaultModelSlot is the reserved word an auxiliary model picker offers
// as "default (inherit from the main model)" (card t_b403ad88): the lane
// follows the model its consumer serves, resolved at the call boundary -
// the choice persists as the literal word, never frozen to a model name.
// The vision lane answers it through its own sentinel (provider capsule);
// the slot lanes answer it through ResolveModelSlot below.
const DefaultModelSlot = "default"

// ParseModelSlot splits a slot into its instance prefix and model
// name, grammar only - no configured-instance knowledge.
//
//	""              -> ("", "")                 the consumer's default
//	"name:model"    -> (name, model)            an instance-qualified
//	                                               model, "default" names
//	                                               the default instance
//	anything else   -> ("", slot)               a bare model
func ParseModelSlot(slot string) (instance, model string) {
	slot = strings.TrimSpace(slot)
	name, rest, hasColon := strings.Cut(slot, ":")
	if !hasColon || name == "" {
		return "", slot
	}
	return name, rest
}

// ResolveModelSlot resolves a slot against a Provider for the three
// consult-shaped call sites (compaction, summarizer, curator): the
// returned provider is the instance the slot names (when configured -
// "default" is the receiver itself) and the returned model is what the
// copy should serve (empty = the instance's own default model). A
// provider that carries no named instances (or a slot prefix that is
// not one) keeps the whole slot as a bare model on the receiver.
//
// The reserved word alone (DefaultModelSlot) answers an empty model on
// the receiver: "default" means the model this consumer already serves,
// which is exactly the empty-slot branch - never a model by that name.
func ResolveModelSlot(prov Provider, slot string) (Provider, string) {
	name, model := ParseModelSlot(slot)
	if name == "" {
		// The reserved word on its own (card t_b403ad88): "default"
		// is what every aux picker offers as "inherit from the main
		// model", and the receiver IS that model. Answering the empty
		// model keeps the whole slot rule in one place - the caller's
		// "empty = my own model" branch is the same branch, so the
		// word never reaches a provider as a model literally named
		// "default".
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
