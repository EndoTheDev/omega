// omega webui: the connection list's client half. The status bar control READS this
// list and switches; the settings pane writes it. The saved list is BACKEND state
// (config.yaml `desktop.instances`, served by GET /instances); localStorage under
// `omega.instances` is only the cache that lets the control paint before the fetch
// lands. Switching never fetches the remote origin (CORS): it navigates, and the page
// reloads. There is no client write path here - settings owns every write.

export interface SavedInstance {
  name: string;
  url: string;
}

export interface InstanceCache {
  list: SavedInstance[];
  lastUsed: string;
}

// the storage surface the cache needs; node tests inject a fake, a browser passes
// localStorage, and a blocked store answers null (the cache is optional by design).
export interface InstanceStorage {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
}

export const INSTANCE_CACHE_KEY = "omega.instances";
export const MAX_INSTANCES = 32;
export const MAX_INSTANCE_NAME = 64;

function defaultStorage(): InstanceStorage | null {
  try {
    return typeof window === "undefined" ? null : window.localStorage;
  } catch {
    return null;
  }
}

// normalizeInstanceName trims and bounds the display name.
export function normalizeInstanceName(raw: string): string {
  const name = String(raw ?? "").trim();
  if (!name) throw new Error("a name is required");
  if (Array.from(name).length > MAX_INSTANCE_NAME) {
    throw new Error(`a name is at most ${MAX_INSTANCE_NAME} characters`);
  }
  return name;
}

// normalizeInstanceURL accepts an http/https address and returns its canonical form: the
// scheme lowercased, no trailing slash. A bare `host:port` is the shorthand every user
// types, so it takes http://; a bare non-http scheme (`javascript:` and friends) is
// refused rather than prefixed.
export function normalizeInstanceURL(raw: string): string {
  const trimmed = String(raw ?? "").trim();
  if (!trimmed) throw new Error("an address is required");
  if (/\s/.test(trimmed)) throw new Error("an address cannot contain spaces");

  let candidate = trimmed;
  const explicit = /^([a-zA-Z][a-zA-Z0-9+.-]*):\/\//.exec(trimmed);
  if (explicit) {
    const scheme = explicit[1].toLowerCase();
    if (scheme !== "http" && scheme !== "https") {
      throw new Error(`only http and https addresses connect (got ${scheme}:)`);
    }
    candidate = `${scheme}://${trimmed.slice(explicit[0].length)}`;
  } else {
    const shorthand = /^[a-zA-Z][a-zA-Z0-9+.-]*:\d+(\/.*)?$/.test(trimmed);
    const bareScheme = /^([a-zA-Z][a-zA-Z0-9+.-]*):/.exec(trimmed);
    if (!shorthand && bareScheme) {
      throw new Error(`only http and https addresses connect (got ${bareScheme[1].toLowerCase()}:)`);
    }
    candidate = `http://${trimmed}`;
  }

  let parsed: URL;
  try {
    parsed = new URL(candidate);
  } catch {
    throw new Error("not a valid address");
  }
  if (!parsed.hostname) throw new Error("the address needs a host");
  const out = parsed.toString();
  return out.endsWith("/") ? out.slice(0, -1) : out;
}

// upsertInstance replaces the row with the same URL in place, else appends. Re-adding a
// saved address is how a row is RENAMED - the settings pane keeps one edit surface.
export function upsertInstance(list: SavedInstance[], row: SavedInstance): SavedInstance[] {
  const out: SavedInstance[] = [];
  let replaced = false;
  for (const item of list) {
    if (item.url === row.url) {
      out.push({ ...row });
      replaced = true;
    } else {
      out.push({ ...item });
    }
  }
  if (!replaced) out.push({ ...row });
  return out;
}

export function removeInstance(list: SavedInstance[], url: string): SavedInstance[] {
  return list.filter((item) => item.url !== url).map((item) => ({ ...item }));
}

// readInstanceCache parses the cache; a corrupt or absent document is the empty cache.
export function readInstanceCache(storage: InstanceStorage | null = defaultStorage()): InstanceCache {
  const empty: InstanceCache = { list: [], lastUsed: "" };
  if (!storage) return empty;
  let parsed: unknown;
  try {
    parsed = JSON.parse(storage.getItem(INSTANCE_CACHE_KEY) ?? "null");
  } catch {
    return empty;
  }
  if (!parsed || typeof parsed !== "object") return empty;
  const doc = parsed as { list?: unknown; lastUsed?: unknown };
  const list: SavedInstance[] = [];
  if (Array.isArray(doc.list)) {
    for (const item of doc.list) {
      if (!item || typeof item !== "object") continue;
      const name = typeof (item as { name?: unknown }).name === "string" ? (item as { name: string }).name : "";
      const url = typeof (item as { url?: unknown }).url === "string" ? (item as { url: string }).url : "";
      if (name && url) list.push({ name, url });
    }
  }
  return { list, lastUsed: typeof doc.lastUsed === "string" ? doc.lastUsed : "" };
}

export function writeInstanceCache(cache: InstanceCache, storage: InstanceStorage | null = defaultStorage()): void {
  if (!storage) return;
  try {
    storage.setItem(INSTANCE_CACHE_KEY, JSON.stringify({ list: cache.list, lastUsed: cache.lastUsed }));
  } catch {
    // a full or blocked store costs the head start, never the list: the backend answers.
  }
}

export function lastUsedInstance(storage: InstanceStorage | null = defaultStorage()): string {
  return readInstanceCache(storage).lastUsed;
}

export function setLastUsedInstance(url: string, storage: InstanceStorage | null = defaultStorage()): void {
  const cache = readInstanceCache(storage);
  writeInstanceCache({ list: cache.list, lastUsed: url }, storage);
}

// fetchInstances reads the backend list. A refusal throws: the caller keeps its cache.
export async function fetchInstances(): Promise<SavedInstance[]> {
  const r = await fetch("/instances");
  if (!r.ok) throw new Error(`instances: ${r.status}`);
  const body = (await r.json()) as { instances?: unknown };
  return Array.isArray(body?.instances) ? (body.instances as SavedInstance[]) : [];
}

export interface InstanceLoad {
  list: SavedInstance[];
  offline: boolean;
}

// loadInstances fetches the backend list and refreshes the cache. A failed fetch is not
// an empty list: the cached rows stay and the control says so.
export async function loadInstances(storage: InstanceStorage | null = defaultStorage()): Promise<InstanceLoad> {
  const cache = readInstanceCache(storage);
  try {
    const list = await fetchInstances();
    writeInstanceCache({ list, lastUsed: cache.lastUsed }, storage);
    return { list, offline: false };
  } catch {
    return { list: cache.list, offline: true };
  }
}

function defaultNavigate(target: string): void {
  if (typeof window !== "undefined") window.location.assign(target);
}

// navigateToInstance is the switch: remember the target, then leave. The page reloads -
// the same thing a browser does when the origin changes.
export function navigateToInstance(
  url: string,
  navigate: (target: string) => void = defaultNavigate,
  storage: InstanceStorage | null = defaultStorage(),
): void {
  const target = normalizeInstanceURL(url);
  setLastUsedInstance(target, storage);
  navigate(target);
}
