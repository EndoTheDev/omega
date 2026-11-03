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
import { fetchModels } from "./omega.ts";

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

// --- the shared catalog state (one fetch per settings open) ---

export interface ModelCatalogState {
  /** null until a fetch answers; null + error = the list never landed */
  entries: ModelEntry[] | null;
  error: string | null;
  loading: boolean;
}

const state = ref<ModelCatalogState>({
  entries: null,
  error: null,
  loading: false,
});
let inflight: Promise<void> | null = null;

// refreshModelCatalog replaces the shared list with a fresh /models
// answer. One inflight request at a time: N model fields calling this
// on open still ride one request. On failure the error lands in state
// and entries stay null - the fields degrade to text entry with the
// error as the hint, never a silent dead dropdown.
export function refreshModelCatalog() {
  if (inflight) return;
  state.value = { entries: null, error: null, loading: true };
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
      };
    })
    .catch((err) => {
      state.value = {
        entries: null,
        error: err instanceof Error ? err.message : String(err),
        loading: false,
      };
    })
    .finally(() => {
      inflight = null;
    });
}

export function useModelCatalog() {
  return { state, refresh: refreshModelCatalog };
}