// omega webui: the connection list's state dots, fed by ONE batch read of each address's
// health body. A browser cannot read another origin's /health (no CORS on this surface by
// design), so the serve probes on the page's behalf and answers one row per address.
// This module asks, maps the answer onto the four dot states, and owns the poll - nothing
// here runs on page load: a panel (or the settings pane) opens the poll and closing it
// stops the timer, so a closed list costs no traffic.

import { instanceAddressKey, type SavedInstance } from "./instances.ts";

export type InstanceState = "healthy" | "warning" | "error" | "unverified";

export interface InstanceStatusRow {
  url: string;
  state: InstanceState;
  /** the probe's failure class when nothing answered, or "not-omega" */
  failure?: string;
  reason?: string;
  /** the health body when one came back - the tooltip's facts */
  facts?: Record<string, unknown>;
}

export interface InstanceStatusReply {
  rows: InstanceStatusRow[];
  /** false when the serving build answered its SPA shell where the route would be */
  checked: boolean;
  /** one muted line for the surface; empty while the states are real */
  note: string;
}

export const STATUS_POLL_MS = 30_000;

export const STATUS_NO_ROUTE_NOTE = "states not checked (this server's build predates them)";

// the classes that mean "no answer at all" - the only ones the hatch flag may soften
const NO_ANSWER_FAILURES: ReadonlySet<string> = new Set(["syntax", "unreachable", "guard"]);

function sourcedFalse(facts: Record<string, unknown> | undefined, section: string, key: string): boolean {
  const group = facts?.[section];
  if (!group || typeof group !== "object") return false;
  return (group as Record<string, unknown>)[key] === false;
}

// classifyInstance is the pane's read of the state law that agent/api/server_instances.go
// states for the wire. The hatch flag lives in the app's list, never on the wire, so the
// pane is the only place that can soften "nothing answered" into "unverified".
export function classifyInstance(row: InstanceStatusRow, unverified = false): InstanceState {
  if (row.state === "unverified") return "unverified";
  // an ANSWER that is not omega is wrong whatever the flag says: we know what it is
  if (row.failure === "not-omega") return "error";
  // the flag only softens NO ANSWER, never a refusal we can name
  if (row.failure && NO_ANSWER_FAILURES.has(row.failure)) {
    return unverified ? "unverified" : "error";
  }
  if (row.state === "warning") return "warning";
  if (row.state === "error") return "error";
  // omega answered: only a SOURCED false degrades - an absent fact is unknown, not broken
  if (sourcedFalse(row.facts, "provider", "reachable") || sourcedFalse(row.facts, "database", "healthy")) {
    return "warning";
  }
  return "healthy";
}

// stateView is the ONE state -> class + word mapping, so no surface invents a colour or
// a word of its own. The word is what an accessible name carries; the class is paint.
export function stateView(state: InstanceState): { word: string; cls: string } {
  switch (state) {
    case "healthy":
      return { word: "healthy", cls: "ok" };
    case "warning":
      return { word: "warning", cls: "warn" };
    case "unverified":
      return { word: "unverified", cls: "unverified" };
    default:
      return { word: "error", cls: "down" };
  }
}

function coerceStatusRow(item: unknown): InstanceStatusRow | null {
  if (!item || typeof item !== "object") return null;
  const doc = item as { url?: unknown; state?: unknown; failure?: unknown; reason?: unknown; facts?: unknown };
  if (typeof doc.url !== "string" || !doc.url) return null;
  const state = typeof doc.state === "string" ? (doc.state as InstanceState) : "error";
  const row: InstanceStatusRow = { url: doc.url, state };
  if (typeof doc.failure === "string" && doc.failure) row.failure = doc.failure;
  if (typeof doc.reason === "string" && doc.reason) row.reason = doc.reason;
  if (doc.facts && typeof doc.facts === "object") row.facts = doc.facts as Record<string, unknown>;
  return row;
}

