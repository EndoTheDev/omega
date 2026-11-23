// omega webui: the observability tab's PURE half. Two reads of the observability capsule
// (harness/observability/routes.go):
//   GET /observability/events?session=&level=&limit= -> {events, buffered, capacity}
//   GET /observability/logs?lines= -> {path, lines} (404 when there is no log file)
// plus the store's session list (GET /sessions) for the one label the source dimension
// needs: the scheduler names every session it fires `cron:<job>`.
//
// The ring is bounded at 1024 and one page caps at PageMax=1000, so an unfiltered page IS
// the whole live history - hence the severity and source filters work CLIENT-side while
// the SESSION scope is a server parameter (filtering the newest N of everything would
// starve a quiet session). DOM-free so the node suite can pin it
// (webui/test/observability.test.ts).

export const EVENTS_PATH = "/observability/events";
export const LOGS_PATH = "/observability/logs";
export const SESSIONS_PATH = "/sessions";

/** the scheduler's label prefix on the sessions it fires (harness/cron/runner.go) */
export const CRON_LABEL_PREFIX = "cron:";

// The source dimension's closed vocabulary. A record's source is DERIVED, never sent:
// the ring has one producer family (the agent loop's spine, recorder.go), so a record
// carries exactly three provenance clues - its type, its tool, and its session (whose
// label can say the scheduler fired the run). Precedence: the RUN outranks the record
// (a cron-fired run's whole trace belongs to the scheduler), then the TOOL's capsule,
// then the provider stream, and the loop owns the remainder. A fifth name (a serving-
// layer `gateway` source) has no producer in the stream today - adding it here without
// one would be a chip that can never match a row.
export const SOURCES = ["llm", "agent", "cron", "kanban"] as const;
export type Source = (typeof SOURCES)[number];

/** session id -> its store label; absent is unknown, not "unlabelled" */
export type SessionLabels = ReadonlyMap<string, string>;

/** one page: the capsule's PageMax ceiling, i.e. essentially the ring */
export const EVENTS_LIMIT = 1000;
/** rows the tab paints: newest in the ring, oldest dropped from the DOM */
export const RENDER_CAP = 300;
/** the tail's line count (the route defaults to the same 200) */
export const TAIL_LINES = 200;
/** the bounded refresh while the tab is mounted: one 5s read, never a poll per record */
export const REFRESH_MS = 5000;

/** one ring record, exactly as the capsule serializes it; `level` is DERIVED server-side (info, warn, error). */
export interface ObservatoryEvent {
  seq: number;
  time: string;
  session?: string;
  type: string;
  level: string;
  tool?: string;
  turn?: number;
  duration_ms?: number;
  summary: string;
  error?: string;
}

export interface EventsPayload {
  events: ObservatoryEvent[];
  buffered: number;
  capacity: number;
}

export interface LogsPayload {
  path: string;
  lines: string[];
}

const str = (v: unknown): string => (typeof v === "string" ? v : "");
const num = (v: unknown): number | undefined =>
  typeof v === "number" && Number.isFinite(v) ? v : undefined;

/** a malformed payload never crashes the pane: null, and the read drops the record. */
export function normalizeEvent(raw: unknown): ObservatoryEvent | null {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) return null;
  const e = raw as Record<string, unknown>;
  const type = str(e.type);
  if (!type) return null;
  const out: ObservatoryEvent = {
    seq: num(e.seq) ?? 0,
    time: str(e.time),
    type,
    level: str(e.level) || "info",
    summary: str(e.summary),
  };
  const session = str(e.session);
  if (session) out.session = session;
  const tool = str(e.tool);
  if (tool) out.tool = tool;
  const turn = num(e.turn);
  if (turn !== undefined) out.turn = turn;
  const duration = num(e.duration_ms);
  if (duration !== undefined) out.duration_ms = duration;
  const error = str(e.error);
  if (error) out.error = error;
  return out;
}

export function parseEventsPayload(body: unknown): EventsPayload {
  const b = (body && typeof body === "object" && !Array.isArray(body)
    ? body
    : {}) as Record<string, unknown>;
  const raw = Array.isArray(b.events) ? b.events : [];
  const events: ObservatoryEvent[] = [];
  for (const r of raw) {
    const e = normalizeEvent(r);
    if (e) events.push(e);
  }
  return {
    events,
    buffered: num(b.buffered) ?? events.length,
    capacity: num(b.capacity) ?? 0,
  };
}

export function parseLogsPayload(body: unknown): LogsPayload {
  const b = (body && typeof body === "object" && !Array.isArray(body)
    ? body
    : {}) as Record<string, unknown>;
  const lines = Array.isArray(b.lines) ? b.lines.filter((l): l is string => typeof l === "string") : [];
  return { path: str(b.path), lines };
}

