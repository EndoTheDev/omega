// omega webui: TYPED MODEL PICKERS' catalog.
import { ref } from "vue";
import { fetchModels, type ModelProviderSection } from "./omega.ts";

export type ModelFilter = "decision" | "vision" | "";
export interface ModelEntry {
  name: string;
  capabilities: string[];
}

// The decision family (System One).
export const KNOWN_DECISION_MODELS = ["clef", "clef-flash", "nimble", "tev1"];

// Vision families for listings that answer without capabilities.
// Extend here when the wire does not say, never guess per-slot in the components.
export const KNOWN_VISION_MODELS = [
  "gemma3",
  "gemma4",
  "llava",
  "llama3.2-vision",
  "minicpm-v",
];

// familyMatch: curated entries name FAMILIES, not exact wires - the qualifier (":27b")
// and the variant tag ("-flash") never break the match.
const familyMatch = (name: string, family: string) =>
  name === family ||
  name.startsWith(family + ":") ||
  name.startsWith(family + "-");

// effectiveCapabilities: the model's flags after the curated fallback.
export function effectiveCapabilities(
  name: string,
  wire: string[] | null | undefined,
): string[] {
  const caps = new Set(wire ?? []);
  for (const family of KNOWN_DECISION_MODELS)
    if (familyMatch(name, family)) caps.add("decision");
  for (const family of KNOWN_VISION_MODELS)
    if (familyMatch(name, family)) caps.add("vision");
  return [...caps];
}

// filterModelEntries: the slot rules.
export function filterModelEntries(
  entries: ModelEntry[],
  filter: ModelFilter,
): ModelEntry[] {
  if (filter === "decision" || filter === "vision")
    return entries.filter((e) =>
      effectiveCapabilities(e.name, e.capabilities).includes(filter),
    );
  return entries;
}

// the auxiliary "default" entry

// AUX_DEFAULT_VALUE is the sentinel an auxiliary model knob persists for "follow.
export const AUX_DEFAULT_VALUE = "default";

// withAuxDefault prepends the "default" entry to a model picker's option list.
export function withAuxDefault(
  options: { value: string; label: string }[],
  enabled: boolean,
  label = "default (the chat's model)",
): { value: string; label: string }[] {
  if (!enabled) return options;
  return [{ value: AUX_DEFAULT_VALUE, label }, ...options];
}

// when a picker must fall back to free text

// pickerNeedsFreeText: the ONE rule that decides whether a model picker renders.
// Count only the NAMED models: the aux "default" sentinel is a rendering entry, not a
// model, so a knob that offers it must still degrade to free text when the filtered
// catalog matched nothing - otherwise the escape hatch (type a model name) disappears
// behind a one-item dead dropdown.
export function pickerNeedsFreeText(
  namedCount: number,
  loading: boolean,
  error: string | null,
): boolean {
  if (error !== null) return true;
  return !loading && namedCount === 0;
}

// a filtered slot's DISQUALIFIER

// slotRejectReason says why a model may not be picked in a filtered slot, or null. The
// settings pickers render a rejected row DISABLED WITH THIS REASON instead of dropping
// it: the catalog must read complete, and an operator can see that a model exists and
// why the knob refuses it. An empty filter (chat slots, the main model) never rejects.
export function slotRejectReason(
  filter: ModelFilter,
  model: string,
  wire: string[] | null | undefined,
): string | null {
  if (filter !== "vision" && filter !== "decision") return null;
  const caps = effectiveCapabilities(model, wire);
  if (filter === "vision" && !caps.includes("vision"))
    return "not a vision model";
  if (filter === "decision" && !caps.includes("decision"))
    return "not an approval model";
  return null;
}

// the value a knob PERSISTS

// knobModelValue maps the picker's emit onto the value a settings knob
// stores. The word "default" carries TWO meanings here and they stay
// apart: the composer grammar's "default:model" addresses the BASE
// PROVIDER's section, while the aux inherit word "default"
// (AUX_DEFAULT_VALUE) means "follow the model the chat runs" - a literal
// the pinned inherit row is the only source.
// slot knobs (compaction, summarizer, curator, approval) persist the
//     chat grammar VERBATIM: "instance:model" / "default:model" - the
//     server resolves them on its slot lane.
// every other model knob persists a BARE model name: vision_model,
//     title_model and provider.model_name feed a provider directly, so a
//     section prefix is never part of the value. An inherited "default"
//     rides through untouched and is never produced by a section pick.
export function knobModelValue(
  raw: string,
  slot: boolean,
  sections: string[],
): string {
  if (slot) return raw;
  if (raw === AUX_DEFAULT_VALUE) return raw;
  if (raw.startsWith("default:")) return raw.slice("default:".length);
  // a model's own name may carry colons ("clef:27b"), so strip exactly the
  // KNOWN section prefix the picker adds - never a split on the first one
  for (const name of sections) {
    if (name === "" || name === "default") continue;
    if (raw.startsWith(name + ":")) return raw.slice(name.length + 1);
  }
  return raw;
}

// the shared catalog state (one fetch per settings open)

export interface ModelCatalogState {
  /** null until a fetch answers; null + error = the list never landed */
  entries: ModelEntry[] | null;
  error: string | null;
  loading: boolean;
  sections: ModelProviderSection[] | null;
}

const state = ref<ModelCatalogState>({
  entries: null,
  error: null,
  loading: false,
  sections: null,
});
let inflight: Promise<void> | null = null;

// refreshModelCatalog replaces the shared list with a fresh /models answer. On failure
// the error lands in state and entries stay null - the fields degrade to text entry
// with the error as the hint, never a silent dead dropdown.
export function refreshModelCatalog() {
  if (inflight) return;
  state.value = { entries: null, error: null, loading: true, sections: null };
  inflight = fetchModels()
    .then((info) => {
      const wireCaps = info.capabilities ?? {};
      state.value = {
        entries: info.models.map((name) => ({
          name,
          capabilities: wireCaps[name] ?? [],
        })),
        error: null,
        loading: false,
        sections: info.providers ?? null,
      };
    })
    .catch((err) => {
      state.value = {
        entries: null,
        error: err instanceof Error ? err.message : String(err),
        loading: false,
        sections: null,
      };
    })
    .finally(() => {
      inflight = null;
    });
}

export function useModelCatalog() {
  return { state, refresh: refreshModelCatalog };
}