// omega webui: the connection list's client half. In a desktop window the APP owns the
// list: the host stores it in the local home's config.yaml (`desktop.instances`) and
// answers every read, write and switch over the raw message channel, so a page served by
// a build that predates the title bar still lists and switches the app's own
// connections. A plain browser tab keeps the active server's GET /instances as its
// source - the switcher still works against any serve. localStorage under
// `omega.instances` is the paint-first copy of whichever source is in charge, never a
// second truth. Switching never fetches the remote origin (CORS): it navigates, and the
// page reloads.

import { isDesktopWindow } from "./desktopChrome.ts";

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

/** the host -> page event every instance request is answered on */
export const INSTANCE_EVENT = "omega:instances";

/** the pane -> status bar event after a browser tab's own save lands */
export const INSTANCE_CHANGED_EVENT = "omega:instances-changed";

/** the raw message bodies the desktop host decodes */
export type InstanceRequest =
  | { omega: "instances:get" }
  | { omega: "instances:save"; instances: SavedInstance[] }
  | { omega: "instances:switch"; url: string };

/** the answer body: the app's list, and the refusal when a write was declined */
export interface InstanceReply {
  instances: SavedInstance[];
  error: string;
}

/** the host's patience with one raw-message round trip */
export const INSTANCE_REPLY_TIMEOUT_MS = 5000;

interface InstanceHost {
  _wails?: {
    invoke?: (message: string) => void;
    dispatchWailsEvent?: (event: { name?: unknown; data?: unknown }) => void;
  };
}

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

// adoptInstances refreshes the cache from the source in charge of the list. Only an
// app-owned answer (the desktop host's list, or a save this page just wrote) reaches it:
// an origin fetch reads the same source, it does not become one.
export function adoptInstances(list: SavedInstance[], storage: InstanceStorage | null = defaultStorage()): void {
  writeInstanceCache({ list, lastUsed: readInstanceCache(storage).lastUsed }, storage);
}

export function lastUsedInstance(storage: InstanceStorage | null = defaultStorage()): string {
  return readInstanceCache(storage).lastUsed;
}

export function setLastUsedInstance(url: string, storage: InstanceStorage | null = defaultStorage()): void {
  const cache = readInstanceCache(storage);
  writeInstanceCache({ list: cache.list, lastUsed: url }, storage);
}

// fetchInstances reads a server's own list. A refusal throws: the caller keeps its cache.
export async function fetchInstances(): Promise<SavedInstance[]> {
  const r = await fetch("/instances");
  if (!r.ok) throw new Error(`instances: ${r.status}`);
  const body = (await r.json()) as { instances?: unknown };
  return Array.isArray(body?.instances) ? (body.instances as SavedInstance[]) : [];
}

// --- the desktop host's channel ---------------------------------------------------

const subscribers = new Set<(reply: InstanceReply) => void>();
const waiters: Array<(reply: InstanceReply) => void> = [];
let armedOn: object | null = null;

function hostApi(): NonNullable<InstanceHost["_wails"]> | null {
  const api = (globalThis as InstanceHost)._wails;
  return api && typeof api.invoke === "function" ? api : null;
}

/** true inside the Wails window: the app, not the loaded page, owns the list */
export function instancesAreAppOwned(host: unknown = globalThis): boolean {
  return isDesktopWindow(host);
}

function coerceReply(data: unknown): InstanceReply {
  const doc = (data ?? {}) as { instances?: unknown; error?: unknown };
  const instances: SavedInstance[] = [];
  if (Array.isArray(doc.instances)) {
    for (const item of doc.instances) {
      if (!item || typeof item !== "object") continue;
      const name = typeof (item as { name?: unknown }).name === "string" ? (item as { name: string }).name : "";
      const url = typeof (item as { url?: unknown }).url === "string" ? (item as { url: string }).url : "";
      if (name && url) instances.push({ name, url });
    }
  }
  return { instances, error: typeof doc.error === "string" ? doc.error : "" };
}

