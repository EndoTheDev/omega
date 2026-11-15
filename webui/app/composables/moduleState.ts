// omega webui: THE MODULE-STATE STORE (card t_103e30a6). ONE reactive copy
// of GET /modules for the whole page, because two surfaces now read it and
// they must never disagree:
//   - the settings tab's MODULES section (the rows + their live toggles),
//   - the dock's runtime gate (dockables.ts moduleGated): a module-owned
//     dockable whose module is OFF leaves the strip, and returns the moment
//     it mounts again.
//
// One fetch at boot + the toggle responses feed it; there is NO polling
// timer. The toggle response IS the refreshed payload (the server answers
// PUT /modules/:name with the whole introspection), so a settings click
// updates the dock in the same tick the row flips.
//
// The last-known OFF set is cached (MODULE_CACHE_KEY) and hydrated
// SYNCHRONOUSLY at boot, before the dock's first render: a reload with a
// module disabled must not paint the chip for the ~1ms a fetch takes and
// then drop it. The boot fetch reconciles the cache with the runtime.

import { ref, type Ref } from "vue";
import {
  fetchModules,
  toggleModule,
  type ModuleRefusal,
  type ModuleRow,
  type ModulesPayload,
} from "./omega";

/** the last-known disabled-module set (the boot hydrate's source) */
export const MODULE_CACHE_KEY = "omega.modules-off";

const rows = ref<ModuleRow[]>([]);
const refusals = ref<ModuleRefusal[]>([]);
/** a live payload has landed this page. Before it the gate trusts the
    cache; if neither exists nothing is hidden (the honest default). */
const known = ref(false);
const disabled = ref<ReadonlySet<string>>(new Set());

let cacheStore: Storage | null = null;
let inflight: Promise<ModulesPayload> | null = null;

/** the settings section's rows (the live /modules introspection) */
export const moduleRows: Ref<ModuleRow[]> = rows;
/** the refusal ledger tail the section prints under the list */
export const moduleRefusals: Ref<ModuleRefusal[]> = refusals;
/** whether a live payload has landed this page */
export const modulesKnown: Ref<boolean> = known;
/** the module names the runtime reports disabled - the gate's off set */
export const modulesOff: Ref<ReadonlySet<string>> = disabled;

/** hydrateModuleState: the SYNCHRONOUS boot read of the cache. Called once
    from app.vue before createDock, so the first paint already knows which
    chips a disabled module owns. A corrupt cache is no cache. */
export function hydrateModuleState(store: Storage): void {
  cacheStore = store;
  const raw = store.getItem(MODULE_CACHE_KEY);
  if (!raw) return;
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return; // a corrupt cache is no cache: the boot fetch corrects it
  }
  if (!Array.isArray(parsed)) return;
  const names = parsed.filter((n): n is string => typeof n === "string" && n !== "");
  disabled.value = new Set(names);
}

/** the cache write: the off set, nothing else (the rows are server truth
    the next fetch re-reads). */
function cacheOff(): void {
  if (!cacheStore) return;
  cacheStore.setItem(MODULE_CACHE_KEY, JSON.stringify([...disabled.value]));
}

/** applyModules: the ONE way a payload lands in the store - the boot read
    and every toggle response ride it, so settings and the dock converge on
    the same set. */
export function applyModules(payload: ModulesPayload): void {
  rows.value = payload.modules;
  refusals.value = payload.refusals;
  disabled.value = new Set(
    payload.modules.filter((m) => m.state === "disabled").map((m) => m.name),
  );
  known.value = true;
  cacheOff();
}

/** loadModules: GET /modules through the store. Concurrent callers share
    one request (settings may open while the boot read is in flight); a
    later call re-reads, so reopening the section stays fresh. */
export async function loadModules(): Promise<ModulesPayload> {
  if (inflight) return inflight;
  inflight = fetchModules()
    .then((payload) => {
      applyModules(payload);
      return payload;
    })
    .finally(() => {
      inflight = null;
    });
  return inflight;
}

/** commitModuleToggle: the ONE write path (the settings row's pill). The
    response is the refreshed introspection, applied here - never a second
    fetch, never a local guess. A refusal throws with the server's ledger
    text; the caller re-reads through loadModules to surface the ledger. */
export async function commitModuleToggle(
  name: string,
  enabled: boolean,
): Promise<ModulesPayload> {
  const payload = await toggleModule(name, enabled);
  applyModules(payload);
  return payload;
}
