// omega webui: the connection list's client half. Inside a desktop window the APP owns
// the list: the host stores it in the local home's config.yaml (`desktop.instances`) and
// answers every read, write and switch over the raw message channel, so a page served by
// a build that predates the title bar still lists and switches the app's own
// connections. That source is DECIDED once, by the shell verdict (desktopChrome.ts): an
// armed page asks the app and an armed answer that fails keeps the cache - it never falls
// back to the serving server's list. A page the shim never reached (a plain browser tab, a
// stranger's omega) reads the active server's GET /instances. localStorage under
// `omega.instances` is the paint-first copy of whichever source is in charge, never a
// second truth. Switching never fetches the remote origin (CORS): an armed page asks the
// host to show the row, a tab navigates, and the page reloads.

import { isDesktopWindow, whenShellDecided, type ShellVerdict } from "./desktopChrome.ts";

export interface SavedInstance {
  name: string;
  url: string;
  /**
   * added anyway: the operator knew the address was real but nothing answered when it was
   * checked. A note about the last check, never a verdict - a later answer makes the row
   * healthy whatever this says.
   */
  unverified?: boolean;
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

/** the raw message bodies the desktop host decodes. The switch's `url` is a row KEY: the
 * host resolves it against its own list plus home and refuses a miss. */
export type InstanceRequest =
  | { omega: "instances:get" }
  | { omega: "instances:save"; instances: SavedInstance[] }
  | { omega: "instances:switch"; url: string };

/** the answer body: the app's list, the refusal when a write was declined, and the
 * desktop's own home origin (absent in a browser tab - the tab's own origin is its home) */
export interface InstanceReply {
  instances: SavedInstance[];
  error: string;
  home?: string;
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

// coerceRow reads one row off a wire body or the cache; the flag rides only when it is
// true, so an absent flag and a verified row stay the same row.
function coerceRow(item: unknown): SavedInstance | null {
  if (!item || typeof item !== "object") return null;
  const doc = item as { name?: unknown; url?: unknown; unverified?: unknown };
  const name = typeof doc.name === "string" ? doc.name : "";
  const url = typeof doc.url === "string" ? doc.url : "";
  if (!name || !url) return null;
  const row: SavedInstance = { name, url };
  if (doc.unverified === true) row.unverified = true;
  return row;
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
      const row = coerceRow(item);
      if (row) list.push(row);
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
  if (!Array.isArray(body?.instances)) return [];
  const list: SavedInstance[] = [];
  for (const item of body.instances) {
    const row = coerceRow(item);
    if (row) list.push(row);
  }
  return list;
}

// --- the add-time probe -----------------------------------------------------------

/** the failure classes the serve answers, plus the one the PAGE adds */
export const PROBE_FAILURES = ["syntax", "unreachable", "not-omega", "guard"] as const;
export type InstanceProbeFailure = (typeof PROBE_FAILURES)[number] | "no-route";

/** the serve's answer about one address: `ok` decides, a refusal carries its own reason */
export interface InstanceProbe {
  ok: boolean;
  url: string;
  state?: string;
  facts?: Record<string, unknown>;
  failure?: InstanceProbeFailure;
  reason?: string;
}

/** an older serve answers its SPA shell where the probe route would be: not an answer */
export const PROBE_NO_ROUTE_REASON = "this server cannot check addresses (its build predates the check)";

// probeInstance asks the serve to fetch the address on this page's behalf - a browser cannot
// read another origin's health body, so the check can only be the server's. A 200 that is
// not the probe's envelope is an older build's SPA catch-all: no-route, never a pass.
export async function probeInstance(
  raw: string,
  doFetch: (target: string) => Promise<Response> = (target) => fetch(target),
): Promise<InstanceProbe> {
  const typed = String(raw ?? "").trim();
  let response: Response;
  try {
    response = await doFetch(`/instances/probe?url=${encodeURIComponent(typed)}`);
  } catch {
    return {
      ok: false,
      url: typed,
      failure: "no-route",
      reason: "this page could not reach its own server to check the address",
    };
  }
  let doc: unknown = null;
  try {
    doc = await response.json();
  } catch {
    doc = null;
  }
  const envelope = (doc ?? {}) as {
    ok?: unknown;
    url?: unknown;
    state?: unknown;
    facts?: unknown;
    failure?: unknown;
    reason?: unknown;
  };
  if (typeof envelope.ok !== "boolean") {
    return { ok: false, url: typed, failure: "no-route", reason: PROBE_NO_ROUTE_REASON };
  }
  const url = typeof envelope.url === "string" && envelope.url ? envelope.url : typed;
  if (envelope.ok) {
    return {
      ok: true,
      url,
      state: typeof envelope.state === "string" ? envelope.state : "omega",
      facts:
        envelope.facts && typeof envelope.facts === "object"
          ? (envelope.facts as Record<string, unknown>)
          : undefined,
    };
  }
  return {
    ok: false,
    url,
    failure: typeof envelope.failure === "string" ? (envelope.failure as InstanceProbeFailure) : "no-route",
    reason: typeof envelope.reason === "string" && envelope.reason ? envelope.reason : "the address was refused without a reason",
  };
}

export interface InstanceDraft {
  name: string;
  url: string;
}

export interface InstanceAdd {
  list: SavedInstance[];
  probe: InstanceProbe;
  added: boolean;
}

// instanceAddressKey folds the spellings of one address: the same endpoint typed with or
// without its scheme and trailing slash is the same row, so an add renames instead of
// appending a duplicate the serve would refuse.
export function instanceAddressKey(raw: string): string {
  const s = String(raw ?? "")
    .trim()
    .toLowerCase()
    .replace(/\/+$/, "");
  return /^[a-z][a-z0-9+.-]*:\/\//.test(s) ? s : `http://${s}`;
}

// matchedURL keeps an existing row's own spelling when the address is the same endpoint.
function matchedURL(list: SavedInstance[], address: string): string {
  const key = instanceAddressKey(address);
  return list.find((row) => instanceAddressKey(row.url) === key)?.url ?? address;
}

// addInstanceRow is the add ACTION: prove the address, then append. A refusal appends
// nothing and answers the probe, so the pane renders the reason beside the hatch.
export async function addInstanceRow(
  list: SavedInstance[],
  draft: InstanceDraft,
  probe: (raw: string) => Promise<InstanceProbe> = probeInstance,
): Promise<InstanceAdd> {
  const typed = String(draft.url ?? "").trim();
  const name = String(draft.name ?? "").trim();
  // the name is the other half of the file's validator: no build saves a nameless row, so
  // an add without one never reaches the network.
  if (!name) {
    return {
      list,
      added: false,
      probe: { ok: false, url: typed, failure: "syntax", reason: "a name is required" },
    };
  }
  const answer = await probe(typed);
  if (!answer.ok) return { list, added: false, probe: answer };
  // a verified row carries no flag: live evidence outranks one a hatch stamped earlier.
  return { list: upsertInstance(list, { name, url: matchedURL(list, answer.url) }), probe: answer, added: true };
}

// hatchInstanceRow is the add-anyway door: the operator knows the address is real and only
// unreachable right now, so the row lands stamped unverified. A syntax refusal never
// reaches it - a shape the file refuses could not be saved by any build.
export function hatchInstanceRow(
  list: SavedInstance[],
  draft: InstanceDraft,
  refused?: InstanceProbe,
): SavedInstance[] {
  const typed = String(draft.url ?? "").trim();
  const address = refused?.url ? refused.url : typed;
  let normalized = address;
  try {
    normalized = normalizeInstanceURL(address);
  } catch {
    normalized = address;
  }
  return upsertInstance(list, {
    name: String(draft.name ?? "").trim(),
    url: matchedURL(list, normalized),
    unverified: true,
  });
}

// --- the desktop host's channel ---------------------------------------------------

const subscribers = new Set<(reply: InstanceReply) => void>();
// One entry per unanswered request, oldest first. A wait's own deadline is carried so a
// request that already gave up can never take the next answer off a live one.
export interface InstanceWaiter {
  at: number;
  settle: (reply: InstanceReply) => void;
}
const waiters: InstanceWaiter[] = [];
let armedOn: object | null = null;

function hostApi(): NonNullable<InstanceHost["_wails"]> | null {
  const api = (globalThis as InstanceHost)._wails;
  return api && typeof api.invoke === "function" ? api : null;
}

/** true when the Wails shim is present: the settings pane routes its `desktop.instances`
 * save to the app on it. The LIST's source is the shell verdict (whenShellDecided), not
 * this shape read. */
export function instancesAreAppOwned(host: unknown = globalThis): boolean {
  return isDesktopWindow(host);
}

function coerceReply(data: unknown): InstanceReply {
  const doc = (data ?? {}) as { instances?: unknown; error?: unknown; home?: unknown };
  const instances: SavedInstance[] = [];
  if (Array.isArray(doc.instances)) {
    for (const item of doc.instances) {
      const row = coerceRow(item);
      if (row) instances.push(row);
    }
  }
  const reply: InstanceReply = { instances, error: typeof doc.error === "string" ? doc.error : "" };
  if (typeof doc.home === "string" && doc.home) reply.home = doc.home;
  return reply;
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
    nextWaiter()?.(reply);
  };
  return true;
}

// takeWaiter is the answer's consumer: the oldest request still inside its own deadline.
// A request that already resolved on its timeout has no claim on a later answer, so it is
// dropped rather than handed someone else's reply.
export function takeWaiter(
  queue: InstanceWaiter[],
  now: number,
  timeoutMs: number = INSTANCE_REPLY_TIMEOUT_MS,
): InstanceWaiter | null {
  while (queue.length > 0) {
    const head = queue.shift();
    if (!head) return null;
    if (now - head.at <= timeoutMs) return head;
  }
  return null;
}

// nextWaiter is the channel's one door on that queue.
function nextWaiter(): ((reply: InstanceReply) => void) | undefined {
  return takeWaiter(waiters, Date.now())?.settle;
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
    const entry = { at: Date.now(), settle };
    waiters.push(entry);
    setTimeout(() => {
      const at = waiters.indexOf(entry);
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

/** show another instance: the app resolves the row key and navigates the window */
export function switchInstancesInApp(key: string, storage: InstanceStorage | null = defaultStorage()): void {
  const target = String(key ?? "").trim();
  setLastUsedInstance(target, storage);
  sendInstances({ omega: "instances:switch", url: target });
}

export interface InstanceLoad {
  list: SavedInstance[];
  offline: boolean;
  /** the shell verdict this read was decided by: armed = the app's list, absent = the
   * serving server's */
  shell: Exclude<ShellVerdict, "undecided">;
  /** the desktop's own home origin; a browser tab has none (its own origin is home) */
  home?: string;
}

// loadInstances reads whichever source the shell verdict names, and asks nothing before
// the verdict lands: an armed page asks the app (a failed answer keeps the cache and says
// so - never the serving server's rows), an absent page fetches its own origin. The app's
// answer also carries the desktop's home origin - derived state the page only displays.
export async function loadInstances(
  storage: InstanceStorage | null = defaultStorage(),
  decided: () => Promise<Exclude<ShellVerdict, "undecided">> = whenShellDecided,
): Promise<InstanceLoad> {
  const cache = readInstanceCache(storage);
  const shell = await decided();
  if (shell === "armed") {
    const reply = await requestInstances();
    if (reply.error) return { list: cache.list, offline: true, shell, home: reply.home };
    adoptInstances(reply.instances, storage);
    return { list: reply.instances, offline: false, shell, home: reply.home };
  }
  try {
    const list = await fetchInstances();
    return { list, offline: false, shell };
  } catch {
    return { list: cache.list, offline: true, shell };
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
