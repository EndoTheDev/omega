import { ref } from "vue";
import { fetchModels, type ModelProviderSection } from "./omega.ts";

export type ModelFilter = "decision" | "vision" | "";
export interface ModelEntry {
  name: string;
  capabilities: string[];
}

export const KNOWN_DECISION_MODELS = ["clef", "clef-flash", "nimble", "tev1"];

// Vision families, curated: extend here, never guess per-slot in the components.
export const KNOWN_VISION_MODELS = [
  "gemma3",
  "gemma4",
  "llava",
  "llama3.2-vision",
  "minicpm-v",
];

// familyMatch: curated entries name FAMILIES, not exact wires (":27b", "-flash" both match)
const familyMatch = (name: string, family: string) =>
  name === family ||
  name.startsWith(family + ":") ||
  name.startsWith(family + "-");

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

// AUX_DEFAULT_VALUE is the sentinel an auxiliary model knob persists for "follow the chat".
export const AUX_DEFAULT_VALUE = "default";

export function withAuxDefault(
  options: { value: string; label: string }[],
  enabled: boolean,
  label = "default (the chat's model)",
): { value: string; label: string }[] {
  if (!enabled) return options;
  return [{ value: AUX_DEFAULT_VALUE, label }, ...options];
}

// pickerNeedsFreeText is the ONE rule for whether a model picker degrades to free text.
// Count only NAMED models: the aux "default" sentinel is a rendering entry, not a model, so
// a knob offering it must still degrade when the filtered catalog matched nothing.
export function pickerNeedsFreeText(
  namedCount: number,
  loading: boolean,
  error: string | null,
): boolean {
  if (error !== null) return true;
  return !loading && namedCount === 0;
}

// slotRejectReason: why a model may not be picked in a filtered slot, or null. Pickers render
// a rejected row DISABLED with this reason, so the catalog reads complete.
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

// knobModelValue maps the picker's emit onto the value a settings knob stores. The word
// "default" carries TWO meanings: the chat grammar's "default:model" addresses the BASE
// PROVIDER section, while the aux inherit word "default" (AUX_DEFAULT_VALUE) means "follow
// the chat's model". Slot knobs persist the chat grammar VERBATIM; every other model knob
// persists a BARE model name, because vision_model, title_model and provider.model_name
// feed a provider directly.
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

// the shared catalog state, one fetch per settings open
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

// refreshModelCatalog replaces the shared list with a fresh /models answer. On failure the
// error lands in state and entries stay null, so fields degrade to text entry with the
// error as the hint.
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