// fetchInstanceStatus asks the serve for one row per address, in the order asked. A 200
// that is not the batch envelope is an older build's SPA catch-all: not checked, never a
// guessed colour.
export async function fetchInstanceStatus(
  urls: readonly string[],
  doFetch: (target: string) => Promise<Response> = (target) => fetch(target),
): Promise<InstanceStatusReply> {
  const query = urls.map((url) => `urls=${encodeURIComponent(url)}`).join("&");
  let response: Response;
  try {
    response = await doFetch(`/instances/status${query ? `?${query}` : ""}`);
  } catch {
    return { rows: [], checked: false, note: "states not checked (this page could not reach its server)" };
  }
  let doc: unknown = null;
  try {
    doc = await response.json();
  } catch {
    doc = null;
  }
  const rows = (doc as { rows?: unknown } | null)?.rows;
  if (!response.ok) {
    return { rows: [], checked: false, note: `states not checked (this server answered ${response.status})` };
  }
  if (!Array.isArray(rows)) {
    return { rows: [], checked: false, note: STATUS_NO_ROUTE_NOTE };
  }
  const out: InstanceStatusRow[] = [];
  for (const item of rows) {
    const row = coerceStatusRow(item);
    if (row) out.push(row);
  }
  return { rows: out, checked: true, note: "" };
}

// instanceStates folds a reply and the list's flags into the pane's lookup, keyed by the
// address's canonical spelling so a row's own typing matches the serve's normalized URL.
export function instanceStates(
  rows: readonly SavedInstance[],
  reply: InstanceStatusReply,
): Map<string, InstanceState> {
  const states = new Map<string, InstanceState>();
  if (!reply.checked) return states;
  const flagged = new Set(rows.filter((row) => row.unverified).map((row) => instanceAddressKey(row.url)));
  for (const row of reply.rows) {
    const key = instanceAddressKey(row.url);
    states.set(key, classifyInstance(row, flagged.has(key)));
  }
  return states;
}

/** the state a row wears, or "" when this build never got one */
export function stateFor(states: ReadonlyMap<string, InstanceState>, url: string): InstanceState | "" {
  return states.get(instanceAddressKey(url)) ?? "";
}

/** the reasons behind the states, keyed by the same canonical address - the tooltip's line */
export function instanceReasons(reply: InstanceStatusReply): Map<string, string> {
  const reasons = new Map<string, string>();
  for (const row of reply.rows) {
    if (row.reason) reasons.set(instanceAddressKey(row.url), row.reason);
  }
  return reasons;
}

export interface InstanceStatusPollOptions {
  load?: (rows: readonly SavedInstance[]) => Promise<InstanceStatusReply>;
  intervalMs?: number;
  onReply?: (reply: InstanceStatusReply, states: Map<string, InstanceState>) => void;
  setInterval?: (fn: () => void, ms: number) => unknown;
  clearInterval?: (handle: unknown) => void;
}

export interface InstanceStatusPoll {
  /** the panel opened: read now, then on the tick */
  start(rows: readonly SavedInstance[]): void;
  /** one read, for a save that changed the list while the poll runs */
  refresh(rows: readonly SavedInstance[]): Promise<void>;
  /** the panel closed (or the pane unmounted): the timer goes */
  stop(): void;
  readonly running: boolean;
}

// startInstanceStatusPoll is the whole lifecycle: one timer, one read in flight. A close
// during a read paints nothing, and a tick that lands mid-read is skipped rather than
// queued - the next tick is the retry.
export function startInstanceStatusPoll(options: InstanceStatusPollOptions = {}): InstanceStatusPoll {
  const load =
    options.load ?? ((rows: readonly SavedInstance[]) => fetchInstanceStatus(rows.map((row) => row.url)));
  const intervalMs = options.intervalMs ?? STATUS_POLL_MS;
  const arm = options.setInterval ?? ((fn: () => void, ms: number) => setInterval(fn, ms));
  const disarm = options.clearInterval ?? ((handle: unknown) => clearInterval(handle as ReturnType<typeof setInterval>));
  let handle: unknown = null;
  let inFlight = false;
  let current: readonly SavedInstance[] = [];

  function running(): boolean {
    return handle !== null;
  }

  async function refresh(rows: readonly SavedInstance[]): Promise<void> {
    current = rows;
    if (inFlight) return;
    inFlight = true;
    try {
      const reply = await load(rows);
      if (!running()) return;
      options.onReply?.(reply, instanceStates(rows, reply));
    } catch {
      // a refused read keeps the last paint and the note - never an invented colour
    } finally {
      inFlight = false;
    }
  }

  function start(rows: readonly SavedInstance[]): void {
    if (running()) return;
    current = rows;
    handle = arm(() => {
      void refresh(current);
    }, intervalMs);
    void refresh(rows);
  }

  function stop(): void {
    if (handle === null) return;
    disarm(handle);
    handle = null;
  }

  return {
    start,
    refresh,
    stop,
    get running() {
      return running();
    },
  };
}