export function eventsQuery(session: string, limit: number = EVENTS_LIMIT): string {
  const qs = new URLSearchParams();
  if (session) qs.set("session", session);
  qs.set("limit", String(limit));
  return qs.toString();
}

/**
 * parseSessionLabels reads GET /sessions (an array of store rows) down to the one field
 * the source dimension needs. A malformed body yields an empty map: the labels are an
 * attribution aid, so a degraded read must cost the cron chip its rows, never the pane.
 */
export function parseSessionLabels(body: unknown): Map<string, string> {
  const rows = Array.isArray(body) ? body : [];
  const out = new Map<string, string>();
  for (const raw of rows) {
    if (!raw || typeof raw !== "object" || Array.isArray(raw)) continue;
    const row = raw as Record<string, unknown>;
    const id = str(row.id);
    const label = str(row.label);
    if (id && label) out.set(id, label);
  }
  return out;
}

/** eventSource names the subsystem that produced one record (see SOURCES for the rules). */
export function eventSource(e: ObservatoryEvent, labels: SessionLabels = new Map()): Source {
  const label = e.session ? (labels.get(e.session) ?? "") : "";
  if (label.startsWith(CRON_LABEL_PREFIX)) return "cron";
  if (e.tool && e.tool.startsWith("kanban.")) return "kanban";
  if (e.type === "stream_end") return "llm";
  return "agent";
}

/** one reading of the pane's filter family; the dimensions compose with AND */
export interface StreamFilter {
  /** severity: keep error-classed rows only */
  errorsOnly?: boolean;
  /** source: the enabled set; absent = every source, an empty array = none on */
  sources?: readonly Source[];
  /** the clear button's watermark: rows at or below it are display-hidden */
  clearedThrough?: number;
}

export function passesFilter(
  e: ObservatoryEvent,
  filter: StreamFilter,
  labels: SessionLabels = new Map(),
): boolean {
  if (filter.clearedThrough !== undefined && e.seq <= filter.clearedThrough) return false;
  if (filter.errorsOnly && !isErrorEvent(e)) return false;
  if (filter.sources && !filter.sources.includes(eventSource(e, labels))) return false;
  return true;
}

/** visibleEvents selects what the tab paints, NEWEST FIRST. */
export function visibleEvents(
  events: readonly ObservatoryEvent[],
  filter: StreamFilter = {},
  cap: number = RENDER_CAP,
  labels: SessionLabels = new Map(),
): ObservatoryEvent[] {
  const picked = events.filter((e) => passesFilter(e, filter, labels));
  const bound = cap > 0 ? cap : picked.length;
  return picked.slice(Math.max(0, picked.length - bound)).reverse();
}

/** sourceCounts is the per-chip reading over the whole fetched page (filters aside). */
export function sourceCounts(
  events: readonly ObservatoryEvent[],
  labels: SessionLabels = new Map(),
): Record<Source, number> {
  const counts = Object.fromEntries(SOURCES.map((s) => [s, 0])) as Record<Source, number>;
  for (const e of events) counts[eventSource(e, labels)] += 1;
  return counts;
}

export function logsQuery(lines: number = TAIL_LINES): string {
  return `lines=${lines}`;
}

export function describeReadError(path: string, status: number): string {
  if (status === 404) {
    return path === EVENTS_PATH
      ? "observability module is not loaded (404) - enable it in Modules"
      : "no log file (logging disabled, or a logger with no path)";
  }
  return `GET ${path}: ${status}`;
}

/** the derived level is the one truth: a record with `error` text still reads by its level, never re-derived here. */
export function isErrorEvent(e: ObservatoryEvent): boolean {
  return e.level === "error";
}

export function errorCount(events: readonly ObservatoryEvent[]): number {
  return events.filter(isErrorEvent).length;
}

/** the row's clock reading; an unparseable stamp renders verbatim rather than "Invalid Date". */
export function formatEventTime(iso: string): string {
  if (!iso) return "-";
  const at = new Date(iso);
  if (Number.isNaN(at.getTime())) return iso;
  const pad = (n: number): string => String(n).padStart(2, "0");
  return `${pad(at.getHours())}:${pad(at.getMinutes())}:${pad(at.getSeconds())}`;
}

/** a missed pair carries no duration at all - an em dash, never a fabricated zero. */
export function formatDuration(ms?: number): string {
  if (ms === undefined || ms < 0) return "-";
  if (ms < 1000) return `${Math.round(ms)}ms`;
  return `${(ms / 1000).toFixed(1)}s`;
}

export function shortSession(id?: string): string {
  if (!id) return "-";
  return id.length > 8 ? id.slice(0, 8) : id;
}
