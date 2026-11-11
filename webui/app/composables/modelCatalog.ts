// omega webui: TYPED MODEL PICKERS' catalog (card t_70c28128). The
// settings pane's model fields are BandSelect dropdowns populated from
// the provider's live model list, each filtered to its slot's
// capability. This composable owns the data: one /models fetch per
// settings open (module state shared by every model field - no
// per-field requests, no polling), the capability-filter rules, and
// the curated fallback for listings that carry no flags.
//
// Grounded 2026-11-01: the ollama-family /api/tags wire (the keypool
// mirror passes it through) carries `capabilities` per model - e.g.
// clef:27b answers ["decision","vision"]. The openai/anthropic list
// endpoints answer ids only. When the wire carries no capability for a
// model (or no capabilities at all), the curated sets below supply
// it: small, greppable, updated in this one file.
import { ref } from "vue";
import { fetchModels, type ModelProviderSection } from "./omega.ts";

export type ModelFilter = "decision" | "vision" | "";
export interface ModelEntry {
  name: string;
  capabilities: string[];
}

// The decision family (System One): every model the approval gate's
// decision slot may name. A listing entry matches by family prefix
// (clef:27b, clef-flash) or exact name.
export const KNOWN_DECISION_MODELS = ["clef", "clef-flash", "nimble", "tev1"];

// Vision families for listings that answer without capabilities
// (openai/anthropic id lists, older mirrors). Extend here when the
// wire does not say, never guess per-slot in the components.
export const KNOWN_VISION_MODELS = [
  "gemma3",
  "gemma4",
  "llava",
  "llama3.2-vision",
  "minicpm-v",
];

// familyMatch: curated entries name FAMILIES, not exact wires - the
// qualifier (":27b") and the variant tag ("-flash") never break the
// match.
const familyMatch = (name: string, family: string) =>
  name === family ||
  name.startsWith(family + ":") ||
  name.startsWith(family + "-");

// effectiveCapabilities: the model's flags after the curated fallback.
// Wire flags win - the curated sets only add what the wire didn't say.
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

// filterModelEntries: the slot rules. decision and vision narrow to
// the matching capability; the chat slots (empty filter) see the full
// available list.
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

// --- the auxiliary "default" entry (card t_df46c8cf) ---

// AUX_DEFAULT_VALUE is the sentinel an auxiliary model knob persists for
// "follow the chat's model". It is a WORD, not a model name: the server
// resolves it per call (the composer's chip at caption time), so a main
// model switch carries the aux lane along with no config write.
export const AUX_DEFAULT_VALUE = "default";

// withAuxDefault prepends the "default" entry to a model picker's option
// list for the aux knobs the server marks default_option. The entry
// rides FIRST - the "leave it to the chat" choice reads before any named
// model - and it is the one option that survives an empty catalog.
export function withAuxDefault(
  options: { value: string; label: string }[],
  enabled: boolean,
  label = "default (the chat's model)",
): { value: string; label: string }[] {
  if (!enabled) return options;
  return [{ value: AUX_DEFAULT_VALUE, label }, ...options];
}

// --- when a picker must fall back to free text ---

// pickerNeedsFreeText: the ONE rule that decides whether a model picker
// renders the listbox or the degraded free-text input + hint. Count only
// the NAMED models: the aux "default" sentinel is a rendering entry, not
// a model, so a knob that offers it must still degrade to free text when
// the filtered catalog matched nothing - otherwise the escape hatch
// (type a model name) disappears behind a one-item dead dropdown.
export function pickerNeedsFreeText(
  namedCount: number,
  loading: boolean,
  error: string | null,
): boolean {
  if (error !== null) return true;
  return !loading && namedCount === 0;
}

// --- a filtered slot's DISQUALIFIER (card t_dac84a33) ---

// slotRejectReason says why a model may not be picked in a filtered slot,
// or null when it may. The settings pickers render a rejected row
// DISABLED WITH THIS REASON instead of dropping it: the catalog must read
// complete, and an operator can see that a model exists and why the knob
// refuses it. An empty filter (chat slots, the main model) never rejects.
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

// --- the value a knob PERSISTS (card t_dac84a33) ---

// knobModelValue maps the picker's emit onto the value a settings knob
// stores. The word "default" carries TWO meanings here and they stay
// apart: the composer grammar's "default:model" addresses the BASE
// PROVIDER's section, while the aux inherit word "default"
// (AUX_DEFAULT_VALUE) means "follow the model the chat runs" - a literal
// the pinned inherit row is the only source of.
//   - slot knobs (compaction, summarizer, curator, approval) persist the
//     chat grammar VERBATIM: "instance:model" / "default:model" - the
//     server resolves them on its slot lane.
//   - every other model knob persists a BARE model name: vision_model,
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

// --- the shared catalog state (one fetch per settings open) ---

export interface ModelCatalogState {
  /** null until a fetch answers; null + error = the list never landed */
  entries: ModelEntry[] | null;
  error: string | null;
  loading: boolean;
  /** the grouped catalog (card t_1914a8c9): one section per provider,
      "default" first - the slot pickers read "instance:model" pairs
      from the named sections. null until the payload carries one. */
  sections: ModelProviderSection[] | null;
}

const state = ref<ModelCatalogState>({
  entries: null,
  error: null,
  loading: false,
  sections: null,
});
let inflight: Promise<void> | null = null;

// refreshModelCatalog replaces the shared list with a fresh /models
// answer. One inflight request at a time: N model fields calling this
// on open still ride one request. On failure the error lands in state
// and entries stay null - the fields degrade to text entry with the
// error as the hint, never a silent dead dropdown.
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