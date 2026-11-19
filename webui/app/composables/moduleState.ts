// THE MODULE-STATE STORE: ONE reactive copy of GET /modules for the page, because the
// settings MODULES section and the dock's runtime gate must never disagree. One boot
// fetch plus the toggle responses feed it - NO polling. The OFF set is hydrated
// SYNCHRONOUSLY from cache before the dock's first render (a disabled module must not
// paint its chip for the ~1ms a fetch takes, then drop it); the boot fetch reconciles.

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
const known = ref(false);
const disabled = ref<ReadonlySet<string>>(new Set());

let cacheStore: Storage | null = null;
let inflight: Promise<ModulesPayload> | null = null;

export const moduleRows: Ref<ModuleRow[]> = rows;
export const moduleRefusals: Ref<ModuleRefusal[]> = refusals;
export const modulesKnown: Ref<boolean> = known;
/** the module names the runtime reports disabled - the gate's off set */
export const modulesOff: Ref<ReadonlySet<string>> = disabled;

/** the SYNCHRONOUS boot read of the cache; must run before the dock's first render */
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

function cacheOff(): void {
  if (!cacheStore) return;
  cacheStore.setItem(MODULE_CACHE_KEY, JSON.stringify([...disabled.value]));
}

export function applyModules(payload: ModulesPayload): void {
  rows.value = payload.modules;
  refusals.value = payload.refusals;
  disabled.value = new Set(
    payload.modules.filter((m) => m.state === "disabled").map((m) => m.name),
  );
  known.value = true;
  cacheOff();
}

/** GET /modules through the store; concurrent callers share one request */
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

/**
 * THE write path. The response is the refreshed introspection, applied here - never a
 * second fetch. A refusal throws with the ledger text; the caller re-reads to surface it.
 */
export async function commitModuleToggle(
  name: string,
  enabled: boolean,
): Promise<ModulesPayload> {
  const payload = await toggleModule(name, enabled);
  applyModules(payload);
  return payload;
}