// armHostChannel hooks the one event entry point the Wails shim delivers on. The shim
// object is re-injected on every navigation, so arming is per shim, not per page.
function armHostChannel(): boolean {
  const api = hostApi();
  if (!api) return false;
  if (armedOn === api) return true;
  armedOn = api;
  const previous = api.dispatchWailsEvent;
  api.dispatchWailsEvent = (event) => {
    if (typeof previous === "function") previous.call(api, event);
    if (event?.name !== INSTANCE_EVENT) return;
    const reply = coerceReply(event.data);
    for (const fn of [...subscribers]) fn(reply);
    waiters.shift()?.(reply);
  };
  return true;
}

/** every answer the host emits, for a surface that only reads */
export function onInstancesReply(fn: (reply: InstanceReply) => void): () => void {
  subscribers.add(fn);
  armHostChannel();
  return () => {
    subscribers.delete(fn);
  };
}

function sendInstances(message: InstanceRequest): boolean {
  if (!armHostChannel()) return false;
  hostApi()?.invoke?.(JSON.stringify(message));
  return true;
}

// awaitReply registers BEFORE the message goes out, so a fast answer can never land on a
// waiter that does not exist yet.
function awaitReply(): Promise<InstanceReply> {
  return new Promise((resolve) => {
    let settled = false;
    const settle = (reply: InstanceReply) => {
      if (settled) return;
      settled = true;
      resolve(reply);
    };
    waiters.push(settle);
    setTimeout(() => {
      const at = waiters.indexOf(settle);
      if (at >= 0) waiters.splice(at, 1);
      settle({
        instances: [],
        error: `the app did not answer within ${INSTANCE_REPLY_TIMEOUT_MS / 1000}s`,
      });
    }, INSTANCE_REPLY_TIMEOUT_MS);
  });
}

/** the app's own list */
export async function requestInstances(): Promise<InstanceReply> {
  const pending = awaitReply();
  if (!sendInstances({ omega: "instances:get" })) {
    return { instances: [], error: "this page has no app behind it" };
  }
  return pending;
}

/** replace the app's list; a refusal throws with the host's words */
export async function saveInstancesInApp(list: SavedInstance[]): Promise<SavedInstance[]> {
  const pending = awaitReply();
  if (!sendInstances({ omega: "instances:save", instances: list })) {
    throw new Error("this page has no app behind it");
  }
  const reply = await pending;
  if (reply.error) throw new Error(reply.error);
  return reply.instances;
}

/** show another instance: the app records the active origin and navigates the window */
export function switchInstancesInApp(url: string, storage: InstanceStorage | null = defaultStorage()): void {
  const target = normalizeInstanceURL(url);
  setLastUsedInstance(target, storage);
  sendInstances({ omega: "instances:switch", url: target });
}

export interface InstanceLoad {
  list: SavedInstance[];
  offline: boolean;
}

// loadInstances reads whichever source is in charge. A desktop window asks the app; a
// browser tab fetches its own origin and leaves the app's cache alone.
export async function loadInstances(storage: InstanceStorage | null = defaultStorage()): Promise<InstanceLoad> {
  const cache = readInstanceCache(storage);
  if (instancesAreAppOwned()) {
    const reply = await requestInstances();
    if (reply.error) return { list: cache.list, offline: true };
    adoptInstances(reply.instances, storage);
    return { list: reply.instances, offline: false };
  }
  try {
    const list = await fetchInstances();
    return { list, offline: false };
  } catch {
    return { list: cache.list, offline: true };
  }
}

function defaultNavigate(target: string): void {
  if (typeof window !== "undefined") window.location.assign(target);
}

// navigateToInstance is a browser tab's switch: remember the target, then leave. The page
// reloads - the same thing a browser does when the origin changes.
export function navigateToInstance(
  url: string,
  navigate: (target: string) => void = defaultNavigate,
  storage: InstanceStorage | null = defaultStorage(),
): void {
  const target = normalizeInstanceURL(url);
  setLastUsedInstance(target, storage);
  navigate(target);
}
